package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPrintSyncResultKeepsCommitSubjectOnOneLine(t *testing.T) {
	var out bytes.Buffer
	printSyncResult(&out, syncResult{
		Name:    "fixture",
		Commit:  "1234567890abcdef",
		Subject: "safe\r\x1b[31m forged",
	})
	const want = "✓  [fixture] added 1234567 safe  [31m forged\n"
	if out.String() != want {
		t.Fatalf("sync output:\nwant %q\ngot  %q", want, out.String())
	}
}

func TestRollbackRestoresOldSnapshotOverPartialCopy(t *testing.T) {
	root := t.TempDir()
	backup := t.TempDir()
	if err := os.MkdirAll(filepath.Join(backup, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "skills", "old"), []byte("previous snapshot"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "skills", "partial"), []byte("incomplete copy"), 0600); err != nil {
		t.Fatal(err)
	}
	// A failed cross-filesystem copy may have a nonempty destination even if its
	// caller never recorded the destination as successfully installed.
	rollbackSnapshot(root, backup, nil, []string{"skills"})
	data, err := os.ReadFile(filepath.Join(root, "skills", "old"))
	if err != nil || string(data) != "previous snapshot" {
		t.Fatalf("lost old snapshot: %q, %v", data, err)
	}
	if _, err = os.Stat(filepath.Join(root, "skills", "partial")); !os.IsNotExist(err) {
		t.Fatalf("partial copy retained: %v", err)
	}
}
