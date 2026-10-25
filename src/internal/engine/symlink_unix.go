//go:build !windows

package engine

import "os"

func mirrorSymlink(target, path string, directory bool) error {
	return os.Symlink(target, path)
}
