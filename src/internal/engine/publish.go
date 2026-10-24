package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
	"gopkg.in/yaml.v3"
)

func (e *Engine) localAsset(d config.Dependency) (string, Entry, error) {
	r := e.Config.OrbitRepo
	if r == nil || d.MirrorDir == "" {
		return "", Entry{}, fmt.Errorf("%s requires orbit_repo and mirror_dir; run orbit vendor sync", d.Name)
	}
	m, err := readManifest(filepath.Join(r.LocalPath, ".orbit-manifest.yaml"))
	if err != nil {
		return "", Entry{}, err
	}
	for _, entry := range m.Dependencies {
		if entry.Name != d.Name {
			continue
		}
		if entry.Source != d.Source || entry.Version != d.Version || entry.Subdir != d.Subdir || entry.MirrorDir != d.MirrorDir || entry.Commit == "" {
			return "", Entry{}, fmt.Errorf("%s local asset identity changed; run orbit vendor sync", d.Name)
		}
		path := filepath.Join(r.LocalPath, filepath.FromSlash(d.MirrorDir))
		if err := config.NoSymlinks(path); err != nil {
			return "", Entry{}, err
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			return "", Entry{}, fmt.Errorf("%s local asset missing; run orbit vendor sync", d.Name)
		}
		return path, entry, nil
	}
	return "", Entry{}, fmt.Errorf("%s has not been assembled; run orbit vendor sync", d.Name)
}

// Pending changes are allowed only within previously managed asset roots.
// Never consume an unrelated staged change or overwrite an unmanaged file.
func (e *Engine) checkManagedChanges(ctx context.Context) error {
	p := e.Config.OrbitRepo.LocalPath
	staged, err := e.git(ctx, p, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if staged != "" {
		return fmt.Errorf("repository has staged changes; commit or unstage them before retrying")
	}
	m, err := readManifest(filepath.Join(p, ".orbit-manifest.yaml"))
	if err != nil {
		return err
	}
	// Include roots removed by a full vendor refresh but still tracked in HEAD.
	if data, err := e.git(ctx, p, "show", "HEAD:.orbit-manifest.yaml"); err == nil {
		old, err := parseManifest([]byte(data))
		if err != nil {
			return err
		}
		m.Dependencies = append(m.Dependencies, old.Dependencies...)
	}
	for _, args := range [][]string{
		{"diff", "--name-only", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
		{"ls-files", "--others", "--ignored", "--exclude-standard", "-z"},
	} {
		out, err := e.git(ctx, p, args...)
		if err != nil {
			return err
		}
		for _, path := range strings.Split(out, "\x00") {
			if path == "" || path == ".orbit-manifest.yaml" {
				continue
			}
			// Finder creates this untracked root file during normal browsing.
			// Preserve it without staging it; tracked changes still need review.
			if args[0] == "ls-files" && path == ".DS_Store" {
				continue
			}
			managed := false
			for _, d := range m.Dependencies {
				if path == d.MirrorDir || strings.HasPrefix(path, d.MirrorDir+"/") {
					managed = true
					break
				}
			}
			if !managed {
				return fmt.Errorf("unmanaged working tree change at %s; preserve/commit it before retrying", path)
			}
		}
	}
	return nil
}

func (e *Engine) Publish(ctx context.Context, deps []config.Dependency, full, force bool) error {
	r := e.Config.OrbitRepo
	if r == nil {
		return fmt.Errorf("orbit_repo is required for sync")
	}
	if _, err := os.Stat(r.LocalPath); err != nil {
		return fmt.Errorf("local repository missing; run orbit vendor sync: %w", err)
	}
	l, err := acquire(r.LocalPath + ".orbit.lock")
	if err != nil {
		return err
	}
	defer l.Unlock()
	if err = e.prepareRepository(ctx); err != nil {
		return err
	}
	if err = e.updateRepository(ctx, force); err != nil {
		return err
	}
	local, err := readManifest(filepath.Join(r.LocalPath, ".orbit-manifest.yaml"))
	if err != nil {
		return err
	}
	previous := Manifest{Version: "1.0"}
	if data, readErr := e.git(ctx, r.LocalPath, "show", "HEAD:.orbit-manifest.yaml"); readErr == nil {
		previous, err = parseManifest([]byte(data))
		if err != nil {
			return err
		}
	}
	selected := map[string]bool{}
	var entries []Entry
	paths := map[string]bool{}
	validation, err := os.MkdirTemp("", "orbit-publish-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(validation)
	for _, d := range deps {
		if d.MirrorDir == "" {
			continue
		}
		source, entry, err := e.localAsset(d)
		if err != nil {
			return err
		}
		// Validate local symlinks and file types before touching the index.
		if err = copyAsset(source, filepath.Join(validation, d.Name), d.MirrorDir, nil); err != nil {
			return err
		}
		entries = append(entries, entry)
		selected[d.Name] = true
		paths[d.MirrorDir] = true
	}
	if len(entries) == 0 {
		return fmt.Errorf("no selected local assets; run orbit vendor sync")
	}
	result := local
	if full {
		for _, d := range local.Dependencies {
			if !selected[d.Name] {
				return fmt.Errorf("local inventory no longer matches configuration; run orbit vendor sync")
			}
		}
	}
	if !full {
		result = Manifest{Version: "1.0"}
		for _, d := range previous.Dependencies {
			if !selected[d.Name] {
				result.Dependencies = append(result.Dependencies, d)
			}
		}
		result.Dependencies = append(result.Dependencies, entries...)
	}
	for _, d := range previous.Dependencies {
		if full || selected[d.Name] {
			paths[d.MirrorDir] = true
		}
	}
	sort.Slice(result.Dependencies, func(i, j int) bool { return result.Dependencies[i].Name < result.Dependencies[j].Name })
	manifest, err := yaml.Marshal(result)
	if err != nil {
		return err
	}
	if _, err = parseManifest(manifest); err != nil {
		return fmt.Errorf("selected manifest conflicts with previously published assets; use a full sync after reviewing the inventory: %w", err)
	}
	manifestFile := filepath.Join(validation, "manifest.yaml")
	if err = os.WriteFile(manifestFile, manifest, 0600); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			if _, err := e.git(context.Background(), r.LocalPath, "reset", "--mixed", "HEAD"); err != nil {
				e.git(context.Background(), r.LocalPath, "read-tree", "--empty")
			}
		}
	}()
	for path := range paths {
		if err = config.NoSymlinks(filepath.Join(r.LocalPath, filepath.FromSlash(path))); err != nil {
			return err
		}
		tracked, err := e.git(ctx, r.LocalPath, "ls-files", "--", path)
		if err != nil {
			return err
		}
		if _, err := os.Lstat(filepath.Join(r.LocalPath, path)); os.IsNotExist(err) && tracked == "" {
			continue
		}
		if _, err = e.git(ctx, r.LocalPath, "add", "-f", "-A", "--", path); err != nil {
			return err
		}
	}
	// Stage only the selected manifest entries without changing the working copy.
	object, err := e.git(ctx, r.LocalPath, "hash-object", "-w", "--", manifestFile)
	if err != nil {
		return err
	}
	if _, err = e.git(ctx, r.LocalPath, "update-index", "--add", "--cacheinfo", "100644,"+object+",.orbit-manifest.yaml"); err != nil {
		return err
	}
	if err = e.verifySnapshotBytes(ctx, r.LocalPath, entries); err != nil {
		return err
	}
	diff, err := e.git(ctx, r.LocalPath, "diff", "--cached", "--name-only")
	if err != nil {
		return err
	}
	if diff != "" {
		message := e.Message
		if message == "" {
			message = "Update Orbit asset snapshot"
		}
		if _, err = e.git(ctx, r.LocalPath, "-c", "core.hooksPath="+os.DevNull, "commit", "-m", message); err != nil {
			return err
		}
	}
	committed = true
	args := []string{"push"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "origin", "HEAD:refs/heads/"+r.Branch)
	if _, err = e.git(ctx, r.LocalPath, args...); err != nil {
		return fmt.Errorf("sync push failed; check remote credentials/SSH keys; local commit retained for retry: %w", err)
	}
	fmt.Fprintln(e.Out, "published", len(entries), "dependencies")
	return nil
}
