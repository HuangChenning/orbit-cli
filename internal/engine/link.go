package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/HuangChenning/orbit-cli/internal/config"
)

func (e *Engine) Link(ctx context.Context, deps []config.Dependency, force bool) error {
	if len(deps) == 0 {
		return fmt.Errorf("no dependencies configured")
	}
	var errs []error
	for _, d := range deps {
		if !d.Link {
			continue
		}
		l, err := e.lock(d, false)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		source, _, err := e.ready(ctx, d)
		if err == nil {
			for _, target := range e.Config.Targets(d) {
				if err = makeLink(source, target, force); err != nil {
					errs = append(errs, err)
				}
			}
		} else {
			errs = append(errs, err)
		}
		l.Unlock()
	}
	return errors.Join(errs...)
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
		if filepath.Clean(old) == filepath.Clean(source) {
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
		if runtime.GOOS == "windows" {
			return fmt.Errorf("symlink failed; enable Windows Developer Mode or run as administrator: %w", err)
		}
		return err
	}
	if info != nil && runtime.GOOS == "windows" {
		if err = os.Remove(target); err != nil {
			return err
		}
	}
	return os.Rename(tmp, target)
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
