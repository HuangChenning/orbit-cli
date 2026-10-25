package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/HuangChenning/orbit-cli/v2/src/internal/config"
	"gopkg.in/yaml.v3"
)

type CheckOptions struct {
	Baseline, AxonConfig, Vendors, Hub, Repo string
	Fix                                      bool
}
type axonConfig struct {
	RepoPath string                                       `yaml:"repo_path"`
	Targets  []struct{ Source, Destination, Type string } `yaml:"targets"`
}
type vendorsConfig struct {
	Vendors []struct{ Name, Repo, Subdir, Dest, Ref string } `yaml:"vendors"`
}
type hubInfo struct{ Remote, Branch, Commit string }
type baseline struct {
	Dependencies []config.Dependency
	Targets      map[string][]string
	Files        []string
}

func readYAML(path string, out any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(b, out)
}

func (e *Engine) baseline(ctx context.Context, o CheckOptions) (baseline, error) {
	var a axonConfig
	var v vendorsConfig
	var hub hubInfo
	var files []string
	if o.Baseline != "" {
		if err := readYAML(filepath.Join(o.Baseline, "axon.yaml"), &a); err != nil {
			return baseline{}, err
		}
		if err := readYAML(filepath.Join(o.Baseline, "axon.vendors.yaml"), &v); err != nil {
			return baseline{}, err
		}
		b, err := os.ReadFile(filepath.Join(o.Baseline, "hub.json"))
		if err != nil {
			return baseline{}, err
		}
		if err = json.Unmarshal(b, &hub); err != nil {
			return baseline{}, err
		}
		b, err = os.ReadFile(filepath.Join(o.Baseline, "hub-files.txt"))
		if err != nil {
			return baseline{}, err
		}
		files = strings.Split(strings.TrimSpace(string(b)), "\n")
	} else {
		if o.AxonConfig == "" || o.Vendors == "" {
			return baseline{}, fmt.Errorf("coverage checks require both --axon-config and --vendors, or --baseline")
		}
		axon, err := e.Config.Expand(o.AxonConfig)
		if err != nil {
			return baseline{}, err
		}
		vendors, err := e.Config.Expand(o.Vendors)
		if err != nil {
			return baseline{}, err
		}
		if err = readYAML(axon, &a); err != nil {
			return baseline{}, err
		}
		if err = readYAML(vendors, &v); err != nil {
			return baseline{}, err
		}
		hubPath := o.Hub
		if hubPath == "" {
			hubPath = a.RepoPath
		}
		hubPath, err = e.Config.Expand(hubPath)
		if err != nil {
			return baseline{}, err
		}
		hub.Remote, err = e.git(ctx, hubPath, "remote", "get-url", "origin")
		if err != nil {
			return baseline{}, err
		}
		hub.Branch, err = e.git(ctx, hubPath, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return baseline{}, err
		}
		hub.Commit, err = e.git(ctx, hubPath, "rev-parse", "HEAD")
		if err != nil {
			return baseline{}, err
		}
		list, err := e.git(ctx, hubPath, "ls-tree", "-r", "--name-only", "HEAD", "skills", "workflows", "scripts", "commands")
		if err != nil {
			return baseline{}, err
		}
		if list != "" {
			files = strings.Split(list, "\n")
		}
	}
	b := baseline{Targets: map[string][]string{}, Files: files}
	home, _ := os.UserHomeDir()
	for _, t := range a.Targets {
		group, err := config.Relative(t.Source, false)
		if err != nil {
			return b, err
		}
		if strings.Contains(group, "/") || t.Type != "" && t.Type != "directory" {
			return b, fmt.Errorf("Axon targets must be category directories")
		}
		dest := strings.TrimRight(t.Destination, "/")
		if strings.HasPrefix(dest, home+"/") {
			dest = "~" + strings.TrimPrefix(dest, home)
		}
		if !contains(b.Targets[group], dest) {
			b.Targets[group] = append(b.Targets[group], dest)
		}
	}
	add := func(name, source, version, subdir, dest, origin string) error {
		if name == "" || source == "" || version == "" {
			return fmt.Errorf("incomplete Axon dependency %s", name)
		}
		var err error
		dest, err = config.Relative(dest, false)
		if err != nil {
			return err
		}
		if subdir == "" {
			subdir = "."
		}
		subdir, err = config.Relative(subdir, true)
		if err != nil {
			return err
		}
		group, _, _ := strings.Cut(dest, "/")
		vendor := true
		b.Dependencies = append(b.Dependencies, config.Dependency{Name: name, Source: source, Ref: version, Subdir: subdir, MirrorDir: dest, Vendor: &vendor, Link: len(b.Targets[group]) > 0, Labels: []string{group, origin}})
		return nil
	}
	covered := map[string]bool{}
	for _, d := range v.Vendors {
		version := d.Ref
		if version == "" {
			version = "main"
		}
		if err := add(d.Name, d.Repo, version, d.Subdir, d.Dest, "vendor"); err != nil {
			return b, err
		}
		covered[b.Dependencies[len(b.Dependencies)-1].MirrorDir] = true
	}
	roots := map[string]bool{}
	cats := map[string]bool{}
	for _, file := range files {
		p, err := config.Relative(file, false)
		if err != nil {
			return b, err
		}
		parts := strings.Split(p, "/")
		cats[parts[0]] = true
		if parts[0] == "skills" {
			if len(parts) < 3 {
				return b, fmt.Errorf("Hub skill root file needs explicit mapping: %s", p)
			}
			roots[strings.Join(parts[:2], "/")] = true
		}
	}
	var sorted []string
	for root := range roots {
		if !covered[root] {
			sorted = append(sorted, root)
		}
	}
	sort.Strings(sorted)
	for _, dest := range sorted {
		name := "hub-skills-" + strings.ReplaceAll(strings.TrimLeft(strings.TrimPrefix(dest, "skills/"), "."), ".", "-")
		if err := add(name, hub.Remote, "refs/heads/"+hub.Branch, dest, dest, "hub"); err != nil {
			return b, err
		}
	}
	for _, cat := range []string{"workflows", "scripts", "commands"} {
		if cats[cat] {
			if err := add("hub-"+cat, hub.Remote, "refs/heads/"+hub.Branch, cat, cat, "hub"); err != nil {
				return b, err
			}
		}
	}
	return b, nil
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

func reportBase() map[string]any {
	r := map[string]any{"status": "ok", "fixed": false, "backup": nil, "verification": "configuration coverage; repository checks are file inventory only"}
	for _, key := range []string{"missing_dependencies", "missing_links", "duplicate_links", "conflicts", "additional_dependencies", "uncovered_files", "missing_repo_files", "unconfigured_repo_files", "empty_categories"} {
		r[key] = []any{}
	}
	return r
}
func appendReport(r map[string]any, key string, item any) { r[key] = append(r[key].([]any), item) }

func compare(c *config.Config, b baseline, repo string) (map[string]any, error) {
	r := reportBase()
	r["expected_dependencies"] = len(b.Dependencies)
	vendorCount := 0
	actual := map[string]config.Dependency{}
	for _, d := range c.Dependencies {
		actual[d.MirrorDir] = d
	}
	for _, want := range b.Dependencies {
		if contains(want.Labels, "vendor") {
			vendorCount++
		}
		got, ok := actual[want.MirrorDir]
		if !ok {
			appendReport(r, "missing_dependencies", want)
			continue
		}
		values := [][3]string{{"name", got.Name, want.Name}, {"source", got.Source, want.Source}, {"ref", got.Ref, want.Ref}, {"subdir", got.Subdir, want.Subdir}}
		for _, value := range values {
			a := value[1]
			if value[0] == "subdir" {
				if a == "" {
					a = "."
				}
				a, _ = config.Relative(a, true)
			}
			if a != value[2] {
				appendReport(r, "conflicts", map[string]string{"name": want.Name, "field": value[0], "actual": value[1], "expected": value[2]})
			}
		}
	}
	r["vendor_dependencies"] = vendorCount
	for group, wanted := range b.Targets {
		for _, target := range wanted {
			if !contains(c.LinkTargets[group], target) {
				appendReport(r, "missing_links", map[string]string{"name": group, "target": target})
			}
		}
	}
	for group, targets := range c.LinkTargets {
		seen := map[string]bool{}
		for _, target := range targets {
			if seen[target] {
				appendReport(r, "duplicate_links", map[string]string{"name": group, "target": target})
			}
			seen[target] = true
		}
	}
	for _, d := range c.Dependencies {
		found := false
		for _, want := range b.Dependencies {
			if d.MirrorDir == want.MirrorDir {
				found = true
			}
		}
		if !found {
			appendReport(r, "additional_dependencies", d.Name)
		}
	}
	covered := func(file string) bool {
		for dir := range actual {
			if dir != "" && strings.HasPrefix(file, dir+"/") {
				return true
			}
		}
		return false
	}
	for _, cat := range []string{"skills", "workflows", "scripts", "commands"} {
		found := false
		for _, file := range b.Files {
			if strings.HasPrefix(file, cat+"/") {
				found = true
				break
			}
		}
		if !found {
			appendReport(r, "empty_categories", cat)
		}
	}
	for _, file := range b.Files {
		if !covered(file) {
			appendReport(r, "uncovered_files", file)
		}
		if repo != "" && filepath.Base(file) != ".axon-vendor.yaml" {
			p := filepath.Join(repo, filepath.FromSlash(file))
			real, err := filepath.EvalSymlinks(p)
			root, rootErr := filepath.EvalSymlinks(repo)
			if err != nil || rootErr != nil || !strings.HasPrefix(real, root+string(filepath.Separator)) {
				appendReport(r, "missing_repo_files", file)
			} else {
				info, err := os.Stat(p)
				if err != nil || !info.Mode().IsRegular() {
					appendReport(r, "missing_repo_files", file)
				}
			}
		}
	}
	if repo != "" {
		err := filepath.WalkDir(repo, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() == ".git" && d.IsDir() {
				return filepath.SkipDir
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(repo, p)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			group, _, _ := strings.Cut(rel, "/")
			if contains([]string{"skills", "workflows", "scripts", "commands"}, group) && !covered(rel) {
				appendReport(r, "unconfigured_repo_files", rel)
			}
			return nil
		})
		if err != nil {
			return r, err
		}
	}
	for _, key := range []string{"missing_dependencies", "missing_links", "duplicate_links", "conflicts", "uncovered_files", "missing_repo_files", "unconfigured_repo_files"} {
		if len(r[key].([]any)) > 0 {
			r["status"] = "differences"
		}
	}
	return r, nil
}

func (e *Engine) CheckCoverage(ctx context.Context, o CheckOptions) (map[string]any, error) {
	b, err := e.baseline(ctx, o)
	if err != nil {
		return nil, err
	}
	before, err := os.ReadFile(e.Config.Path)
	if err != nil {
		return nil, err
	}
	raw, err := config.Parse(before)
	if err != nil {
		return nil, err
	}
	repo := o.Repo
	if repo != "" {
		repo, err = e.Config.Expand(repo)
		if err != nil {
			return nil, err
		}
	}
	r, err := compare(raw, b, repo)
	if err != nil {
		return nil, err
	}
	if o.Fix && len(r["conflicts"].([]any)) == 0 {
		// Parse a second copy, preserving current dependency values and ordering.
		candidate, _ := config.Parse(before)
		if candidate.LinkTargets == nil {
			candidate.LinkTargets = map[string][]string{}
		}
		for group, wanted := range b.Targets {
			targets := []string{}
			for _, t := range append(candidate.LinkTargets[group], wanted...) {
				if !contains(targets, t) {
					targets = append(targets, t)
				}
			}
			candidate.LinkTargets[group] = targets
		}
		for _, want := range b.Dependencies {
			found := false
			for i := range candidate.Dependencies {
				if candidate.Dependencies[i].MirrorDir == want.MirrorDir {
					found = true
					break
				}
			}
			if !found {
				candidate.Dependencies = append(candidate.Dependencies, want)
			}
		}
		if !reflect.DeepEqual(raw, candidate) {
			candidate.Path = e.Config.Path
			data, err := yaml.Marshal(candidate)
			if err != nil {
				return nil, err
			}
			if err = validateCandidate(data, e.Config.Path); err != nil {
				return nil, err
			}
			backup, err := replaceConfig(e.Config.Path, before, data)
			if err != nil {
				return nil, err
			}
			candidate, _ = config.Parse(data)
			updated, err := compare(candidate, b, repo)
			if err != nil {
				return nil, err
			}
			updated["fixed"] = true
			updated["backup"] = backup
			updated["changes"] = map[string]any{"missing_dependencies": r["missing_dependencies"], "missing_links": r["missing_links"], "duplicate_links": r["duplicate_links"]}
			updated["formatting"] = "YAML formatting normalized; exact original preserved in backup"
			r = updated
		}
	}
	return r, nil
}

func validateCandidate(b []byte, path string) error {
	c, err := config.Parse(b)
	if err != nil {
		return err
	}
	c.Path = path
	return c.Resolve()
}

func replaceConfig(path string, before, after []byte) (string, error) {
	if err := config.NoSymlinks(path); err != nil {
		return "", err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(current, before) {
		return "", fmt.Errorf("configuration changed during check")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	backup, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*.bak")
	if err != nil {
		return "", err
	}
	name := backup.Name()
	if _, err = backup.Write(before); err != nil {
		backup.Close()
		return "", err
	}
	if err = backup.Close(); err != nil {
		return "", err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".orbit-config-")
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(after); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return "", err
	}
	if err = f.Close(); err != nil {
		return "", err
	}
	if err = os.Chmod(f.Name(), info.Mode().Perm()); err != nil {
		return "", err
	}
	current, err = os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !bytes.Equal(current, before) {
		return "", fmt.Errorf("configuration changed before replacement")
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return "", err
	}
	return name, nil
}

func (e *Engine) Check(ctx context.Context, deps []config.Dependency) (map[string]any, error) {
	r := map[string]any{"status": "ok", "verification": "clean cache and configured symlink destinations", "issues": []string{}}
	issues := []string{}
	for _, d := range deps {
		var err error
		unlock := func() {}
		if d.Vendoring() {
			l, lockErr := e.lock(d, false)
			if lockErr != nil {
				issues = append(issues, lockErr.Error())
				continue
			}
			_, _, err = e.ready(ctx, d)
			unlock = func() { l.Unlock() }
		}
		var source string
		if err == nil && d.Link {
			source, _, err = e.localAsset(d)
		}
		if err != nil {
			issues = append(issues, err.Error())
		} else {
			for _, target := range e.Config.Targets(d) {
				got, err := filepath.EvalSymlinks(target)
				wanted, _ := filepath.EvalSymlinks(source)
				info, statErr := os.Lstat(target)
				if err != nil || statErr != nil || info.Mode()&os.ModeSymlink == 0 || got != wanted {
					issues = append(issues, "incorrect or missing link: "+target)
				}
			}
		}
		unlock()
	}
	r["issues"] = issues
	if len(issues) > 0 {
		r["status"] = "differences"
	}
	return r, nil
}
