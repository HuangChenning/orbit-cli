package engine

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
	"github.com/gofrs/flock"
	ignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

type Entry struct {
	Name      string `yaml:"name"`
	Source    string `yaml:"source"`
	Version   string `yaml:"version"`
	Commit    string `yaml:"commit"`
	Subdir    string `yaml:"subdir"`
	MirrorDir string `yaml:"mirror_dir"`
}
type Manifest struct {
	Version      string  `yaml:"version"`
	Dependencies []Entry `yaml:"dependencies"`
}

func readManifest(path string) (Manifest, error) {
	m := Manifest{Version: "1.0"}
	if err := config.NoSymlinks(path); err != nil {
		return m, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	return parseManifest(b)
}

func parseManifest(b []byte) (Manifest, error) {
	m := Manifest{Version: "1.0"}
	if err := yaml.Unmarshal(b, &m); err != nil {
		return m, err
	}
	if m.Version != "1.0" {
		return m, fmt.Errorf("unknown manifest version")
	}
	var dirs []string
	names := map[string]bool{}
	for _, d := range m.Dependencies {
		p, err := config.Relative(d.MirrorDir, false)
		if err != nil || p != d.MirrorDir {
			return m, fmt.Errorf("unsafe manifest mirror_dir %q", d.MirrorDir)
		}
		for _, old := range dirs {
			if config.Overlap(filepath.FromSlash(old), filepath.FromSlash(p)) {
				return m, fmt.Errorf("manifest paths overlap")
			}
		}
		dirs = append(dirs, p)
		if names[d.Name] {
			return m, fmt.Errorf("duplicate manifest name")
		}
		names[d.Name] = true
	}
	return m, nil
}

func copyAsset(source, dest, mirrorDir string, matcher *ignore.GitIgnore) error {
	return filepath.WalkDir(source, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		out := filepath.Join(dest, rel)
		if rel != "." && matcher != nil {
			match := filepath.ToSlash(filepath.Join(mirrorDir, rel))
			if d.IsDir() {
				match += "/"
			}
			if matcher.MatchesPath(match) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(out, info.Mode().Perm()|0700)
		}
		if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(target) {
				return fmt.Errorf("absolute symlink cannot be mirrored: %s", path)
			}
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(source, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return fmt.Errorf("symlink escapes selected asset: %s", path)
			}
			return os.Symlink(target, out)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported file type: %s", path)
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, err = io.Copy(f, in)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		return closeErr
	})
}

func (e *Engine) Assemble(ctx context.Context, deps []config.Dependency, full bool) (resultErr error) {
	var selectedDeps []config.Dependency
	for _, d := range deps {
		if d.MirrorDir != "" {
			selectedDeps = append(selectedDeps, d)
		}
	}
	deps = selectedDeps
	if len(deps) == 0 {
		return fmt.Errorf("no selected dependencies have mirror_dir")
	}
	r := e.Config.OrbitRepo
	if r == nil {
		return fmt.Errorf("orbit_repo is required for vendor sync")
	}
	// Hold all cache locks through snapshot construction; sync/clean cannot race the copy.
	var locks []*flock.Flock
	defer func() {
		for _, l := range locks {
			l.Unlock()
		}
	}()
	for _, d := range unique(deps) {
		l, err := e.lock(d, false)
		if err != nil {
			return err
		}
		locks = append(locks, l)
	}
	stage, err := os.MkdirTemp("", "orbit-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var matcher *ignore.GitIgnore
	ignorePath := filepath.Join(filepath.Dir(e.Config.Path), ".orbitignore")
	if b, readErr := os.ReadFile(ignorePath); readErr == nil {
		matcher = ignore.CompileIgnoreLines(strings.Split(string(b), "\n")...)
	} else if !os.IsNotExist(readErr) {
		return readErr
	}
	var entries []Entry
	for _, d := range deps {
		if d.MirrorDir == "" {
			return fmt.Errorf("%s requires mirror_dir", d.Name)
		}
		source, s, err := e.ready(ctx, d)
		if err != nil {
			return err
		}
		if err = copyAsset(source, filepath.Join(stage, filepath.FromSlash(d.MirrorDir)), d.MirrorDir, matcher); err != nil {
			return err
		}
		entries = append(entries, Entry{d.Name, d.Source, d.Version, s.Commit, d.Subdir, d.MirrorDir})
	}
	l, err := acquire(r.LocalPath + ".orbit.lock")
	if err != nil {
		return err
	}
	defer l.Unlock()
	if err = e.prepareRepository(ctx); err != nil {
		return err
	}
	p := r.LocalPath
	old, err := readManifest(filepath.Join(p, ".orbit-manifest.yaml"))
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, d := range deps {
		selected[d.Name] = true
	}
	var result []Entry
	remove := map[string]bool{}
	owned := map[string]bool{}
	for _, d := range old.Dependencies {
		owned[d.MirrorDir] = true
		if full || selected[d.Name] {
			remove[d.MirrorDir] = true
		} else {
			result = append(result, d)
		}
	}
	for _, d := range entries {
		for _, preserved := range result {
			if config.Overlap(filepath.FromSlash(d.MirrorDir), filepath.FromSlash(preserved.MirrorDir)) {
				return fmt.Errorf("selected mirror path overlaps unselected asset %s", preserved.Name)
			}
		}
		target := filepath.Join(p, filepath.FromSlash(d.MirrorDir))
		if err = config.NoSymlinks(target); err != nil {
			return err
		}
		for _, previous := range old.Dependencies {
			if previous.MirrorDir != d.MirrorDir && config.Overlap(filepath.FromSlash(previous.MirrorDir), filepath.FromSlash(d.MirrorDir)) {
				return fmt.Errorf("mirror path changed across overlapping managed roots; choose a distinct path")
			}
		}
		if _, err = os.Lstat(target); err == nil && !owned[d.MirrorDir] {
			return fmt.Errorf("refusing to overwrite unowned mirror directory %s", target)
		} else if err != nil && !os.IsNotExist(err) {
			return err
		}
		remove[d.MirrorDir] = true
		result = append(result, d)
	}
	for dir := range remove {
		if err = config.NoSymlinks(filepath.Join(p, filepath.FromSlash(dir))); err != nil {
			return err
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	manifest, err := yaml.Marshal(Manifest{"1.0", result})
	if err != nil {
		return err
	}
	// Copy and validation have succeeded. Move old managed roots aside for rollback.
	backup, err := os.MkdirTemp(p, ".orbit-rollback-")
	if err != nil {
		return err
	}
	var moved, installed []string
	completed := false
	defer func() {
		if !completed {
			if err := rollbackSnapshot(p, backup, installed, moved); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("rollback failed; backup retained at %s: %w", backup, err))
				return
			}
		}
		if err := os.RemoveAll(backup); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}()
	for dir := range remove {
		target := filepath.Join(p, filepath.FromSlash(dir))
		if _, err = os.Lstat(target); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return err
		}
		saved := filepath.Join(backup, filepath.FromSlash(dir))
		if err = os.MkdirAll(filepath.Dir(saved), 0755); err != nil {
			return err
		}
		if err = os.Rename(target, saved); err != nil {
			return err
		}
		moved = append(moved, dir)
	}
	for _, d := range entries {
		dest := filepath.Join(p, filepath.FromSlash(d.MirrorDir))
		if err = os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			return err
		}
		installed = append(installed, d.MirrorDir)
		if err = os.Rename(filepath.Join(stage, filepath.FromSlash(d.MirrorDir)), dest); err != nil {
			// Temporary directories can be on a different filesystem.
			if err = copyAsset(filepath.Join(stage, filepath.FromSlash(d.MirrorDir)), dest, d.MirrorDir, nil); err != nil {
				return err
			}
		}
	}
	manifestPath := filepath.Join(p, ".orbit-manifest.yaml")
	oldBytes, readErr := os.ReadFile(manifestPath)
	defer func() {
		if !completed {
			if readErr == nil {
				os.WriteFile(manifestPath, oldBytes, 0600)
			} else {
				os.Remove(manifestPath)
			}
		}
	}()
	if err = os.WriteFile(manifestPath, manifest, 0600); err != nil {
		return err
	}
	completed = true
	fmt.Fprintln(e.Out, "assembled", len(entries), "dependencies into", p)
	return nil
}

func rollbackSnapshot(root, backup string, installed, moved []string) error {
	var errs []error
	for _, dir := range installed {
		if err := os.RemoveAll(filepath.Join(root, filepath.FromSlash(dir))); err != nil {
			errs = append(errs, err)
		}
	}
	for _, dir := range moved {
		dest := filepath.Join(root, filepath.FromSlash(dir))
		// Failed fallback copies can create a destination before success is recorded.
		if err := os.RemoveAll(dest); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := os.Rename(filepath.Join(backup, filepath.FromSlash(dir)), dest); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Staging must never silently apply text/filter transformations to source bytes.
// Verify Git blob identities before committing, and roll back on any conversion.
func (e *Engine) verifySnapshotBytes(ctx context.Context, root string, entries []Entry) error {
	index, err := e.git(ctx, root, "ls-files", "--stage", "-z")
	if err != nil {
		return err
	}
	for _, record := range strings.Split(index, "\x00") {
		if record == "" {
			continue
		}
		header, path, ok := strings.Cut(record, "\t")
		if !ok {
			return fmt.Errorf("invalid Git index entry")
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[2] != "0" {
			return fmt.Errorf("unmerged Git index entry")
		}
		selected := false
		for _, entry := range entries {
			if path == entry.MirrorDir || strings.HasPrefix(path, entry.MirrorDir+"/") {
				selected = true
				break
			}
		}
		if !selected {
			continue
		}
		if fields[0] == "160000" {
			return fmt.Errorf("nested Git repository cannot be published: %s", path)
		}
		info, err := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		var h hash.Hash = sha1.New()
		if len(fields[1]) == 64 {
			h = sha256.New()
		}
		fmt.Fprintf(h, "blob %d\x00", info.Size())
		f, err := os.Open(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(h, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if hex.EncodeToString(h.Sum(nil)) != fields[1] {
			return fmt.Errorf("Git attributes/filter would transform asset bytes at %s; remove the transforming attributes before publishing", path)
		}
	}
	return nil
}

func (e *Engine) prepareRepository(ctx context.Context) error {
	r := e.Config.OrbitRepo
	p := r.LocalPath
	if err := config.NoSymlinks(p); err != nil {
		return err
	}
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		if err = os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			return err
		}
		stage, err := os.MkdirTemp(filepath.Dir(p), "."+filepath.Base(p)+".orbit-clone-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		if _, err = e.git(ctx, filepath.Dir(p), "clone", "--no-checkout", "--", r.Remote, stage); err != nil {
			return err
		}
		if _, err = e.git(ctx, stage, "show-ref", "--verify", "refs/remotes/origin/"+r.Branch); err == nil {
			if _, err = e.git(ctx, stage, "checkout", "-B", r.Branch, "refs/remotes/origin/"+r.Branch); err != nil {
				return err
			}
		} else {
			if _, err = e.git(ctx, stage, "symbolic-ref", "HEAD", "refs/heads/"+r.Branch); err != nil {
				return err
			}
		}
		if _, err = os.Lstat(p); err == nil {
			return fmt.Errorf("repository destination appeared during initialization; refusing overwrite")
		} else if !os.IsNotExist(err) {
			return err
		}
		if err = os.Rename(stage, p); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := config.NoSymlinks(filepath.Join(p, ".git")); err != nil {
		return err
	}
	info, err := os.Stat(filepath.Join(p, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("repository must be a Git checkout with a real .git directory")
	}
	remote, err := e.git(ctx, p, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if remote != r.Remote {
		return fmt.Errorf("repository remote mismatch; refusing adoption")
	}
	branch, err := e.git(ctx, p, "symbolic-ref", "--short", "HEAD")
	if err != nil || branch != r.Branch {
		return fmt.Errorf("repository branch mismatch")
	}
	status, err := e.git(ctx, p, "status", "--porcelain", "--untracked-files=all", "--ignored")
	if err != nil {
		return err
	}
	if status != "" {
		return e.checkManagedChanges(ctx)
	}
	return nil
}

func (e *Engine) updateRepository(ctx context.Context, force bool) error {
	r := e.Config.OrbitRepo
	p := r.LocalPath
	remote, err := e.git(ctx, p, "ls-remote", "--heads", "origin", "refs/heads/"+r.Branch)
	if err != nil {
		return err
	}
	if remote == "" {
		return nil
	}
	if _, err = e.git(ctx, p, "fetch", "--no-tags", "origin", "+refs/heads/"+r.Branch+":refs/remotes/origin/"+r.Branch); err != nil {
		return err
	}
	remoteRef := "refs/remotes/origin/" + r.Branch
	if _, err = e.git(ctx, p, "rev-parse", "--verify", "HEAD"); err != nil {
		status, statusErr := e.git(ctx, p, "status", "--porcelain", "--untracked-files=all", "--ignored")
		if statusErr != nil {
			return statusErr
		}
		if status != "" {
			return fmt.Errorf("remote was initialized after local assembly; preserve local assets and reconcile the remote history before sync")
		}
		_, err = e.git(ctx, p, "reset", "--hard", remoteRef)
		return err
	}
	if _, err = e.git(ctx, p, "merge-base", "--is-ancestor", remoteRef, "HEAD"); err == nil {
		return nil
	}
	if _, err = e.git(ctx, p, "merge-base", "--is-ancestor", "HEAD", remoteRef); err == nil {
		_, err = e.git(ctx, p, "merge", "--ff-only", remoteRef)
		return err
	}
	if !force {
		return fmt.Errorf("repository history diverged; refusing automatic merge/rebase; inspect it or explicitly use --force")
	}
	return nil
}
