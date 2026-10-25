package engine

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/HuangChenning/orbit-cli/v2/src/internal/config"
	ignore "github.com/sabhiram/go-gitignore"
	"gopkg.in/yaml.v3"
)

type skillOccurrence struct {
	Dependency string `json:"dependency"`
	Path       string `json:"path"`
}

// CheckDuplicates inspects effective selections and available local skill files.
// It does not fetch upstreams, modify configuration, or compare entire asset trees.
func (e *Engine) CheckDuplicates(deps []config.Dependency) (map[string]any, error) {
	r := map[string]any{
		"status": "ok", "config": e.Config.Path,
		"verification":      "configured sources and available local SKILL.md names/bytes; .orbitignore applied; no upstream or whole-asset verification",
		"duplicate_sources": []any{}, "overlapping_sources": []any{},
		"duplicate_skill_names": []any{}, "duplicate_skill_content": []any{},
		"unverified_assets": []any{}, "excluded_paths": 0, "skills_checked": 0,
	}
	var matcher *ignore.GitIgnore
	b, err := os.ReadFile(filepath.Join(filepath.Dir(e.Config.Path), ".orbitignore"))
	if err == nil {
		matcher = ignore.CompileIgnoreLines(strings.Split(string(b), "\n")...)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	bySource := map[string][]string{}
	for i, d := range deps {
		key := d.Source + "\x00" + d.Ref + "\x00" + d.Subdir
		bySource[key] = append(bySource[key], d.Name)
		for _, other := range deps[:i] {
			if d.Source == other.Source && d.Ref == other.Ref && d.Subdir != other.Subdir &&
				(d.Subdir == "." || other.Subdir == "." || strings.HasPrefix(d.Subdir, other.Subdir+"/") || strings.HasPrefix(other.Subdir, d.Subdir+"/")) {
				parent, child := other, d
				if d.Subdir == "." || strings.HasPrefix(other.Subdir, d.Subdir+"/") {
					parent, child = d, other
				}
				rel, err := filepath.Rel(filepath.FromSlash(parent.Subdir), filepath.FromSlash(child.Subdir))
				if err != nil {
					return nil, err
				}
				// A collection can exclude the entire separately configured subtree.
				if matcher != nil && parent.MirrorDir != "" && matcher.MatchesPath(filepath.ToSlash(filepath.Join(parent.MirrorDir, rel))+"/") {
					continue
				}
				appendReport(r, "overlapping_sources", []string{other.Name, d.Name})
			}
		}
	}
	keys := make([]string, 0, len(bySource))
	for key := range bySource {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if len(bySource[key]) > 1 {
			appendReport(r, "duplicate_sources", bySource[key])
		}
	}
	names, hashes := map[string][]skillOccurrence{}, map[string][]skillOccurrence{}
	for _, d := range deps {
		if d.MirrorDir == "" || e.Config.OrbitRepo == nil {
			appendReport(r, "unverified_assets", map[string]string{"dependency": d.Name, "reason": "no local asset destination configured"})
			continue
		}
		root := filepath.Join(e.Config.OrbitRepo.LocalPath, filepath.FromSlash(d.MirrorDir))
		if err := config.NoSymlinks(root); err != nil {
			return nil, err
		}
		if info, err := os.Stat(root); os.IsNotExist(err) {
			appendReport(r, "unverified_assets", map[string]string{"dependency": d.Name, "reason": "local asset not downloaded", "path": root})
			continue
		} else if err != nil {
			return nil, err
		} else if !info.IsDir() {
			appendReport(r, "unverified_assets", map[string]string{"dependency": d.Name, "reason": "local asset is not a directory", "path": root})
			continue
		}
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			mirrorPath := filepath.ToSlash(filepath.Join(d.MirrorDir, rel))
			matchPath := mirrorPath
			if entry.IsDir() {
				matchPath += "/"
			}
			if matcher != nil && matcher.MatchesPath(matchPath) {
				r["excluded_paths"] = r["excluded_paths"].(int) + 1
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				appendReport(r, "unverified_assets", map[string]string{"dependency": d.Name, "reason": "symlink not followed", "path": mirrorPath})
				return nil
			}
			if entry.Name() != "SKILL.md" || entry.IsDir() {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("skill is not a regular file: %s", path)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			name := filepath.Base(filepath.Dir(path))
			text := strings.ReplaceAll(string(data), "\r\n", "\n")
			if strings.HasPrefix(text, "---\n") {
				front, _, ok := strings.Cut(text[4:], "\n---")
				if ok {
					var metadata struct {
						Name string `yaml:"name"`
					}
					if err := yaml.Unmarshal([]byte(front), &metadata); err != nil {
						return fmt.Errorf("%s frontmatter: %w", mirrorPath, err)
					}
					if strings.TrimSpace(metadata.Name) != "" {
						name = metadata.Name
					}
				}
			}
			item := skillOccurrence{d.Name, mirrorPath}
			names[name] = append(names[name], item)
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			hashes[hash] = append(hashes[hash], item)
			r["skills_checked"] = r["skills_checked"].(int) + 1
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for _, group := range []struct {
		Field string
		Items map[string][]skillOccurrence
		Key   string
	}{
		{"duplicate_skill_names", names, "name"}, {"duplicate_skill_content", hashes, "sha256"},
	} {
		keys := make([]string, 0, len(group.Items))
		for key := range group.Items {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			items := group.Items[key]
			owners := map[string]bool{}
			for _, item := range items {
				owners[item.Dependency] = true
			}
			if len(owners) > 1 {
				appendReport(r, group.Field, map[string]any{group.Key: key, "occurrences": items})
			}
		}
	}
	if len(r["unverified_assets"].([]any)) > 0 {
		r["status"] = "incomplete"
	}
	for _, field := range []string{"duplicate_sources", "overlapping_sources", "duplicate_skill_names", "duplicate_skill_content"} {
		if len(r[field].([]any)) > 0 {
			r["status"] = "differences"
		}
	}
	return r, nil
}
