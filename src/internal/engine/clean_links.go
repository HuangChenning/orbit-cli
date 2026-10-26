package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/HuangChenning/orbit-cli/v2/src/internal/config"
)

// CleanLinks handles foreign links too, but never traverses a target root or
// child symlink. Scan everything before deleting anything.
func (e *Engine) CleanLinks(ctx context.Context, yes bool) error {
	roots := map[string]bool{}
	for _, targets := range e.Config.LinkTargets {
		for _, root := range targets {
			roots[root] = true
		}
	}
	var ordered []string
	for root := range roots {
		ordered = append(ordered, root)
	}
	sort.Strings(ordered)
	links := map[string]string{}
	for _, root := range ordered {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := config.NoSymlinks(root); err != nil {
			return err
		}
		if err := e.safeCleanRoot(root); err != nil {
			return err
		}
		entries, err := os.ReadDir(root)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink == 0 {
				continue
			}
			path := filepath.Join(root, entry.Name())
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if os.IsNotExist(err) || (err == nil && info.IsDir()) {
				links[path] = target
			} else if err != nil {
				return err
			}
		}
	}
	var paths []string
	for path := range links {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if !yes {
		for _, path := range paths {
			fmt.Fprintln(e.Out, "would remove", path, "->", links[path])
		}
		fmt.Fprintf(e.Out, "link clean preview: candidates=%d; use --yes to remove these links\n", len(paths))
		return nil
	}
	for i, path := range paths {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("link clean stopped after removed=%d: %w", i, err)
		}
		if err := config.NoSymlinks(filepath.Dir(path)); err != nil {
			return fmt.Errorf("link clean stopped after removed=%d: %w", i, err)
		}
		target, err := os.Readlink(path)
		if err != nil || target != links[path] {
			return fmt.Errorf("link clean stopped after removed=%d: link changed during cleanup: %s", i, path)
		}
		if err = os.Remove(path); err != nil {
			return fmt.Errorf("link clean stopped after removed=%d: %w", i, err)
		}
		fmt.Fprintln(e.Out, "removed", path, "->", target)
	}
	fmt.Fprintf(e.Out, "link clean summary: removed=%d\n", len(paths))
	return nil
}

func (e *Engine) safeCleanRoot(root string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if root == filepath.Dir(root) || nativeWithin(home, root) {
		return fmt.Errorf("refusing unsafe link cleanup root: %s", root)
	}
	if nativeOverlap(root, e.Config.GlobalCache) || (e.Config.OrbitRepo != nil && nativeOverlap(root, e.Config.OrbitRepo.LocalPath)) {
		return fmt.Errorf("link cleanup root overlaps managed storage: %s", root)
	}
	return nil
}

func nativeOverlap(a, b string) bool { return nativeWithin(a, b) || nativeWithin(b, a) }

func nativeWithin(path, parent string) bool {
	for {
		if sameNativePath(path, parent) {
			return true
		}
		next := filepath.Dir(path)
		if next == path {
			return false
		}
		path = next
	}
}
