# Changelog

## v2.1.1 — 2026-10-29

- Let Git clean up cache locks on Unix when an operation times out or is canceled, then forcibly terminate any remaining child processes. Interrupted shallow fetches can be retried without leaving `shallow.lock` behind during graceful termination.
- Preserve timeout and cancellation causes in Git errors. Existing locks with unknown ownership are retained and reported with safe recovery instructions rather than deleted automatically.

## v2.1.0 — 2026-10-26

- Restore assets from the personal repository with `orbit pull`, without upstream dependencies or caches, and register them in the local YAML. Validate incoming assets and configuration before installation; update existing repositories only by fast-forward and preserve dirty worktrees and unpublished commits.
- Preserve local dependency switches, labels, tool targets and unrelated declarations. Conflicting asset identities stop with both values shown; `pull --update-config` explicitly accepts the remote identity after review. Configuration writes are atomic and detect concurrent edits.
- Preview directory and broken symlinks directly under `link_targets` with `orbit link clean`; `--yes` removes those links, including Axon/foreign links, while preserving assets, real directories, file links and nested links.
- Document Ubuntu Homebrew installation, trust-store permissions, and the complete new-device pull/link workflow in both READMEs.

## v2.0.0 — 2026-10-25

- Add read-only `check --duplicates` for repeated source selections, nested source directories, skill names and identical `SKILL.md` bytes, honoring `.orbitignore` and reporting incomplete local scans.
- Default asset commands to `~/.orbit/orbit.yaml`; select project configurations explicitly with `--config`. **Breaking change:** project and parent-directory `orbit.yaml` files are no longer discovered automatically. Existing project workflows must pass `--config ./orbit.yaml`; the YAML schema remains `version: "1.0"`.
- Show configuration paths, per-asset link actions and totals, plus sync validation, commit/no-change and push progress.
- Move the Go module and internal imports to `github.com/HuangChenning/orbit-cli/v2` for the v2 major release; update release metadata injection accordingly.

## v1.1.0 — 2026-10-25

- Use dependency `ref` for Git branches, tags and commits; accept legacy dependency `version` while rejecting configurations containing both keys. The schema version remains `"1.0"`, and existing caches/manifests remain compatible.
- Add `vendor: false` to skip upstream downloads and assembly while preserving local assets, manifest records, linking and publication. The default is `true`.
- Select assets with `--name`, or optional groups with `--label`; both accept comma-separated lists and repeated flags, with whitespace trimming, deduplication and OR matching. Reject mixed selectors, unknown names, empty items and Chinese commas.
- Generate and repair canonical dependency refs, preserve disabled switches, and place optional labels after vendor/link. Expand English and Chinese configuration references and examples.

## v1.0.0 — 2026-10-25

- Download and assemble Git-backed skills, workflows, scripts and commands with `orbit vendor sync`.
- Link local assets to developer tools, including cleanup when a dependency's `link` switch is disabled.
- Commit and push managed local assets with `orbit sync`, independently of upstream availability or caches.
- Filter by labels, check configuration coverage, repair known omissions and safely clean owned caches.
- Protect real files, unmanaged changes, manifest ownership and publication bytes; isolate validation paths from dependency names.
- Add native macOS Intel/Apple Silicon, Linux amd64 and Windows amd64 acceptance checks and unsigned candidate packages with SHA256 checksums.
- Check the latest stable GitHub Release with `orbit update check`; install a verified native package with `orbit update`, preserving the current program on download or integrity failure.
- Identify builds through `orbit --version`. Source builds use `dev`; release candidates include version, commit and build metadata.

### Compatibility

This is the first stable release. Source builds target `./src` and require a maintained Go series (currently Go 1.26 or 1.27). Release packages built with Go 1.27 require macOS 13 or newer. See the READMEs for the actual native validation images and unsigned installation notes.
