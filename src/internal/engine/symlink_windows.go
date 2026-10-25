package engine

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// The mirrored relative target may not have been copied yet. Use the source's
// type rather than os.Symlink's destination-side probe to set the directory flag.
func mirrorSymlink(target, path string, directory bool) error {
	if !directory {
		return os.Symlink(target, path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(absolute, `\\?\`) {
		if strings.HasPrefix(absolute, `\\`) {
			absolute = `\\?\UNC\` + absolute[2:]
		} else {
			absolute = `\\?\` + absolute
		}
	}
	old, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	new, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return err
	}
	const allowUnprivilegedCreate = 0x2
	flags := uint32(allowUnprivilegedCreate | windows.SYMBOLIC_LINK_FLAG_DIRECTORY)
	err = windows.CreateSymbolicLink(new, old, flags)
	if err != nil {
		err = windows.CreateSymbolicLink(new, old, flags&^allowUnprivilegedCreate)
	}
	if err != nil {
		return &os.LinkError{Op: "symlink", Old: target, New: path, Err: err}
	}
	return nil
}
