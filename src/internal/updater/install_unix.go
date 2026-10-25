//go:build !windows

package updater

import "os"

func replaceExecutable(staged, destination string) (string, error) {
	return "", os.Rename(staged, destination)
}
