package engine

import (
	"errors"
	"os"
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
