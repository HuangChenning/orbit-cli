"""Build and verify unsigned native release candidates. Never tags or publishes."""

import argparse
import datetime as dt
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import platform
import re
import subprocess
import tarfile
import zipfile


ROOT = Path(__file__).resolve().parents[1]
TARGETS = ("darwin_arm64", "darwin_amd64", "linux_amd64", "windows_amd64")
GO_VERSION = "go1.27.3"
CONTENTS = ("LICENSE", "README.md", "README.zh-CN.md", "CHANGELOG.md")


def run(*args, env=None):
    return subprocess.check_output(args, cwd=ROOT, env=env, text=True).strip()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def native_target():
    system = {"Darwin": "darwin", "Linux": "linux", "Windows": "windows"}[platform.system()]
    arch = {"arm64": "arm64", "aarch64": "arm64", "x86_64": "amd64", "amd64": "amd64"}[platform.machine().lower()]
    target = f"{system}_{arch}"
    if target not in TARGETS:
        raise ValueError(f"unsupported native target: {target}")
    return target


def archive_name(version, target):
    return f"orbit_{version.removeprefix('v')}_{target}." + ("zip" if target.startswith("windows_") else "tar.gz")


def build(args):
    if run("git", "status", "--porcelain", "--untracked-files=normal"):
        raise ValueError("candidate builds require a clean checkout; commit reviewed changes first")
    target = native_target()
    goos, goarch = target.split("_")
    if json.loads(run("go", "env", "-json", "GOOS", "GOARCH")) != {"GOOS": goos, "GOARCH": goarch}:
        raise ValueError("candidate build must use the native GOOS/GOARCH")
    toolchain = run("go", "env", "GOVERSION")
    if toolchain != GO_VERSION:
        raise ValueError(f"release toolchain must be {GO_VERSION}, got {toolchain}")
    commit = run("git", "rev-parse", "HEAD")
    epoch = int(run("git", "show", "-s", "--format=%ct", "HEAD"))
    built = dt.datetime.fromtimestamp(epoch, dt.timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    output = Path(args.directory).resolve()
    binary_name = "orbit.exe" if goos == "windows" else "orbit"
    binary = output / target / binary_name
    binary.parent.mkdir(parents=True, exist_ok=True)
    package = "github.com/HuangChenning/orbit-cli/src/internal/cli"
    flags = f"-X {package}.Version={args.version} -X {package}.Commit={commit} -X {package}.BuildTime={built}"
    command = ["go", "build", "-trimpath", "-buildvcs=false", "-ldflags", flags, "-o", str(binary), "./src"]
    subprocess.run(command, cwd=ROOT, env=dict(os.environ, CGO_ENABLED="0"), check=True)
    archive = output / archive_name(args.version, target)
    files = [(binary_name, binary)] + [(name, ROOT / name) for name in CONTENTS]
    if goos == "windows":
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED) as bundle:
            date = dt.datetime.fromtimestamp(epoch, dt.timezone.utc).timetuple()[:6]
            for name, source in files:
                info = zipfile.ZipInfo(name, date)
                info.external_attr = (0o100755 if name == binary_name else 0o100644) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                bundle.writestr(info, source.read_bytes())
    else:
        with archive.open("wb") as raw, gzip.GzipFile(filename="", fileobj=raw, mode="wb", mtime=epoch) as compressed:
            with tarfile.open(fileobj=compressed, mode="w") as bundle:
                for name, source in files:
                    data = source.read_bytes()
                    info = tarfile.TarInfo(name)
                    info.size, info.mtime = len(data), epoch
                    info.mode = 0o755 if name == binary_name else 0o644
                    bundle.addfile(info, io.BytesIO(data))
    metadata = dict(version=args.version, commit=commit, tree=run("git", "rev-parse", "HEAD^{tree}"),
                    built=built, go=toolchain, target=target, os=platform.platform(),
                    image_version=os.environ.get("ImageVersion", "local"), git=run("git", "--version"),
                    python=platform.python_version(), cgo_enabled=False, build_args=command[1:-2] + ["<binary>", "./src"],
                    archive=archive.name, sha256=digest(archive), signed=False)
    archive.with_name(archive.name + ".json").write_text(json.dumps(metadata, indent=2) + "\n", encoding="utf-8")
    print(archive)


def verify(args):
    target = native_target()
    directory = Path(args.directory).resolve()
    archive = directory / archive_name(args.version, target)
    metadata = json.loads(archive.with_name(archive.name + ".json").read_text(encoding="utf-8"))
    if (metadata["version"], metadata["commit"], metadata["target"], metadata["sha256"]) != (args.version, args.commit, target, digest(archive)):
        raise ValueError("candidate metadata/checksum does not match the expected source and native target")
    binary_name = "orbit.exe" if target.startswith("windows_") else "orbit"
    expected = {binary_name, *CONTENTS}
    destination = Path(args.destination).resolve()
    destination.mkdir(parents=True, exist_ok=False)
    if archive.suffix == ".zip":
        with zipfile.ZipFile(archive) as bundle:
            names = bundle.namelist()
            if set(names) != expected or len(names) != len(expected):
                raise ValueError("unexpected package contents")
            for info in bundle.infolist():
                if info.is_dir() or (info.external_attr >> 16) & 0o170000 != 0o100000:
                    raise ValueError("package entries must be regular files")
                (destination / info.filename).write_bytes(bundle.read(info))
    else:
        with tarfile.open(archive) as bundle:
            members = bundle.getmembers()
            if {m.name for m in members} != expected or len(members) != len(expected) or any(not m.isfile() for m in members):
                raise ValueError("unexpected package contents")
            for member in members:
                path = destination / member.name
                path.write_bytes(bundle.extractfile(member).read())
                path.chmod(member.mode)
    binary = destination / binary_name
    version = run(str(binary), "--version")
    for field in (f"orbit {args.version}", f"commit: {args.commit}", f"built: {metadata['built']}", f"go: {metadata['go']}"):
        if field not in version.splitlines():
            raise ValueError(f"installed binary version mismatch: {field}")
    run(str(binary), "--help")
    print(f"verified {archive.name}: {metadata['sha256']}")


def checksums(args):
    directory = Path(args.directory).resolve()
    records = []
    for target in TARGETS:
        archive = directory / archive_name(args.version, target)
        record = json.loads(archive.with_name(archive.name + ".json").read_text(encoding="utf-8"))
        if (record["version"], record["commit"], record["target"], record["archive"], record["sha256"], record["go"], record["signed"]) != (args.version, args.commit, target, archive.name, digest(archive), GO_VERSION, False):
            raise ValueError(f"candidate mismatch: {target}")
        records.append(record)
    for field in ('tree', 'built', 'build_args', 'cgo_enabled'):
        if any(record[field] != records[0][field] for record in records):
            raise ValueError(f'candidate {field} values differ')
    (directory / "SHA256SUMS").write_text("".join(f"{r['sha256']}  {r['archive']}\n" for r in records), encoding="utf-8")
    (directory / "CANDIDATE.json").write_text(json.dumps(dict(version=args.version, commit=args.commit, packages=records), indent=2) + "\n", encoding="utf-8")
    print(f"verified {len(records)} packages for {args.version} at {args.commit}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("build", "verify", "checksums"))
    parser.add_argument("--version", required=True)
    parser.add_argument("--commit")
    parser.add_argument("--directory", default="dist")
    parser.add_argument("--destination", default="dist/installed")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", args.version):
        parser.error("version must be vMAJOR.MINOR.PATCH")
    if args.command != "build" and (not args.commit or not re.fullmatch(r"[0-9a-f]{40}", args.commit)):
        parser.error("verification requires --commit with the full expected SHA")
    try:
        {"build": build, "verify": verify, "checksums": checksums}[args.command](args)
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError) as exc:
        parser.exit(1, f"candidate: {exc}\n")


if __name__ == "__main__":
    main()
