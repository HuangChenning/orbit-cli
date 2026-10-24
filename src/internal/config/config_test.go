package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepositoryDefaultsToOrbitHome(t *testing.T) {
	c, err := Parse([]byte("version: '1.0'\nglobal_cache: ~/.orbit/cache\norbit_repo:\n  remote: https://example.com/repo.git\n  branch: main\ndependencies: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	c.Path = filepath.Join(t.TempDir(), "orbit.yaml")
	if err = c.Resolve(); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if c.OrbitRepo.LocalPath != filepath.Join(home, ".orbit", "repo") {
		t.Fatalf("mirror default %q", c.OrbitRepo.LocalPath)
	}
}

func TestScalarTypesCannotBecomePathsByYAMLCoercion(t *testing.T) {
	for _, input := range []string{
		"version: 1.0\nglobal_cache: cache\ndependencies: []",
		"version: '1.0'\nglobal_cache: true\ndependencies: []",
		"version: '1.0'\nglobal_cache: cache\ndependencies: [{name: true, source: /tmp/a, version: main}]",
		"version: '1.0'\nglobal_cache: cache\nlink_targets: {skills: [123]}\ndependencies: []",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("accepted incorrect scalar type: %s", input)
		}
	}
}

func TestAncestorSymlinksCannotBeManagedStorage(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "cache")); err != nil {
		t.Fatal(err)
	}
	c := Config{Version: "1.0", GlobalCache: "cache", Path: filepath.Join(dir, "orbit.yaml")}
	if err := c.Resolve(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("must protect outside directory: %v", err)
	}
}
