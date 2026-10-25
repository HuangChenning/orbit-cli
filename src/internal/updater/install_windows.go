package updater

import (
	"fmt"
	"os"
	"path/filepath"
)

func replaceExecutable(staged, destination string) (string, error) {
	backupFile, err := os.CreateTemp(filepath.Dir(destination), ".orbit-backup-*.exe")
	if err != nil {
		return "", err
	}
	backup := backupFile.Name()
	if err := backupFile.Close(); err != nil {
		os.Remove(backup)
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return "", err
	}
	if err := os.Rename(destination, backup); err != nil {
		return "", err
	}
	if err := os.Rename(staged, destination); err != nil {
		if rollback := os.Rename(backup, destination); rollback != nil {
			return backup, fmt.Errorf("install failed: %v; rollback failed: %v; original binary retained at %s", err, rollback, backup)
		}
		return "", err
	}
	if err := os.Remove(backup); err != nil {
		return backup, nil
	}
	return "", nil
}
