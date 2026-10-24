# Orbit CLI

**One YAML file to distribute Git-backed assets across your developer tools.**

English · [简体中文](README.zh-CN.md)

Go 1.20+ · System Git · Declarative configuration

[Quick start](#quick-start) · [Commands](#commands) · [Personal mirror](#mirror-to-a-personal-repository) · [Axon migration checks](#check-and-repair-an-axon-migration)

## Overview

Orbit helps developers reuse **skills, workflows, scripts, and commands** across tools and projects. Sync upstream repositories into a shared cache, distribute selected directories through symlinks, and optionally aggregate them into a personal Git repository.

## Example output

This output comes from an isolated local Git example. Two tools' `skill-creator` links resolve to the same cached directory.

```console
$ orbit sync
synced skill-creator
$ orbit link
$ orbit check
{
  "issues": [],
  "status": "ok",
  "verification": "clean cache and configured symlink destinations"
}
```

## How it works

<img src="assets/readme/asset-flow.svg" width="100%" alt="Orbit asset flow: sync fetches upstream Git into a shared cache; link maps cached directories to developer tools; mirror aggregates the cache into a local repository and pushes to a personal Git remote.">

`sync` updates the cache, `link` creates or removes links according to the configuration, and `mirror` commits and pushes. Run each command independently. The personal mirror never writes back to upstream.

## Quick start

Ensure **Git is on your PATH** and **Go 1.20+** is installed. Build from source on macOS / Linux:

```sh
git clone https://github.com/HuangChenning/orbit-cli.git
cd orbit-cli
mkdir -p "$HOME/.orbit/bin"
go build -o "$HOME/.orbit/bin/orbit" .
export PATH="$HOME/.orbit/bin:$PATH"
orbit init
```

Add the PATH setting to your shell startup file to make it persistent. By default, `orbit init` creates `~/.orbit/orbit.yaml` without overwriting an existing configuration.

Edit that file to declare your first dependency:

```yaml
version: "1.0"
global_cache: "~/.orbit/cache"

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
orbit sync
orbit link
orbit check
```

This example links the asset to `~/.claude/skills/skill-creator` and `~/.codex/skills/skill-creator`. If a real file, directory, or symlinked parent directory occupies a destination, resolve the conflict manually first.

## Commands

| Command | Purpose | Common options |
| --- | --- | --- |
| `orbit init` | Generate configuration without overwriting an existing file | `--path <dir>` |
| `orbit sync` | Update the cache from upstream | `--label <label>`, `--jobs <n>` |
| `orbit link` | Create or remove matching links according to the `link` switch | `--label <label>`, `--force` |
| `orbit mirror` | Aggregate, commit, and push a personal mirror | `--label <label>`, `--message <text>` |
| `orbit check` | Check the cache and enabled links | `--fix` with explicit Axon inputs |
| `orbit clean` | Preview and confirm deletion of selected managed caches | `--label <label>`, `--yes` |

Labels are case-sensitive. Repeated `--label` flags use **OR** matching; no matches is an error. Sync runs at most 4 concurrent Git operations by default. Each Git operation has a two-minute timeout, adjustable with `--timeout`.

```sh
orbit sync --label skills --label workflows --jobs 4
```

## Directories and configuration

| Default path | Purpose |
| --- | --- |
| `~/.orbit/bin/orbit` | Binary built by the installation steps above |
| `~/.orbit/orbit.yaml` | Global configuration |
| `~/.orbit/cache` | Git cache shared by source and version |
| `~/.orbit/repo` | Local repository created on the first mirror operation |

Configuration lookup order: `--config <file>` → nearest `orbit.yaml` in the project or its parent directories → global configuration. Relative paths resolve from the configuration file's directory. Paths starting with `~/` are supported.

Declare tool root directories once in the global `link_targets`, grouped by category. Each dependency uses a `link` switch. The first segment of `mirror_dir` selects the category; the remaining path is appended to each tool root.

After changing an existing dependency's `link` to `false`, run `orbit link` again to remove symlinks at the current destinations that still point to that dependency's cached subdirectory, including dangling links. Cache contents, real files, directories, and links pointing elsewhere are preserved. Cleanup works even when the cache is missing and respects `--label`. If you also change the source, version, `subdir`, `mirror_dir`, or global targets, old links that no longer match the current configuration must be handled manually.

| Dependency field | Default or behavior when omitted |
| --- | --- |
| `name`, `source`, `version` | Required; names must be unique |
| `subdir` | Defaults to `.`, using the whole repository |
| `link` | Defaults to `false`; `orbit link` removes matching old links; sync and mirror remain available |
| `mirror_dir` | If omitted, sync only; required when linking is enabled |

## Mirror to a personal repository

Add `orbit_repo` to the same configuration. Replace `YOUR_ACCOUNT` with your account, create the remote repository, and ensure your Git credentials work:

```yaml
orbit_repo:
  remote: "git@github.com:YOUR_ACCOUNT/orbit-repo.git"
  branch: "main"
  local_path: "~/.orbit/repo"
```

You can omit `local_path`; it defaults to `~/.orbit/repo`. After syncing, run:

```sh
orbit mirror
```

- Selected caches must be clean and synced. If any preflight check fails, no partial snapshot is published.
- Nested `.git` entries are excluded. Internal relative symlinks are preserved; absolute or escaping links are rejected. An `.orbitignore` beside the configuration uses Gitignore rules. `.env` is not implicitly excluded.
- `.orbit-manifest.yaml` records sources and resolved commits. A label-filtered mirror preserves unselected assets. A full mirror removes only stale assets previously managed by Orbit; the first takeover preserves unmanaged content such as a README.
- Unchanged content produces no empty commit. A failed push keeps the local commit for the next attempt. Git attributes or filters that transform asset bytes cause an error and rollback.

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

Add `--repo "$HOME/.orbit/repo"` to either command to compare the mirror's file inventory and report unconfigured assets. This checks neither file bytes nor versions and does not guess unknown assets' sources.

## Limitations and behavior

| Scenario | Behavior |
| --- | --- |
| Local cache edits | Sync uses fetch, reset --hard, and clean -ffdx to discard changes, including ignored files; edits through symlinks also affect the shared cache |
| Version selection | Branches track upstream; a full SHA pins a commit; ambiguous branch/tag names or changed cached tags are rejected |
| Link conflicts | `link --force` replaces incorrect symlinks only; real files, directories, and symlinked parents require manual handling |
| Diverged mirror history | Normal push by default, without automatic merge/rebase; `mirror --force` requires confirmation, or explicit `--force --yes` in non-interactive use |
| Non-interactive cleanup | `clean --yes` confirms deletion of selected managed caches; `--yes` alone does not authorize a force push |
| Git extensions | Git LFS and submodules are not supported yet |
| Platform verification | Runtime verified on macOS arm64; cross-compilation passed for macOS amd64, Linux amd64, and Windows amd64; native Linux / Windows behavior still needs verification |

Commands exit with `0` on success and a nonzero status on validation differences or operational failure. See `orbit --help` and `orbit <command> --help` for all options.
