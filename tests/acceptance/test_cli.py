"""Black-box contract for the Go CLI. No network or real tool directories.

ORBIT_BIN=/absolute/path/to/orbit python3 -m unittest discover -s tests/acceptance -v
Missing CLI is an explicit prerequisite failure, never a skip or a fake success.
"""

import json
import stat
import os
import pathlib
import subprocess
import tempfile
import shutil
import time
import unittest

import yaml


ROOT = pathlib.Path(__file__).resolve().parents[2]


class OrbitCLIFixture(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.binary = pathlib.Path(os.environ.get("ORBIT_BIN", str(ROOT / ("orbit.exe" if os.name == "nt" else "orbit")))).resolve()
        if not cls.binary.is_file():
            raise AssertionError(
                f"Orbit CLI is not implemented/built: {cls.binary}. "
                "Configuration tests do not prove Sync/Link/Mirror work. "
                "Build the Go CLI and set ORBIT_BIN to run these contracts."
            )

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="orbit-contract-")
        self.addCleanup(self.temp.cleanup)
        self.root = pathlib.Path(self.temp.name)
        self.env = dict(os.environ, GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL=os.devnull,
                        GIT_TERMINAL_PROMPT="0", GIT_ALLOW_PROTOCOL="file",
                        GIT_CONFIG_COUNT="1", GIT_CONFIG_KEY_0="core.symlinks",
                        GIT_CONFIG_VALUE_0="true",
                        GIT_AUTHOR_NAME="OrbitTests", GIT_AUTHOR_EMAIL="orbit@example.invalid",
                        GIT_COMMITTER_NAME="OrbitTests", GIT_COMMITTER_EMAIL="orbit@example.invalid")
        self.upstream = self.root / "upstream"
        self.upstream.mkdir()
        self.git(self.upstream, "init", "--initial-branch=main")
        for relative, content in {
            "skills/alpha/SKILL.md": b"alpha-v1\n",
            "skills/alpha/.env": b"ordinary-fixture-data=1\n",
            "workflows/review.md": b"review-v1\n",
            "scripts/run.sh": b"#!/bin/sh\r\necho fixture\r\n",
            "commands/build.md": b"build-v1\n",
            ".gitignore": b"ignored.tmp\n",
            "unrelated.txt": b"not part of any dependency\n",
        }.items():
            path = self.upstream / relative
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(content)
        (self.upstream / "scripts/run.sh").chmod(0o755)
        self.commit("fixture v1")
        self.first = self.git(self.upstream, "rev-parse", "HEAD").strip()
        self.remote = self.root / "remote.git"
        self.git(self.root, "init", "--bare", "--initial-branch=main", str(self.remote))
        self.path = self.root / "orbit.yaml"
        self.config = dict(version="1.0", global_cache=str(self.root / "cache"),
                           orbit_repo=dict(remote=str(self.remote), branch="main",
                                           local_path=str(self.root / "mirror")),
                           link_targets={c: [str(self.root / "workspace" / c)]
                                         for c in ("skills", "workflows", "scripts", "commands")},
                           dependencies=[])
        for category, subdir in (("skills", "skills/alpha"), ("workflows", "workflows"),
                                 ("scripts", "scripts"), ("commands", "commands")):
            self.config["dependencies"].append(dict(
                name=category, source=str(self.upstream), version="refs/heads/main",
                subdir=subdir, mirror_dir=category,
                link=True, labels=[category],
            ))
        self.save()

    def cache_path(self, dependency):
        for path in (self.root / "cache/.state").glob("*.json"):
            state = json.loads(path.read_text())
            if (state["Version"] == dependency["version"] and
                    pathlib.Path(state["Source"]).resolve() == pathlib.Path(dependency["source"]).resolve()):
                return self.root / "cache" / path.stem
        self.fail("selected dependency must have a ready cache ownership record")

    def remove_fixture_tree(self, path):
        # Only this test's temporary tree may have Git's read-only bits cleared.
        path = pathlib.Path(path)
        self.assertTrue(path.resolve().is_relative_to(self.root.resolve()))
        def retry(function, filename, error):
            if not isinstance(error[1], PermissionError):
                raise error[1]
            os.chmod(filename, stat.S_IWRITE | stat.S_IREAD)
            function(filename)
        shutil.rmtree(path, onerror=retry)

    def git(self, directory, *args, success=True):
        result = subprocess.run(
            ["git", "-c", "user.name=OrbitTests", "-c", "user.email=orbit@example.invalid",
             "-c", "core.autocrlf=false", "-c", "core.symlinks=true", "-C", str(directory), *args],
            capture_output=True, text=True, env=self.env, timeout=20,
        )
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout

    def commit(self, message):
        self.git(self.upstream, "add", ".")
        self.git(self.upstream, "commit", "-m", message)

    def save(self):
        self.path.write_text(yaml.safe_dump(self.config, sort_keys=False))

    def orbit(self, *args, success=True, stdin=None, cwd=None, explicit=True):
        prefix = [str(self.binary)]
        if explicit:
            prefix += ["--config", str(self.path)]
        result = subprocess.run(prefix + list(args), capture_output=True, text=True,
                                input=stdin, cwd=cwd or self.root, env=self.env, timeout=30)
        if success:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        elif success is False:
            self.assertNotEqual(result.returncode, 0, "failure was falsely reported as success")
        return result

    def sync_link(self):
        self.orbit("vendor", "sync")
        self.orbit("link")

    def mirror_head(self):
        return self.git(self.remote, "show-ref", "--hash", "--verify", "refs/heads/main", success=False).strip()


class OrbitCLIContracts(OrbitCLIFixture):
    def test_ref_migration_reuses_installed_assets_and_cache(self):
        self.sync_link()
        states = sorted((self.root / "cache/.state").glob("*.json"))
        before = {p.name: p.read_bytes() for p in states}
        manifest = (self.root / "mirror/.orbit-manifest.yaml").read_bytes()
        for dep in self.config["dependencies"]:
            dep["ref"] = dep.pop("version")
        self.save()
        self.orbit("check")
        self.orbit("link")
        self.orbit("sync")
        self.assertEqual((self.root / "mirror/.orbit-manifest.yaml").read_bytes(), manifest)
        self.assertEqual({p.name: p.read_bytes() for p in states}, before)
        self.assertEqual(len(list((self.root / "cache/.state").glob("*.json"))), 1)

    def test_vendor_false_preserves_local_edits_manifest_and_publication(self):
        self.sync_link()
        asset = self.root / "mirror/skills/SKILL.md"
        asset.write_text("local customization\n")
        old_entry = yaml.safe_load((self.root / "mirror/.orbit-manifest.yaml").read_text())["dependencies"]
        old_entry = next(d for d in old_entry if d["name"] == "skills")
        self.config["dependencies"][0]["vendor"] = False
        self.save()
        (self.upstream / "skills/alpha/SKILL.md").write_text("upstream v2\n")
        (self.upstream / "commands/build.md").write_text("upstream build v2\n")
        self.commit("upstream v2")
        self.orbit("vendor", "sync")
        self.assertEqual(asset.read_text(), "local customization\n")
        self.assertEqual((self.root / "mirror/commands/build.md").read_text(), "upstream build v2\n")
        entries = yaml.safe_load((self.root / "mirror/.orbit-manifest.yaml").read_text())["dependencies"]
        self.assertEqual(next(d for d in entries if d["name"] == "skills"), old_entry)
        self.orbit("link")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "local customization\n")
        self.orbit("clean", "--yes")
        for dep in self.config["dependencies"]:
            dep["vendor"] = False
        self.save()
        self.orbit("check")

    def test_disabled_vendors_skip_unavailable_sources_and_missing_assets_fail_link(self):
        self.config["dependencies"] = [dict(name="disabled", source=str(self.root / "missing.git"),
                                             ref="main", mirror_dir="skills/disabled",
                                             vendor=False, link=True)]
        self.save()
        self.orbit("vendor", "sync")
        self.orbit("vendor", "sync", "--name", "disabled")
        self.assertFalse((self.root / "cache").exists())
        self.assertFalse((self.root / "mirror").exists())
        self.orbit("link", success=False)

    def test_name_and_label_lists_update_only_selected_assets(self):
        self.sync_link()
        for flag in ("--name", "--label"):
            for values in (("skills,workflows",), ("skills", "workflows"), (" skills ,workflows,skills ",)):
                with self.subTest(flag=flag, values=values):
                    (self.root / "mirror/scripts/run.sh").write_text("keep unselected\n")
                    (self.root / "mirror/skills/SKILL.md").write_text("replace selected\n")
                    args = ["vendor", "sync"]
                    for value in values:
                        args.extend((flag, value))
                    result = self.orbit(*args)
                    self.assertIn("assembled 2 dependencies", result.stdout)
                    self.assertEqual((self.root / "mirror/skills/SKILL.md").read_text(), "alpha-v1\n")
                    self.assertEqual((self.root / "mirror/scripts/run.sh").read_text(), "keep unselected\n")
        self.orbit("link", "--name", "skills")
        self.orbit("check", "--name", "skills")
        self.orbit("sync", "--name", "skills")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "alpha-v1\n")
        self.assertNotIn("scripts", self.git(self.remote, "ls-tree", "--name-only", "main").splitlines())

    def test_invalid_selectors_fail_before_downloading(self):
        for flag in ("--name", "--label"):
            for value in ("", ",skills", "skills,", "skills,,workflows", "skills，workflows", "skills,   "):
                with self.subTest(flag=flag, value=value):
                    result = self.orbit("vendor", "sync", flag, value, success=False)
                    self.assertTrue("empty" in result.stderr or "comma" in result.stderr, result.stderr)
        for args in (("--name", "skills,typo"), ("--name", "skills", "--label", "skills")):
            self.orbit("vendor", "sync", *args, success=False)
        self.assertFalse((self.root / "cache").exists())
        self.assertFalse((self.root / "mirror").exists())

    def test_operational_chain_publishes_edits_and_preserves_links_after_clean(self):
        self.path.unlink()
        self.orbit("init", "--path", str(self.root), explicit=False)
        self.assertTrue(self.path.is_file())
        self.save()
        self.sync_link()
        report = json.loads(self.orbit("check").stdout)
        self.assertEqual(report["status"], "ok")
        self.assertEqual(report["issues"], [])
        linked = self.root / "workspace/skills/SKILL.md"
        linked.write_bytes(b"local operational edit\n")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "local operational edit\n")
        cache = self.cache_path(self.config["dependencies"][0])
        self.orbit("clean", "--yes")
        self.assertFalse(cache.exists())
        self.assertTrue((self.root / "workspace/skills").is_symlink())
        self.assertEqual(linked.read_bytes(), b"local operational edit\n")

    def test_operational_check_reports_missing_wrong_links_and_dirty_cache(self):
        self.sync_link()
        target = self.root / "workspace/skills"
        target.unlink()
        result = json.loads(self.orbit("check", success=False).stdout)
        self.assertTrue(any(issue.startswith("incorrect or missing link: ") and
                            pathlib.Path(issue.removeprefix("incorrect or missing link: ")).resolve() == target.resolve()
                            for issue in result["issues"]), result)
        foreign = self.root / "foreign"
        foreign.mkdir()
        os.symlink(foreign, target, target_is_directory=True)
        result = json.loads(self.orbit("check", success=False).stdout)
        self.assertTrue(any(issue.startswith("incorrect or missing link: ") and
                            pathlib.Path(issue.removeprefix("incorrect or missing link: ")).resolve() == target.resolve()
                            for issue in result["issues"]), result)
        self.orbit("link", "--force")
        (self.cache_path(self.config["dependencies"][0]) / "skills/alpha/SKILL.md").write_text("dirty cache")
        result = json.loads(self.orbit("check", success=False).stdout)
        self.assertEqual(result["status"], "differences")
        self.assertTrue(any("dirty cache" in issue for issue in result["issues"]), result)

    def test_clean_labels_and_unsupported_all_preserve_unselected_cache(self):
        self.config["dependencies"][0]["version"] = self.first
        self.save()
        self.sync_link()
        selected = self.cache_path(self.config["dependencies"][0])
        other = self.cache_path(self.config["dependencies"][1])
        result = self.orbit("clean", "--all", "--yes", success=False)
        self.assertIn("unknown flag: --all", result.stderr)
        self.assertTrue(selected.is_dir())
        self.assertTrue(other.is_dir())
        self.orbit("clean", "--label", "skills", "--yes")
        self.assertFalse(selected.exists())
        self.assertTrue(other.is_dir())
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_clean_rejects_selected_cache_with_invalid_ownership(self):
        self.sync_link()
        cache = self.cache_path(self.config["dependencies"][0])
        state_path = self.root / "cache/.state" / (cache.name + ".json")
        state = json.loads(state_path.read_text())
        state["Source"] = "unrelated synthetic owner"
        state_path.write_text(json.dumps(state))
        result = self.orbit("clean", "--label", "skills", "--yes", success=False)
        self.assertIn("ownership mismatch", result.stderr)
        self.assertEqual((cache / "skills/alpha/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_relative_escaping_symlink_is_not_copied_into_mirror(self):
        outside = self.upstream / "outside-secret"
        outside.write_text("must remain outside selected asset")
        os.symlink("../../outside-secret", self.upstream / "skills/alpha/escape")
        self.commit("relative escape fixture")
        result = self.orbit("vendor", "sync", success=False)
        self.assertIn("symlink escapes selected asset", result.stderr)
        self.assertFalse((self.root / "mirror").exists())
        self.assertEqual(outside.read_text(), "must remain outside selected asset")

    def test_update_help_and_argument_validation_need_no_configuration_or_git(self):
        self.path.unlink()
        home = self.root / "empty-home"
        self.env.update(PATH="", HOME=str(home), USERPROFILE=str(home))
        for command in (("update", "--help"), ("update", "check", "--help")):
            result = self.orbit(*command, explicit=False)
            self.assertIn("stable", result.stdout)
        result = self.orbit("update", "check", "unexpected", explicit=False, success=False)
        self.assertIn("unexpected", result.stderr)
        self.assertFalse((self.root / "cache").exists())
        self.assertFalse(home.exists())

    def test_version_requires_neither_configuration_nor_git(self):
        self.path.unlink()
        self.env.update(PATH=str(self.root / "no-programs"),
                        HOME=str(self.root / "empty-home"), USERPROFILE=str(self.root / "empty-home"))
        result = self.orbit("--version", explicit=False)
        output = result.stdout.lower()
        for field in ("orbit", "commit:", "built:", "go:"):
            self.assertIn(field, output)
        expected = os.environ.get("ORBIT_EXPECT_VERSION", "dev")
        self.assertEqual(result.stdout.splitlines()[0], "orbit " + expected)
        expected_commit = os.environ.get("ORBIT_EXPECT_COMMIT")
        if expected_commit:
            self.assertEqual([line for line in result.stdout.splitlines() if line.startswith("commit: ")],
                             ["commit: " + expected_commit])
        self.assertFalse((self.root / "cache").exists())

    def test_help_explains_the_public_commands(self):
        output = self.orbit("--help", explicit=False).stdout
        for command in ("init", "vendor", "sync", "link", "clean"):
            self.assertIn(command, output)
        self.orbit("mirror", success=False)

    def test_vendor_sync_materializes_all_categories_without_commit_or_push(self):
        self.orbit("vendor", "sync")
        for relative, expected in (("skills/SKILL.md", b"alpha-v1\n"),
                                   ("workflows/review.md", b"review-v1\n"),
                                   ("scripts/run.sh", b"#!/bin/sh\r\necho fixture\r\n"),
                                   ("commands/build.md", b"build-v1\n")):
            self.assertEqual((self.root / "mirror" / relative).read_bytes(), expected)
        self.assertEqual(self.mirror_head(), "")
        self.assertEqual(self.git(self.root / "mirror", "rev-parse", "--verify", "HEAD",
                                  success=False), "")
        self.assertFalse((self.root / "workspace").exists())

    def test_vendor_downloads_only_selected_ref_without_unrelated_history(self):
        self.git(self.upstream, "branch", "unrelated")
        (self.upstream / "skills/alpha/SKILL.md").write_text("new selected snapshot\n")
        self.commit("selected branch update")
        self.orbit("vendor", "sync", "--label", "skills")
        cache = next(p for p in (self.root / "cache").iterdir() if not p.name.startswith("."))
        self.assertEqual(self.git(cache, "rev-list", "--count", "HEAD").strip(), "1",
                         "asset downloads must not fetch the entire upstream history")
        refs = self.git(cache, "for-each-ref", "--format=%(refname)", "refs/orbit").splitlines()
        self.assertEqual(refs, ["refs/orbit/heads/main"])

    def test_sync_publishes_local_assets_without_upstream_or_cache(self):
        self.orbit("vendor", "sync")
        self.upstream.rename(self.root / "offline-upstream")
        self.remove_fixture_tree(self.root / "cache")
        (self.root / "mirror/skills/SKILL.md").write_text("local publication\n")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"),
                         "local publication\n")
        self.assertFalse((self.root / "cache").exists())

    def test_sync_requires_local_assets_and_never_downloads_them(self):
        self.orbit("sync", success=False)
        self.assertFalse((self.root / "cache").exists())
        self.assertFalse((self.root / "mirror").exists())
        self.assertEqual(self.mirror_head(), "")

    def test_sync_never_uses_dependency_names_as_validation_paths(self):
        scratch = self.root / "scratch"
        scratch.mkdir()
        self.env.update(TMPDIR=str(scratch), TMP=str(scratch), TEMP=str(scratch))
        for name, escaped in (("../../escaped-posix", "escaped-posix"),
                              (r"..\..\escaped-windows", "escaped-windows")):
            with self.subTest(name=name):
                self.config["dependencies"][0]["name"] = name
                self.save()
                self.orbit("vendor", "sync")
                self.orbit("sync")
                self.assertFalse((self.root / escaped).exists(),
                                 "dependency names must not direct validation writes outside the temporary directory")
                self.assertEqual(list(scratch.glob("orbit-publish-*")), [],
                                 "Orbit validation workspaces must be cleaned up")
                self.assertEqual(list(scratch.rglob("SKILL.md")), [],
                                 "asset copies must not escape into the temporary directory")
                self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "alpha-v1\n")

    def test_sync_dependency_name_cannot_collide_with_validation_manifest(self):
        self.config["dependencies"][0]["name"] = "manifest.yaml"
        self.save()
        self.orbit("vendor", "sync")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "alpha-v1\n")

    def test_label_sync_keeps_unselected_manifest_and_assets_unpublished(self):
        self.orbit("vendor", "sync")
        self.orbit("sync", "--label", "skills")
        files = self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines()
        self.assertIn("skills/SKILL.md", files)
        self.assertNotIn("workflows/review.md", files)
        manifest = yaml.safe_load(self.git(self.remote, "show", "main:.orbit-manifest.yaml"))
        self.assertEqual([d["name"] for d in manifest["dependencies"]], ["skills"])
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:workflows/review.md"), "review-v1\n")

    def test_partial_publish_rejects_manifest_ownership_overlap(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        previous = self.mirror_head()
        self.config["dependencies"][0]["name"] = "renamed-skills"
        self.save()
        self.orbit("vendor", "sync")
        self.orbit("sync", "--label", "skills", success=False)
        self.assertEqual(self.mirror_head(), previous)
        self.orbit("sync")
        manifest = yaml.safe_load(self.git(self.remote, "show", "main:.orbit-manifest.yaml"))
        self.assertIn("renamed-skills", [d["name"] for d in manifest["dependencies"]])

    def test_links_use_repo_assets_and_survive_cache_cleanup(self):
        self.orbit("vendor", "sync")
        self.orbit("link")
        self.assertEqual((self.root / "workspace/skills").resolve(),
                         (self.root / "mirror/skills").resolve())
        self.orbit("clean", "--yes")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")
        self.orbit("link")

    def use_native_short_cache_path(self):
        if os.name == "nt":
            import ctypes
            buffer = ctypes.create_unicode_buffer(32768)
            length = ctypes.windll.kernel32.GetShortPathNameW(str(self.root.resolve()), buffer, len(buffer))
            self.assertGreater(length, 0, "Windows must resolve its native path alias")
            self.config["global_cache"] = str(pathlib.Path(buffer.value) / "cache")
            self.save()

    def test_repo_links_replace_matching_old_cache_links_without_force(self):
        self.use_native_short_cache_path()
        self.orbit("vendor", "sync")
        target = self.root / "workspace/skills"
        target.parent.mkdir()
        cache = next(p for p in (self.root / "cache").iterdir() if not p.name.startswith("."))
        os.symlink(cache.resolve() / "skills/alpha", target, target_is_directory=True)
        self.orbit("link", "--label", "skills")
        self.assertEqual(target.resolve(), (self.root / "mirror/skills").resolve())

    def test_sync_rejects_local_nested_git_without_staging_or_pushing_it(self):
        self.orbit("vendor", "sync")
        nested = self.root / "mirror/skills/nested"
        nested.mkdir()
        self.git(nested, "init", "--initial-branch=main")
        (nested / "file").write_text("nested repository")
        self.git(nested, "add", ".")
        self.git(nested, "commit", "-m", "nested repository")
        self.orbit("sync", success=False)
        self.assertEqual(self.mirror_head(), "")
        self.assertEqual(self.git(self.root / "mirror", "diff", "--cached", "--name-only"), "")

    def test_init_creates_template_and_preserves_existing_configuration(self):
        folder = self.root / "new-project"
        folder.mkdir()
        self.orbit("init", "--path", str(folder), explicit=False)
        path = folder / "orbit.yaml"
        self.assertEqual(yaml.safe_load(path.read_text())["version"], "1.0")
        before = path.read_bytes()
        self.orbit("init", "--path", str(folder), explicit=False, success=False)
        self.assertEqual(path.read_bytes(), before)

    def test_unknown_config_field_fails_before_cache_is_created(self):
        self.config["typo"] = "must not be ignored"
        self.save()
        self.orbit("vendor", "sync", success=False)
        self.assertFalse((self.root / "cache").exists())

    def test_parent_config_discovery_uses_config_relative_paths(self):
        self.config["global_cache"] = "./cache"
        self.config["link_targets"]["skills"] = ["./workspace/skills"]
        self.save()
        child = self.root / "nested" / "child"
        child.mkdir(parents=True)
        self.orbit("vendor", "sync", cwd=child, explicit=False)
        self.orbit("link", cwd=child, explicit=False)
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")
        self.assertFalse((child / "workspace").exists())

    def test_vendor_sync_does_not_implicitly_link_or_publish(self):
        self.orbit("vendor", "sync")
        self.assertFalse((self.root / "workspace").exists())
        self.assertTrue((self.root / "mirror/skills/SKILL.md").is_file())
        self.assertEqual(self.mirror_head(), "")

    def test_all_four_categories_link_only_the_selected_subdirectories(self):
        self.sync_link()
        for category, relative, expected in (
            ("skills", "SKILL.md", b"alpha-v1\n"),
            ("workflows", "review.md", b"review-v1\n"),
            ("scripts", "run.sh", b"#!/bin/sh\r\necho fixture\r\n"),
            ("commands", "build.md", b"build-v1\n"),
        ):
            target = self.root / "workspace" / category
            self.assertTrue(target.is_symlink(), f"{category} was copied rather than linked")
            self.assertEqual((target / relative).read_bytes(), expected)
            self.assertFalse((target / "unrelated.txt").exists())

    def test_multiple_link_targets_share_the_same_asset(self):
        second = self.root / "second-tool" / "skills"
        self.config["link_targets"]["skills"].append(str(second))
        self.save()
        self.sync_link()
        self.assertTrue(second.is_symlink())
        self.assertEqual(second.resolve(), (self.root / "workspace/skills").resolve())

    def test_global_targets_append_asset_suffix_and_link_false_disables_only_linking(self):
        self.config["dependencies"][0]["mirror_dir"] = "skills/alpha"
        self.config["dependencies"][1]["link"] = False
        self.save()
        self.sync_link()
        self.assertTrue((self.root / "workspace/skills/alpha").is_symlink())
        self.assertFalse((self.root / "workspace/workflows").exists())
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:workflows/review.md"), "review-v1\n")

    def test_default_init_and_config_discovery_use_orbit_home_directory(self):
        fake_home = self.root / "home"
        fake_home.mkdir()
        self.env.update(HOME=str(fake_home), USERPROFILE=str(fake_home))
        self.orbit("init", explicit=False)
        path = fake_home / ".orbit/orbit.yaml"
        self.assertTrue(path.is_file())
        self.assertEqual(yaml.safe_load(path.read_text())["global_cache"], "~/.orbit/cache")
        path.write_bytes(self.path.read_bytes())
        child = self.root / "without-project-config"
        child.mkdir()
        # Remove the nearer project config so only the global config can satisfy lookup.
        self.path.unlink()
        self.orbit("vendor", "sync", explicit=False, cwd=child)
        self.orbit("link", explicit=False, cwd=child)
        self.assertTrue((self.root / "workspace/skills").is_symlink())

    def test_link_refuses_symlink_parent_without_writing_into_its_destination(self):
        outside = self.root / "outside"
        outside.mkdir()
        os.symlink(outside, self.root / "workspace", target_is_directory=True)
        self.orbit("vendor", "sync")
        self.orbit("link", success=False)
        self.assertEqual(list(outside.iterdir()), [])

    def test_clean_removes_only_owned_cache_and_preserves_assembled_links(self):
        self.sync_link()
        sentinel = self.root / "cache/unmanaged"
        sentinel.mkdir()
        (sentinel / "keep").write_text("keep")
        self.orbit("clean", "--yes")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")
        self.assertEqual((sentinel / "keep").read_text(), "keep")
        self.orbit("vendor", "sync")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_label_union_is_case_sensitive_and_zero_matches_fail(self):
        self.orbit("vendor", "sync", "--label", "skills", "--label", "scripts")
        self.orbit("link", "--label", "skills", "--label", "scripts")
        self.assertTrue((self.root / "workspace/skills/SKILL.md").is_file())
        self.assertTrue((self.root / "workspace/scripts/run.sh").is_file())
        self.assertFalse((self.root / "workspace/workflows").exists())
        self.orbit("vendor", "sync", "--label", "SKILLS", success=False)

    def test_branch_updates_but_commit_pinned_dependency_stays_unchanged(self):
        pinned = dict(self.config["dependencies"][0], name="pinned", version=self.first,
                      mirror_dir="pinned", link=True)
        self.config["link_targets"]["pinned"] = [str(self.root / "workspace/pinned")]
        self.config["dependencies"].append(pinned)
        self.save()
        self.sync_link()
        (self.upstream / "skills/alpha/SKILL.md").write_bytes(b"alpha-v2\n")
        self.commit("fixture v2")
        self.orbit("vendor", "sync")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v2\n")
        self.assertEqual((self.root / "workspace/pinned/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_sync_discards_cache_edits_untracked_and_ignored_files_only(self):
        self.sync_link()
        target = self.root / "workspace/skills"
        (target / "SKILL.md").write_text("local edit must be discarded")
        (target / "extra.txt").write_text("untracked")
        (target / "ignored.tmp").write_text("ignored")
        sentinel = self.root / "outside.txt"
        sentinel.write_text("keep")
        self.orbit("vendor", "sync")
        self.assertEqual((target / "SKILL.md").read_bytes(), b"alpha-v1\n")
        self.assertFalse((target / "extra.txt").exists())
        self.assertFalse((target / "ignored.tmp").exists())
        self.assertEqual(sentinel.read_text(), "keep")

    def test_partial_sync_failure_continues_other_dependencies_and_returns_failure(self):
        self.config["dependencies"].insert(0, dict(
            name="unavailable", source=str(self.root / "missing.git"), version="main",
            mirror_dir="unavailable", labels=["bad"], link=False))
        self.save()
        self.orbit("vendor", "sync", success=False)
        self.assertFalse((self.root / "mirror").exists())
        good_cache = self.cache_path(self.config["dependencies"][1])
        self.assertEqual((good_cache / "skills/alpha/SKILL.md").read_bytes(), b"alpha-v1\n")
        state = json.loads((self.root / "cache/.state" / (good_cache.name + ".json")).read_text())
        self.assertEqual(state["Commit"], self.first)
        self.orbit("vendor", "sync", "--label", "skills")
        self.orbit("link", "--label", "skills")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_missing_cache_link_does_not_download(self):
        self.orbit("link", success=False)
        self.assertFalse((self.root / "cache").exists())

    def test_force_link_never_deletes_real_directory_or_file(self):
        self.orbit("vendor", "sync")
        target = self.root / "workspace/skills"
        target.mkdir(parents=True)
        (target / "user.txt").write_text("keep user content")
        self.orbit("link", "--label", "skills", success=False)
        self.orbit("link", "--label", "skills", "--force", success=False)
        self.assertEqual((target / "user.txt").read_text(), "keep user content")
        (target / "user.txt").unlink()
        target.rmdir()
        target.write_text("real file")
        self.orbit("link", "--label", "skills", "--force", success=False)
        self.assertEqual(target.read_text(), "real file")

    def test_correct_link_is_idempotent(self):
        self.sync_link()
        target = self.root / "workspace/skills"
        before = target.lstat()
        self.orbit("link")
        self.assertEqual(target.lstat().st_mtime_ns, before.st_mtime_ns)

    def test_disabling_link_removes_matching_targets_only_for_selected_labels(self):
        second = self.root / "second-tool/skills"
        self.config["link_targets"]["skills"].append(str(second))
        self.save()
        self.sync_link()
        target = self.root / "workspace/skills"
        source = target.resolve()
        second.unlink()
        os.symlink(os.path.relpath(target.resolve(), second.parent.resolve()), second, target_is_directory=True)
        for dep in self.config["dependencies"]:
            dep["link"] = False
        self.save()
        self.orbit("link", "--label", "skills")
        self.assertFalse(target.is_symlink())
        self.assertFalse(second.is_symlink())
        self.assertEqual((source / "SKILL.md").read_bytes(), b"alpha-v1\n")
        self.assertTrue((self.root / "workspace/workflows").is_symlink())
        self.orbit("link", "--label", "skills")
        self.config["dependencies"][0]["link"] = True
        self.save()
        self.orbit("link", "--label", "skills")
        self.assertEqual(target.resolve(), source)
        self.assertEqual(second.resolve(), source)

    def test_disabling_link_preserves_foreign_links_and_real_entries_even_with_force(self):
        roots = [self.root / f"tool-{n}/skills" for n in range(3)]
        self.config["link_targets"]["skills"].extend(map(str, roots))
        self.save()
        self.sync_link()
        target = self.root / "workspace/skills"
        target.unlink()
        foreign = self.root / "foreign"
        foreign.mkdir()
        (foreign / "keep").write_text("foreign content")
        os.symlink(foreign, target, target_is_directory=True)
        roots[0].unlink()
        roots[0].mkdir()
        (roots[0] / "keep").write_text("real directory")
        roots[1].unlink()
        roots[1].write_text("real file")
        self.config["dependencies"][0]["link"] = False
        self.save()
        self.orbit("link", "--label", "skills", "--force")
        self.assertEqual(target.resolve(), foreign.resolve())
        self.assertEqual((foreign / "keep").read_text(), "foreign content")
        self.assertEqual((roots[0] / "keep").read_text(), "real directory")
        self.assertEqual(roots[1].read_text(), "real file")
        self.assertFalse(roots[2].is_symlink())

    def test_disabling_link_removes_dangling_links_without_recreating_cache(self):
        self.sync_link()
        self.remove_fixture_tree(self.root / "cache")
        self.remove_fixture_tree(self.root / "mirror")
        for category in ("skills", "workflows", "scripts", "commands"):
            target = self.root / "workspace" / category
            self.assertTrue(target.is_symlink())
            self.assertFalse(target.exists(), "cleanup must start with genuinely dangling repo links")
        for dep in self.config["dependencies"]:
            dep["link"] = False
        self.save()
        self.orbit("link")
        for category in ("skills", "workflows", "scripts", "commands"):
            self.assertFalse((self.root / "workspace" / category).is_symlink())
        self.assertFalse((self.root / "cache").exists())

    def test_disabling_link_rejects_symlink_parent_without_deleting_through_it(self):
        self.sync_link()
        outside = self.root / "outside"
        (self.root / "workspace").rename(outside)
        os.symlink(outside, self.root / "workspace", target_is_directory=True)
        for dep in self.config["dependencies"]:
            dep["link"] = False
        self.save()
        self.orbit("link", success=False)
        for category in ("skills", "workflows", "scripts", "commands"):
            self.assertTrue((outside / category).is_symlink())

    def test_disabled_missing_links_do_not_require_sync_or_create_directories(self):
        for dep in self.config["dependencies"]:
            dep["link"] = False
        self.save()
        self.orbit("link")
        self.orbit("link")
        self.assertFalse((self.root / "workspace").exists())
        self.assertFalse((self.root / "cache").exists())

    def test_disabling_link_after_version_change_preserves_old_cache_link(self):
        self.sync_link()
        target = self.root / "workspace/skills"
        cache = next(p for p in (self.root / "cache").iterdir() if not p.name.startswith("."))
        target.unlink()
        os.symlink(cache / "skills/alpha", target, target_is_directory=True)
        old = os.readlink(target)
        self.config["dependencies"][0].update(link=False, version=self.first)
        self.save()
        self.orbit("link", "--label", "skills", "--force")
        self.assertEqual(os.readlink(target), old)

    def test_mirror_publishes_file_bytes_without_nested_git_or_unrelated_content(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        for path, expected in (("skills/SKILL.md", "alpha-v1\n"),
                               ("workflows/review.md", "review-v1\n"),
                               ("commands/build.md", "build-v1\n")):
            self.assertEqual(self.git(self.remote, "show", "main:" + path), expected)
        raw = subprocess.run(["git", "-C", str(self.remote), "show", "main:scripts/run.sh"],
                             capture_output=True, env=self.env, timeout=20, check=True)
        self.assertEqual(raw.stdout, b"#!/bin/sh\r\necho fixture\r\n")
        self.assertEqual((self.root / "mirror/scripts/run.sh").read_bytes(),
                         b"#!/bin/sh\r\necho fixture\r\n")
        if os.name != "nt":
            self.assertEqual((self.root / "mirror/scripts/run.sh").stat().st_mode & 0o111, 0o111,
                             "assembled executable scripts must retain their POSIX executable bits")
        files = self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines()
        self.assertNotIn("unrelated.txt", files)
        self.assertFalse(any(".git" in pathlib.PurePosixPath(p).parts for p in files))
        self.assertIn("skills/.env", files)
        self.assertIn(".orbit-manifest.yaml", files)
        manifest = self.git(self.remote, "show", "main:.orbit-manifest.yaml")
        self.assertIn(self.first, manifest)
        self.assertIn(str(self.upstream), manifest)

    def test_mirror_without_changes_creates_no_commit(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        before = self.mirror_head()
        self.orbit("sync")
        self.assertEqual(self.mirror_head(), before)

    def test_mirror_directory_is_a_literal_git_path_and_never_stages_rollback_copies(self):
        dep = self.config["dependencies"][0]
        dep["mirror_dir"] = "custom/[ab]"
        dep["link"] = False
        self.save()
        self.orbit("vendor", "sync")
        self.orbit("sync")
        (self.upstream / "skills/alpha/SKILL.md").write_text("new asset\n")
        self.commit("new asset")
        self.orbit("vendor", "sync")
        self.orbit("sync")
        files = self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines()
        self.assertFalse(any(p.startswith(".orbit-rollback-") for p in files), files)
        self.assertEqual(self.git(self.remote, "show", "main:custom/[ab]/SKILL.md"), "new asset\n")
        before = self.mirror_head()
        self.orbit("sync")
        self.assertEqual(before, self.mirror_head())

    def test_git_attributes_cannot_silently_transform_mirrored_asset_bytes(self):
        seed = self.root / "seed"
        self.git(self.root, "clone", str(self.remote), str(seed))
        (seed / ".gitattributes").write_text("*.sh text eol=lf\n")
        self.git(seed, "add", ".")
        self.git(seed, "commit", "-m", "existing attributes")
        self.git(seed, "push", "origin", "main")
        self.orbit("vendor", "sync")
        before = self.mirror_head()
        result = self.orbit("sync", success=False)
        self.assertIn("transform", result.stderr.lower())
        self.assertEqual(before, self.mirror_head())
        self.assertEqual(self.git(self.root / "mirror", "diff", "--cached", "--name-only"), "")
        self.assertEqual((self.root / "mirror/scripts/run.sh").read_bytes(), b"#!/bin/sh\r\necho fixture\r\n")

    def test_mirror_skips_dependencies_without_a_mirror_directory_and_accepts_message(self):
        self.orbit("vendor", "sync")
        self.config["dependencies"].append(dict(name="cache-only", source=str(self.upstream),
                                              version=self.first, link=False))
        self.save()
        self.orbit("sync", "--message", "My asset update")
        self.assertEqual(self.git(self.remote, "log", "-1", "--format=%s", "main").strip(),
                         "My asset update")

    def test_dirty_cache_prevents_vendor_assembly_but_not_local_publication(self):
        self.sync_link()
        cache = next(p for p in (self.root / "cache").iterdir() if not p.name.startswith("."))
        (cache / "skills/alpha/SKILL.md").write_text("dirty internal cache")
        (self.root / "workspace/skills/SKILL.md").write_text("publish local repo content")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "publish local repo content")
        self.orbit("vendor", "sync")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_missing_selected_dependency_does_not_publish_partial_snapshot(self):
        # A different ref requires a distinct cache; subdirs of one clone share readiness.
        self.config["dependencies"][1]["version"] = self.first
        self.save()
        self.orbit("vendor", "sync", "--label", "skills")
        self.orbit("sync", success=False)
        self.assertEqual(self.mirror_head(), "")

    def test_label_mirror_preserves_unselected_assets(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        (self.upstream / "skills/alpha/SKILL.md").write_text("alpha-v2\n")
        self.commit("new skills")
        self.orbit("vendor", "sync", "--label", "skills")
        self.orbit("sync", "--label", "skills")
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "alpha-v2\n")
        self.assertEqual(self.git(self.remote, "show", "main:commands/build.md"), "build-v1\n")

    def test_full_mirror_removes_only_previously_managed_removed_dependency(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        self.config["dependencies"] = [d for d in self.config["dependencies"] if d["name"] != "scripts"]
        self.save()
        self.orbit("vendor", "sync")
        self.orbit("sync")
        files = self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines()
        self.assertNotIn("scripts/run.sh", files)
        self.assertIn("commands/build.md", files)

    def test_repo_git_metadata_is_not_stripped_from_source_cache(self):
        self.sync_link()
        target = next(p for p in (self.root / "cache").iterdir() if not p.name.startswith("."))
        self.orbit("sync")
        self.assertEqual(self.git(target, "rev-parse", "HEAD").strip(), self.first)

    def test_external_symlink_is_not_copied_into_mirror(self):
        outside = self.root / "outside-secret"
        outside.write_text("must remain outside")
        os.symlink(str(outside), self.upstream / "skills/alpha/external")
        self.commit("external symlink fixture")
        self.orbit("vendor", "sync", success=False)
        self.assertFalse((self.root / "mirror").exists())
        self.assertEqual(self.mirror_head(), "")
        self.assertEqual(outside.read_text(), "must remain outside")

    def test_internal_symlink_is_preserved_as_a_symlink(self):
        self.use_native_short_cache_path()
        os.symlink("SKILL.md", self.upstream / "skills/alpha/alias")
        self.commit("internal symlink fixture")
        self.orbit("vendor", "sync")
        self.orbit("sync")
        alias = self.root / "mirror/skills/alias"
        self.assertTrue(alias.is_symlink())
        self.assertEqual(os.readlink(alias), "SKILL.md")

    def test_internal_directory_symlink_preserves_type_before_target_is_copied(self):
        directory = self.upstream / "skills/alpha/z-directory"
        directory.mkdir()
        (directory / "content").write_bytes(b"directory target\n")
        os.symlink("z-directory", self.upstream / "skills/alpha/a-alias", target_is_directory=True)
        self.commit("internal directory symlink fixture")
        self.orbit("vendor", "sync")
        alias = self.root / "mirror/skills/a-alias"
        self.assertTrue(alias.is_symlink())
        self.assertTrue(alias.is_dir())
        self.assertEqual(os.readlink(alias), "z-directory")
        self.assertEqual((alias / "content").read_bytes(), b"directory target\n")
        self.orbit("sync")
        self.assertEqual(self.git(self.remote, "show", "main:skills/a-alias"), "z-directory")

    def test_ignore_rules_exclude_files_without_implicitly_excluding_dotenv(self):
        (self.root / ".orbitignore").write_text("SKILL.md\n")
        self.orbit("vendor", "sync")
        self.orbit("sync")
        files = self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines()
        self.assertNotIn("skills/SKILL.md", files)
        self.assertIn("skills/.env", files)

    def test_branch_tag_ambiguity_is_an_error(self):
        self.git(self.upstream, "tag", "main")
        self.config["dependencies"][0]["version"] = "main"
        self.save()
        self.orbit("vendor", "sync", "--label", "skills", success=False)

    def test_moved_tag_is_not_silently_updated(self):
        self.git(self.upstream, "tag", "stable")
        self.config["dependencies"][0]["version"] = "refs/tags/stable"
        self.save()
        self.orbit("vendor", "sync", "--label", "skills")
        (self.upstream / "skills/alpha/SKILL.md").write_text("new tag target\n")
        self.commit("move tag target")
        self.git(self.upstream, "tag", "-f", "stable")
        self.orbit("vendor", "sync", "--label", "skills", success=False)

    def test_cached_tag_cannot_silently_become_a_same_named_branch(self):
        self.git(self.upstream, "tag", "stable")
        self.config["dependencies"][0]["version"] = "stable"
        self.save()
        self.orbit("vendor", "sync", "--label", "skills")
        self.orbit("link", "--label", "skills")
        self.git(self.upstream, "tag", "-d", "stable")
        (self.upstream / "skills/alpha/SKILL.md").write_text("wrong replacement\n")
        self.commit("replace tag by branch")
        self.git(self.upstream, "branch", "stable")
        self.orbit("vendor", "sync", "--label", "skills", success=False)
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

    def test_replaced_annotated_tag_is_rejected_even_when_commit_is_unchanged(self):
        self.git(self.upstream, "tag", "-a", "stable", "-m", "original tag")
        self.config["dependencies"][0]["version"] = "refs/tags/stable"
        self.save()
        self.orbit("vendor", "sync", "--label", "skills")
        self.git(self.upstream, "tag", "-f", "-a", "stable", "-m", "different tag object")
        self.orbit("vendor", "sync", "--label", "skills", success=False)

    def test_lfs_pointer_is_reported_as_unsupported(self):
        (self.upstream / ".gitattributes").write_text("*.bin filter=lfs diff=lfs merge=lfs -text\n")
        (self.upstream / "skills/alpha/payload.bin").write_text(
            "version https://git-lfs.github.com/spec/v1\noid sha256:" + "a" * 64 + "\nsize 123\n")
        self.commit("LFS pointer")
        self.orbit("vendor", "sync", "--label", "skills", success=False)

    def test_submodule_is_reported_as_unsupported(self):
        (self.upstream / ".gitmodules").write_text(
            '[submodule "embedded"]\n\tpath = embedded\n\turl = ' + self.upstream.as_posix() + '\n')
        self.git(self.upstream, "add", ".gitmodules")
        self.git(self.upstream, "update-index", "--add", "--cacheinfo", "160000," + self.first + ",embedded")
        self.git(self.upstream, "commit", "-m", "submodule fixture")
        self.orbit("vendor", "sync", success=False)

    def test_force_without_confirmation_does_not_publish(self):
        self.orbit("vendor", "sync")
        self.orbit("sync", "--force", success=False, stdin="")
        self.assertEqual(self.mirror_head(), "")

    def test_failed_push_is_retried_even_if_snapshot_has_not_changed(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        (self.upstream / "skills/alpha/SKILL.md").write_text("alpha-v2\n")
        self.commit("fixture v2")
        self.orbit("vendor", "sync")
        hook = self.remote / "hooks/pre-receive"
        hook.write_bytes(b"#!/bin/sh\nexit 1\n")
        hook.chmod(0o755)
        old_head = self.mirror_head()
        self.orbit("sync", success=False)
        self.assertEqual(self.mirror_head(), old_head)
        local_commit = self.git(self.root / "mirror", "rev-parse", "HEAD").strip()
        self.assertNotEqual(local_commit, old_head)
        hook.unlink()
        self.orbit("sync")
        self.assertEqual(self.mirror_head(), local_commit)
        self.assertEqual(self.git(self.remote, "show", "main:skills/SKILL.md"), "alpha-v2\n")

    def test_diverged_history_is_not_automatically_merged_or_rebased(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        local = self.root / "mirror"
        (local / "local.txt").write_text("local commit")
        self.git(local, "add", ".")
        self.git(local, "commit", "-m", "local divergence")
        local_head = self.git(local, "rev-parse", "HEAD").strip()
        other = self.root / "other-clone"
        self.git(self.root, "clone", str(self.remote), str(other))
        (other / "external.txt").write_text("remote commit")
        self.git(other, "add", ".")
        self.git(other, "commit", "-m", "remote divergence")
        self.git(other, "push", "origin", "main")
        remote_head = self.mirror_head()
        self.orbit("sync", success=False)
        self.assertEqual(self.mirror_head(), remote_head)
        self.assertEqual(self.git(local, "rev-parse", "HEAD").strip(), local_head)

    def test_concurrent_sync_does_not_corrupt_shared_cache(self):
        command = [str(self.binary), "--config", str(self.path), "vendor", "sync"]
        processes = [subprocess.Popen(command, cwd=self.root, env=self.env,
                                      stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                     for _ in range(2)]
        try:
            results = []
            for process in processes:
                out, err = process.communicate(timeout=30)
                results.append((process.returncode, out + err))
            self.assertTrue(any(code == 0 for code, _ in results), results)
            self.orbit("link")
            self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")
        finally:
            for process in processes:
                if process.poll() is None:
                    process.kill()
                    process.communicate()

    def test_yes_alone_never_authorizes_history_rewrite(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        other = self.root / "other-clone"
        self.git(self.root, "clone", str(self.remote), str(other))
        (other / "external.txt").write_text("remote history must remain")
        self.git(other, "add", ".")
        self.git(other, "commit", "-m", "external update")
        self.git(other, "push", "origin", "main")
        external = self.mirror_head()
        self.orbit("sync", "--yes")
        self.assertEqual(self.git(self.remote, "merge-base", external, "main").strip(), external)

    def test_dirty_mirror_worktree_is_preserved(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        local = self.root / "mirror/local-user-file"
        local.write_text("keep")
        before = self.mirror_head()
        self.orbit("sync", success=False)
        self.assertEqual(local.read_text(), "keep")
        self.assertEqual(self.mirror_head(), before)

    def test_sync_preserves_untracked_finder_metadata_without_publishing_it(self):
        self.orbit("vendor", "sync")
        local = self.root / "mirror"
        metadata = local / ".DS_Store"
        metadata.write_bytes(b"Finder metadata")
        for ignored in (False, True):
            if ignored:
                (local / ".git/info/exclude").write_text(".DS_Store\n")
            self.orbit("sync")
            self.assertEqual(metadata.read_bytes(), b"Finder metadata")
            self.assertNotIn(".DS_Store", self.git(self.remote, "ls-tree", "-r", "--name-only", "main").splitlines())
        (local / "unrelated.txt").write_text("preserve unrelated work")
        result = self.orbit("sync", success=False)
        self.assertIn("unmanaged working tree change at unrelated.txt", result.stderr)

    def test_sync_still_rejects_tracked_or_staged_finder_metadata_changes(self):
        self.orbit("vendor", "sync")
        self.orbit("sync")
        local = self.root / "mirror"
        metadata = local / ".DS_Store"
        metadata.write_bytes(b"tracked metadata")
        self.git(local, "add", ".DS_Store")
        self.assertIn("staged changes", self.orbit("sync", success=False).stderr)
        self.git(local, "commit", "-m", "Track metadata explicitly")
        metadata.write_bytes(b"modified metadata")
        self.assertIn("unmanaged working tree change at .DS_Store", self.orbit("sync", success=False).stderr)

    def test_clean_declined_confirmation_preserves_all_assets(self):
        self.sync_link()
        self.orbit("clean", success=None, stdin="n\n")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")

class GitSubprocessContracts(OrbitCLIFixture):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.shim_temp = tempfile.TemporaryDirectory(prefix="orbit-git-shim-")
        cls.addClassCleanup(cls.shim_temp.cleanup)
        cls.git_shim = pathlib.Path(cls.shim_temp.name) / ("git.exe" if os.name == "nt" else "git")
        result = subprocess.run(["go", "build", "-o", str(cls.git_shim),
                                 str(ROOT / "tests/helpers/gitshim/main.go")],
                                cwd=ROOT, capture_output=True, text=True, timeout=60)
        if result.returncode != 0:
            raise AssertionError("native Git helper build failed: " + result.stdout + result.stderr)

    def install_git_shim(self, mode, marker=None):
        cls = type(self)
        real_git = shutil.which("git", path=self.env["PATH"])
        self.assertIsNotNone(real_git, "Git is a required test prerequisite")
        self.env.update(ORBIT_TEST_REAL_GIT=real_git, ORBIT_TEST_GIT_MODE=mode,
                        ORBIT_TEST_GIT_MARKER=str(marker or ""))
        self.env["PATH"] = str(cls.git_shim.parent) + os.pathsep + self.env["PATH"]

    def test_git_timeout_terminates_child_processes_and_releases_cache_lock(self):
        self.orbit("vendor", "sync")
        marker = self.root / "orphan-child"
        self.install_git_shim("timeout-fetch", marker)
        command = [str(self.binary), "--config", str(self.path), "vendor", "sync", "--timeout", "5s"]
        started = time.monotonic()
        process = subprocess.Popen(command, cwd=self.root, env=self.env,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        try:
            ready = pathlib.Path(str(marker) + ".started")
            readiness_deadline = time.monotonic() + 10
            while not ready.exists() and process.poll() is None and time.monotonic() < readiness_deadline:
                time.sleep(0.02)
            self.assertTrue(ready.exists(), "timeout fixture must start its child within the readiness bound")
            # Release only a confirmed child; its write is twelve seconds away,
            # leaving ample margin for the five-second deadline and tree teardown.
            pathlib.Path(str(marker) + ".release").write_bytes(b"release\n")
            released = time.monotonic()
            out, err = process.communicate(timeout=20)
            self.assertNotEqual(process.returncode, 0, out + err)
            self.assertIn("git fetch:", err)
            self.assertGreaterEqual(time.monotonic() - started, 4.5,
                                    "fetch must fail on its configured deadline, not an immediate helper error")
            time.sleep(max(0, released + 13 - time.monotonic()))
            self.assertFalse(marker.exists(), "timed-out Git must not leave a child writing later")
        finally:
            if process.poll() is None:
                process.kill()
                process.communicate(timeout=30)
        self.env.pop("ORBIT_TEST_GIT_MODE")
        self.orbit("vendor", "sync")

    def test_failed_personal_clone_leaves_no_checkout_and_retry_succeeds(self):
        self.install_git_shim("fail-clone")
        result = self.orbit("vendor", "sync", success=False)
        self.assertIn("injected clone failure", result.stderr)
        self.assertFalse((self.root / "mirror").exists(),
                         "failed initial clone must not leave a checkout requiring manual deletion")
        self.assertEqual(list(self.root.glob(".mirror.orbit-clone-*")), [],
                         "failed clone staging directories must be cleaned up")
        self.env.pop("ORBIT_TEST_GIT_MODE")
        self.orbit("vendor", "sync")
        self.orbit("link")
        self.assertEqual((self.root / "workspace/skills/SKILL.md").read_bytes(), b"alpha-v1\n")
        self.assertEqual(self.git(self.root / "mirror", "symbolic-ref", "--short", "HEAD").strip(),
                         "main")

    def test_failed_cache_initialization_can_be_retried_without_manual_deletion(self):
        self.install_git_shim("fail-init")
        self.orbit("vendor", "sync", success=False)
        self.env.pop("ORBIT_TEST_GIT_MODE")
        self.orbit("vendor", "sync")
        self.orbit("link")
        self.assertTrue((self.root / "workspace/skills/SKILL.md").is_file())


if __name__ == "__main__":
    unittest.main()
