# Changelog

## v1.0.0 — Unreleased

- Download and assemble Git-backed skills, workflows, scripts and commands with `orbit vendor sync`.
- Link local assets to developer tools, including cleanup when a dependency's `link` switch is disabled.
- Commit and push managed local assets with `orbit sync`, independently of upstream availability or caches.
- Filter by labels, check configuration coverage, repair known omissions and safely clean owned caches.
- Protect real files, unmanaged changes, manifest ownership and publication bytes; isolate validation paths from dependency names.
- Add native macOS Intel/Apple Silicon, Linux amd64 and Windows amd64 acceptance checks and unsigned candidate packages with SHA256 checksums.
- Check the latest stable GitHub Release with `orbit update check`; install a verified native package with `orbit update`, preserving the current program on download or integrity failure.
- Identify builds through `orbit --version`. Source builds use `dev`; release candidates include version, commit and build metadata.

### Compatibility

This is the first stable release candidate; no stable GitHub Release has been published yet. Source builds target `./src` and require a maintained Go series (currently Go 1.26 or 1.27). Candidate packages built with Go 1.27 require macOS 13 or newer. See the READMEs for the actual native validation images and unsigned installation notes.
