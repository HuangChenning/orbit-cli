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

func TestDuplicateYAMLKeysAreRejected(t *testing.T) {
	for _, input := range []string{
		"version: '1.0'\nglobal_cache: cache\nglobal_cache: other\ndependencies: []\n",
		"version: '1.0'\nglobal_cache: cache\ndependencies: [{name: fixture, source: upstream, source: other, version: main}]\n",
	} {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("duplicate keys could silently change managed storage: %s", input)
		}
	}
}

func TestOverlappingManagedPathsAreRejected(t *testing.T) {
	for _, input := range []string{
		"version: '1.0'\nglobal_cache: storage\norbit_repo: {remote: synthetic, branch: main, local_path: storage/repo}\ndependencies: []\n",
		"version: '1.0'\nglobal_cache: cache\nlink_targets: {skills: [cache/skills]}\ndependencies: [{name: fixture, source: upstream, version: main, mirror_dir: skills, link: true}]\n",
		"version: '1.0'\nglobal_cache: cache\ndependencies: [{name: first, source: upstream, version: main, mirror_dir: skills}, {name: second, source: upstream, version: main, mirror_dir: skills/nested}]\n",
	} {
		c, err := Parse([]byte(input))
		if err != nil {
			t.Fatal(err)
		}
		c.Path = filepath.Join(t.TempDir(), "orbit.yaml")
		if err = c.Resolve(); err == nil || !strings.Contains(err.Error(), "overlap") {
			t.Fatalf("overlapping paths must not share ownership: %v", err)
		}
	}
}

func TestDependencyRefCompatibilityAndStrictControls(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
		valid        bool
	}{
		{"canonical", "ref: refs/heads/main", true},
		{"legacy", "version: refs/heads/main", true},
		{"disabled", "ref: main, vendor: false", true},
		{"enabled", "ref: main, vendor: true", true},
		{"both", "ref: main, version: main", false},
		{"merged ref and legacy", "<<: {ref: main}, version: main", false},
		{"merged null vendor", "<<: {vendor: null}, ref: main", false},
		{"empty ref with legacy", "ref: '', version: main", false},
		{"empty legacy with ref", "ref: main, version: ''", false},
		{"numeric ref", "ref: 123", false},
		{"quoted switch", "ref: main, vendor: 'false'", false},
		{"null switch", "ref: main, vendor: null", false},
		{"unknown field", "ref: main, vendor: true, invented: true", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := Parse([]byte("version: '1.0'\nglobal_cache: cache\ndependencies: [{name: fixture, source: upstream, " + tc.fields + "}]\n"))
			if err == nil {
				c.Path = filepath.Join(t.TempDir(), "orbit.yaml")
				err = c.Resolve()
			}
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, got error %v", tc.valid, err)
			}
		})
	}
}

func TestDefaultConfigIgnoresProjectAndParentCopies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	global := filepath.Join(home, ".orbit", "orbit.yaml")
	if err := os.MkdirAll(filepath.Dir(global), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(global, []byte("global"), 0600); err != nil {
		t.Fatal(err)
	}
	project := filepath.Join(t.TempDir(), "orbit.yaml")
	if err := os.WriteFile(project, []byte("project"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Find(filepath.Dir(project), "")
	if err != nil || got != global {
		t.Fatalf("project copy must not shadow global config: %q %v", got, err)
	}
	got, err = Find(filepath.Dir(project), project)
	if err != nil || got != project {
		t.Fatalf("explicit project config ignored: %q %v", got, err)
	}
	if err := os.Remove(global); err != nil {
		t.Fatal(err)
	}
	if _, err = Find(filepath.Dir(project), ""); err == nil {
		t.Fatal("missing global config must not silently adopt a project copy")
	}
}
