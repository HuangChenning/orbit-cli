# Orbit CLI

**One YAML file to distribute Git-backed assets across your developer tools.**

English · [简体中文](README.zh-CN.md)

Go 1.20+ · System Git · Declarative configuration

[Quick start](#quick-start) · [Commands](#commands) · [Publish to GitHub](#publish-to-github) · [Axon migration checks](#check-and-repair-an-axon-migration)

## Overview

Orbit helps developers reuse **skills, workflows, scripts, and commands** across tools and projects. `orbit vendor sync` downloads upstream assets into `~/.orbit/repo`, `orbit link` distributes them through symlinks, and `orbit sync` commits and pushes the local assets to your personal Git repository.

## Example output

Example session from an isolated local Git repository, with the local repository path abbreviated. Two tools' `skill-creator` links resolve to the same assembled directory.

```console
$ orbit vendor sync
synced skill-creator
assembled 1 dependencies into <local-repo>
$ orbit link
$ orbit check
{
  "issues": [],
  "status": "ok",
  "verification": "clean cache and configured symlink destinations"
}
```

## How it works

<img src="assets/readme/asset-flow.svg" width="640" alt="Orbit asset flow: vendor sync downloads upstream Git through an internal cache into the local asset repository; link maps local assets to developer tools; sync commits and pushes local assets to your personal Git remote.">

`vendor sync` downloads and assembles assets locally without committing or pushing. `link` creates or removes tool links. `sync` commits and pushes local assets without downloading upstream or overwriting local content from the cache. Nothing writes back to upstream.

## Quick start

Ensure **Git is on your PATH** and **Go 1.20+** is installed. Build from source on macOS / Linux:

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" ./src
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

Add the PATH setting to your shell startup file to make it persistent. By default, `orbit init` creates `~/.orbit/orbit.yaml` without overwriting an existing configuration.

Create your personal remote repository, then edit this file. Replace `YOUR_ACCOUNT` with your account and declare your first dependency:

```yaml
version: "1.0"
global_cache: "~/.orbit/cache"

orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"

link_targets:
  skills:
    - "~/.claude/skills"
    - "~/.codex/skills"

dependencies:
  - name: "skill-creator"
    source: "https://github.com/anthropics/skills.git"
    version: "refs/heads/main"
    subdir: "skills/skill-creator"
    mirror_dir: "skills/skill-creator"
    link: true
    labels: ["skills"]
```

Then run:

```sh
orbit vendor sync
orbit link
orbit check
```

The downloaded files appear in `~/.orbit/repo/skills/skill-creator`. This example links that directory to `~/.claude/skills/skill-creator` and `~/.codex/skills/skill-creator`. If a real file, directory, or symlinked parent directory occupies a destination, resolve the conflict manually first.

## Commands

| Command | Purpose | Common options |
| --- | --- | --- |
| `orbit init` | Generate configuration without overwriting an existing file | `--path <dir>` |
| `orbit vendor sync` | Download upstream and assemble local assets | `--label <label>`, `--jobs <n>` |
| `orbit link` | Create or remove matching links according to the `link` switch | `--label <label>`, `--force` |
| `orbit sync` | Commit and push local assets to your personal remote | `--label <label>`, `--message <text>` |
| `orbit check` | Check the cache and enabled links | `--fix` with explicit Axon inputs |
| `orbit clean` | Preview and confirm deletion of selected managed caches | `--label <label>`, `--yes` |

Labels are case-sensitive. Repeated `--label` flags use **OR** matching; no matches is an error. Vendor downloads run at most 4 concurrent Git operations by default. Each Git operation has a two-minute timeout, adjustable with `--timeout`.

Vendor downloads fetch a shallow snapshot of the configured version, rather than every upstream branch and its full history. The cache is internal; use the assembled files under `~/.orbit/repo`.

```sh
orbit vendor sync --label skills --label workflows --jobs 4
```

### Clean downloaded caches

Run `orbit link` first if tool links still point to the old cache location. Then remove the managed caches for all dependencies in the current configuration:

```sh
orbit clean
```

The command lists the selected dependencies and asks for confirmation. Use `orbit clean --yes` to skip confirmation, or `orbit clean --label skills` to select a group. No `--all` flag is needed or supported. Caches no longer declared in the configuration are not selected.

Cleanup preserves assembled assets in `~/.orbit/repo` and links pointing there. `orbit sync` can still commit and push those assets without a cache; the next `orbit vendor sync` downloads the removed caches again and refreshes the selected local assets.

## Directories and configuration

In the source repository, Go code lives in `src/main.go` and `src/internal/`. You can keep project skills locally under the root `internal/skills/` directory; the entire root `internal/` directory is ignored by Git. `go.mod` and `go.sum` remain at the repository root; run build commands from there, using `go build -o orbit ./src` or `go run ./src --help`.

| Default path | Purpose |
| --- | --- |
| `~/.orbit/bin/orbit` | Binary built by the installation steps above |
| `~/.orbit/orbit.yaml` | Global configuration |
| `~/.orbit/cache` | Git cache shared by source and version |
| `~/.orbit/repo` | Downloaded assets under `skills/`, `workflows/`, `scripts/`, and `commands/`, according to `mirror_dir` |

Configuration lookup order: `--config <file>` → nearest `orbit.yaml` in the project or its parent directories → global configuration. Relative paths resolve from the configuration file's directory. Paths starting with `~/` are supported.

Declare tool root directories once in the global `link_targets`, grouped by category. Each dependency uses a `link` switch. The first segment of `mirror_dir` selects the category; the remaining path is appended to each tool root.

After changing an existing dependency's `link` to `false`, run `orbit link` again to remove symlinks at the current destinations that still point to that dependency's local asset directory, including dangling links. Matching links to the old cache location are also removed. Cache contents, local assets, real files, directories, and links pointing elsewhere are preserved. Cleanup works even when the cache is missing and respects `--label`. Old links at destinations no longer present in the configuration require manual handling.

| Dependency field | Default or behavior when omitted |
| --- | --- |
| `name`, `source`, `version` | Required; names must be unique |
| `subdir` | Defaults to `.`, using the whole repository |
| `link` | Defaults to `false`; `orbit link` removes matching old links; download and publication remain available |
| `mirror_dir` | Asset path inside the local repo; if omitted, download to cache only; required when linking is enabled |

## Publish to GitHub

The `orbit_repo` section specifies your personal remote and local asset directory. Ensure the remote exists and your Git credentials work:

```yaml
orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"
```

You can omit `local_path`; it defaults to `~/.orbit/repo`. After downloading and reviewing your local assets, publish them with:

```sh
orbit sync
```

- `vendor sync` prepares all selected assets before replacing managed directories. Failed downloads or copy validation leave the previous assembled assets intact. A full vendor refresh removes only stale assets previously managed by Orbit; label-filtered refreshes preserve unselected assets. Local edits in refreshed assets are discarded to match upstream.
- The first vendor operation may clone your personal remote to preserve its history and unmanaged files such as a README. It creates no commit and performs no push. Later downloads do not publish pending local changes.
- Vendor assembly excludes nested `.git` entries and applies Gitignore-style rules from the `.orbitignore` beside your configuration. Internal relative symlinks are preserved; absolute or escaping links are rejected. `.env` is not implicitly excluded. Publication rejects nested Git repositories added locally.
- `sync` publishes the current managed local assets, including your edits, without requiring the cache or contacting upstream dependencies. Unmanaged working-tree changes and existing staged changes must be handled manually. An untracked `.DS_Store` at the repository root is preserved without being published, even when Git ignores it; tracked or staged changes to it still require manual handling. `--label` stages only selected assets and their manifest entries, leaving other local changes pending.
- `.orbit-manifest.yaml` records upstream sources and resolved commits used for assembly; it does not claim that later local edits match those commits. Unchanged content produces no empty commit. A failed push retains the local commit for retry. Git attributes or filters that transform asset bytes cause publication to fail and clear its staging changes while preserving local files.

## Check and repair an Axon migration

Everyday commands use the Orbit configuration. With explicit Axon inputs, check vendor declarations and the Hub's committed assets in all four categories for missing dependencies, tool mappings, source conflicts, and uncovered files:

```sh
orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml"
```

Review the report, then use `--fix` to add omissions with known sources and deduplicate entries:

```sh
orbit check --axon-config "$HOME/.axon/axon.yaml" \
  --vendors "$HOME/.axon/repo/axon.vendors.yaml" --fix
```

Repair saves a byte-for-byte backup first and reports its changes. YAML comments and formatting are normalized. Custom dependencies, labels, and `link: false` settings are preserved. Source conflicts are not automatically overwritten, and the original Axon files are not modified.

Add `--repo "$HOME/.orbit/repo"` to either command to compare the local repository's file inventory and report unconfigured assets. This checks neither file bytes nor versions and does not guess unknown assets' sources.

## Limitations and behavior

| Scenario | Behavior |
| --- | --- |
| Local edits | Vendor sync restores selected caches and managed local assets to upstream; edits through tool links affect the local repo, and sync publishes them |
| Version selection | Branches track upstream; a full SHA pins a commit; ambiguous branch/tag names or changed cached tags are rejected |
| Link conflicts | `link --force` replaces incorrect symlinks only; real files, directories, and symlinked parents require manual handling |
| Diverged personal repository history | Normal push by default, without automatic merge/rebase; `sync --force` requires confirmation, or explicit `--force --yes` in non-interactive use |
| Cache cleanup | `clean --yes` confirms deletion of selected managed caches; assembled assets and their links remain available; `--yes` alone does not authorize a force push |
| Git extensions | Git LFS and submodules are not supported yet |
| Platform verification | Runtime verified on macOS arm64; cross-compilation passed for macOS amd64, Linux amd64, and Windows amd64; native Linux / Windows behavior still needs verification |

Commands exit with `0` on success and a nonzero status on validation differences or operational failure. See `orbit --help` and `orbit <command> --help` for all options.
