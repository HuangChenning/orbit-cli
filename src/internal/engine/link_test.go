package engine

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSymlinkPermissionErrorPreservesCauseAndExplainsWindowsRemedy(t *testing.T) {
	err := symlinkError(os.ErrPermission)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("symlink failure lost the permission error: %v", err)
	}
	if runtime.GOOS == "windows" {
		if !strings.Contains(err.Error(), "Developer Mode") || !strings.Contains(err.Error(), "administrator") {
			t.Fatalf("Windows symlink failure must explain how to obtain permission: %v", err)
		}
	} else if err != os.ErrPermission {
		t.Fatalf("non-Windows symlink failure must preserve the original error: %v", err)
	}
}

func TestNativePathComparisonProtectsForeignSymlinkParents(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(root, "foreign")
	if err := os.Symlink(source, foreign); err != nil {
		t.Fatal(err)
	}
	if sameNativePath(source, foreign) {
		t.Fatal("a foreign symlink must not acquire ownership by resolving to the same asset")
	}
	if !sameNativePath(filepath.Join(source, "missing"), filepath.Join(source, "missing")) {
		t.Fatal("owned dangling destinations must remain removable")
	}
	if sameNativePath(filepath.Join(source, "missing"), filepath.Join(foreign, "missing")) {
		t.Fatal("dangling paths through foreign parents must not acquire ownership")
	}
}
