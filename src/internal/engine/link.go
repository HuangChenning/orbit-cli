package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
)

func (e *Engine) Link(ctx context.Context, deps []config.Dependency, force bool) error {
	if len(deps) == 0 {
		return fmt.Errorf("no dependencies configured")
	}
	var errs []error
	total := map[string]int{}
	defer func() { printLinkCounts(e.Out, "link summary", total) }()
	if e.Config.OrbitRepo != nil {
		l, err := acquire(e.Config.OrbitRepo.LocalPath + ".orbit.lock")
		if err != nil {
			total["failed"]++
			return err
		}
		defer l.Unlock()
	}
	for _, d := range deps {
		counts := map[string]int{}
		record := func(action string) { counts[action]++; total[action]++ }
		if !d.Link {
			sources := []string{filepath.Join(e.cache(d), filepath.FromSlash(d.Subdir))}
			if e.Config.OrbitRepo != nil && d.MirrorDir != "" {
				sources = append(sources, filepath.Join(e.Config.OrbitRepo.LocalPath, filepath.FromSlash(d.MirrorDir)))
			}
			// Resolve the configured destinations even though linking is disabled.
			d.Link = true
			for _, target := range e.Config.Targets(d) {
				removed, failed := false, false
				for _, source := range sources {
					didRemove, err := removeMatchingLink(source, target)
					if err != nil {
						errs = append(errs, err)
						record("failed")
						failed = true
						break
					}
					if didRemove {
						removed = true
						break
					}
				}
				if !failed {
					if removed {
						record("removed")
					} else if _, err := os.Lstat(target); os.IsNotExist(err) {
						record("absent")
					} else if err != nil {
						errs = append(errs, err)
						record("failed")
					} else {
						record("preserved")
					}
				}
			}
			printLinkCounts(e.Out, "link "+d.Name+" (disabled)", counts)
			continue
		}
		source, _, err := e.localAsset(d)
		if err == nil {
			for _, target := range e.Config.Targets(d) {
				replace := force
				action := "created"
				if old, readErr := os.Readlink(target); readErr == nil {
					if !filepath.IsAbs(old) {
						old = filepath.Join(filepath.Dir(target), old)
					}
					action = "replaced"
					if sameNativePath(old, source) {
						action = "unchanged"
					}
					replace = replace || sameNativePath(old, filepath.Join(e.cache(d), filepath.FromSlash(d.Subdir)))
				}
				if err = makeLink(source, target, replace); err != nil {
					errs = append(errs, err)
					record("failed")
				} else {
					record(action)
				}
			}
		} else {
			errs = append(errs, err)
			record("failed")
		}
		printLinkCounts(e.Out, "link "+d.Name, counts)
	}
	return errors.Join(errs...)
}

func printLinkCounts(out io.Writer, label string, counts map[string]int) {
	var fields []string
	for _, action := range []string{"created", "replaced", "unchanged", "removed", "preserved", "absent", "failed"} {
		if counts[action] > 0 {
			fields = append(fields, fmt.Sprintf("%s=%d", action, counts[action]))
		}
	}
	if len(fields) == 0 {
		fields = append(fields, "no configured targets")
	}
	fmt.Fprintf(out, "%s: %s\n", label, strings.Join(fields, ", "))
}

// sameNativePath recognizes native aliases without following symlink parents.
// Missing suffixes are compared beneath the nearest existing directory so cleanup
// still recognizes an owned dangling link after its cache or repo was removed.
func sameNativePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	if config.NoSymlinks(a) != nil || config.NoSymlinks(b) != nil {
		return false
	}
	ai, ae := os.Stat(a)
	bi, be := os.Stat(b)
	if ae == nil && be == nil {
		return os.SameFile(ai, bi)
	}
	if !os.IsNotExist(ae) || !os.IsNotExist(be) {
		return false
	}
	an, bn := filepath.Base(a), filepath.Base(b)
	if runtime.GOOS == "windows" {
		an, bn = strings.ToLower(an), strings.ToLower(bn)
	}
	if an != bn || filepath.Dir(a) == a || filepath.Dir(b) == b {
		return false
	}
	return sameNativePath(filepath.Dir(a), filepath.Dir(b))
}

func removeMatchingLink(source, target string) (bool, error) {
	if err := config.NoSymlinks(filepath.Dir(target)); err != nil {
		return false, err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return false, nil
	}
	old, err := os.Readlink(target)
	if err != nil {
		return false, err
	}
	if !filepath.IsAbs(old) {
		old = filepath.Join(filepath.Dir(target), old)
	}
	// Native aliases and missing suffixes must also match for dangling links.
	if !sameNativePath(old, source) {
		return false, nil
	}
	if err := os.Remove(target); err != nil {
		return false, err
	}
	return true, nil
}

func makeLink(source, target string, force bool) error {
	if err := config.NoSymlinks(filepath.Dir(target)); err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("real file or directory at %s; move it manually before linking", target)
		}
		old, err := os.Readlink(target)
		if err != nil {
			return err
		}
		if !filepath.IsAbs(old) {
			old = filepath.Join(filepath.Dir(target), old)
		}
		if sameNativePath(old, source) {
			return nil
		}
		if !force {
			return fmt.Errorf("wrong link at %s; use --force to replace the symlink", target)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	// Build the replacement before removing an existing link.
	f, err := os.CreateTemp(filepath.Dir(target), ".orbit-link-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	f.Close()
	os.Remove(tmp)
	defer os.Remove(tmp)
	if err = os.Symlink(source, tmp); err != nil {
		return symlinkError(err)
	}
	if info != nil && runtime.GOOS == "windows" {
		if err = os.Remove(target); err != nil {
			return err
		}
	}
	return os.Rename(tmp, target)
}

func symlinkError(err error) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("symlink failed; enable Windows Developer Mode or run as administrator: %w", err)
	}
	return err
}

func (e *Engine) Clean(ctx context.Context, deps []config.Dependency) error {
	var errs []error
	for _, d := range unique(deps) {
		l, err := e.lock(d, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		_, err = e.readState(d)
		if err == nil {
			err = e.verifyRepository(ctx, d)
		}
		if err == nil {
			err = os.RemoveAll(e.cache(d))
		}
		if err == nil {
			err = os.Remove(e.statePath(d))
		}
		l.Unlock()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
