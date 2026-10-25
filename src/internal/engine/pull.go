package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/HuangChenning/orbit-cli/v2/src/internal/config"
	"gopkg.in/yaml.v3"
)

// Pull restores a personal repository, then registers its manifest locally.
// It never fetches dependency sources or discards local work.
func (e *Engine) Pull(ctx context.Context, updateConfig bool) error {
	if e.Config.OrbitRepo == nil {
		return fmt.Errorf("orbit_repo is required for pull")
	}
	r := e.Config.OrbitRepo
	lock, err := acquire(r.LocalPath + ".orbit.lock")
	if err != nil {
		return err
	}
	defer lock.Unlock()
	configLock, err := acquire(e.Config.Path + ".orbit.lock")
	if err != nil {
		return err
	}
	defer configLock.Unlock()
	if err = config.NoSymlinks(e.Config.Path); err != nil {
		return err
	}
	original, err := os.ReadFile(e.Config.Path)
	if err != nil {
		return err
	}
	// Reject edits made since command loading rather than merging against stale settings.
	fresh, err := config.Parse(original)
	if err != nil {
		return err
	}
	fresh.Path = e.Config.Path
	if err = fresh.Resolve(); err != nil {
		return err
	}
	if fresh.OrbitRepo == nil || *fresh.OrbitRepo != *r {
		return fmt.Errorf("repository configuration changed; retry pull")
	}
	e.Config = fresh
	r = fresh.OrbitRepo
	_, statErr := os.Lstat(r.LocalPath)
	initial := os.IsNotExist(statErr)
	if statErr != nil && !initial {
		return statErr
	}
	if err = config.NoSymlinks(r.LocalPath); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(r.LocalPath), 0700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Dir(r.LocalPath), ".orbit-pull-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	var commit, previous string
	if initial {
		fmt.Fprintln(e.Out, "cloning personal repository branch", r.Branch)
		if _, err = e.git(ctx, filepath.Dir(stage), "clone", "--no-checkout", "--single-branch", "--branch", r.Branch, "--", r.Remote, stage); err != nil {
			return err
		}
		commit, err = e.git(ctx, stage, "rev-parse", "--verify", "refs/remotes/origin/"+r.Branch)
	} else {
		if err = e.prepareRepository(ctx); err != nil {
			return err
		}
		top, topErr := e.git(ctx, r.LocalPath, "rev-parse", "--show-toplevel")
		real, realErr := filepath.EvalSymlinks(r.LocalPath)
		actual, actualErr := filepath.EvalSymlinks(top)
		if topErr != nil || realErr != nil || actualErr != nil || real != actual {
			return fmt.Errorf("personal repository Git root mismatch")
		}
		if err = e.requireCleanPull(ctx); err != nil {
			return err
		}
		previous, err = e.git(ctx, r.LocalPath, "rev-parse", "--verify", "HEAD")
		if err != nil {
			return err
		}
		fmt.Fprintln(e.Out, "fetching personal repository branch", r.Branch)
		if _, err = e.git(ctx, r.LocalPath, "fetch", "--no-tags", "origin", "+refs/heads/"+r.Branch+":refs/remotes/origin/"+r.Branch); err != nil {
			return err
		}
		commit, err = e.git(ctx, r.LocalPath, "rev-parse", "--verify", "refs/remotes/origin/"+r.Branch)
		if err == nil {
			if _, err = e.git(ctx, r.LocalPath, "merge-base", "--is-ancestor", "HEAD", commit); err != nil {
				return fmt.Errorf("pull requires a fast-forward; local commits are ahead or histories diverged; review and publish/reconcile them before retrying")
			}
			// Clone local objects into a disposable checkout to validate the fetched snapshot.
			_, err = e.git(ctx, filepath.Dir(stage), "clone", "--no-checkout", "--no-hardlinks", "--", r.LocalPath, stage)
		}
	}
	if err != nil {
		return err
	}
	if _, err = e.git(ctx, stage, "-c", "core.hooksPath="+os.DevNull, "checkout", "-B", r.Branch, commit); err != nil {
		return err
	}
	incoming, err := e.validatePull(ctx, stage)
	if err != nil {
		return err
	}
	candidate, err := fresh.RegisterDependencies(original, incoming, updateConfig)
	if err != nil {
		return err
	}
	// Prepare a complete file before changing the repository; disk/permission failures
	// here must not advance the checkout or replace the existing configuration.
	var configTemp string
	if !bytes.Equal(candidate, original) {
		file, err := os.CreateTemp(filepath.Dir(fresh.Path), ".orbit-config-")
		if err != nil {
			return err
		}
		configTemp = file.Name()
		defer os.Remove(configTemp)
		_, writeErr := file.Write(candidate)
		if writeErr == nil {
			writeErr = file.Sync()
		}
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err = unchangedConfig(fresh.Path, original); err != nil {
		return err
	}
	if initial {
		if _, err = os.Lstat(r.LocalPath); !os.IsNotExist(err) {
			return fmt.Errorf("repository destination appeared during pull; refusing overwrite")
		}
		if err = os.Rename(stage, r.LocalPath); err != nil {
			return err
		}
	} else {
		if err = e.requireCleanPull(ctx); err != nil {
			return err
		}
		if _, err = e.git(ctx, r.LocalPath, "-c", "core.hooksPath="+os.DevNull, "merge", "--ff-only", "--no-autostash", commit); err != nil {
			return err
		}
	}
	if configTemp != "" {
		err = unchangedConfig(fresh.Path, original)
		if err == nil {
			err = os.Rename(configTemp, fresh.Path)
		}
		if err != nil {
			if initial {
				if rollbackErr := os.Rename(r.LocalPath, stage); rollbackErr != nil {
					return fmt.Errorf("configuration write failed: %v; initial checkout rollback failed: %w", err, rollbackErr)
				}
			}
			return fmt.Errorf("configuration not written: %w; review configuration and retry pull (existing repository may already be updated)", err)
		}
	}
	fmt.Fprintln(e.Out, "registered", len(incoming), "remote assets in", fresh.Path)
	if previous == commit {
		fmt.Fprintln(e.Out, "personal repository unchanged")
	}
	fmt.Fprintln(e.Out, "pull complete:", commit, "at", r.LocalPath)
	return nil
}

func unchangedConfig(path string, original []byte) error {
	if err := config.NoSymlinks(path); err != nil {
		return err
	}
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, original) {
		return fmt.Errorf("configuration changed during pull; refusing overwrite")
	}
	return nil
}

func (e *Engine) requireCleanPull(ctx context.Context) error {
	status, err := e.git(ctx, e.Config.OrbitRepo.LocalPath, "status", "--porcelain", "--untracked-files=all", "--ignored")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("pull blocked by local working tree changes; commit or preserve them first:\n%s", status)
	}
	return nil
}

func (e *Engine) validatePull(ctx context.Context, stage string) ([]config.Dependency, error) {
	path := filepath.Join(stage, ".orbit-manifest.yaml")
	if err := config.NoSymlinks(path); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("personal repository must contain .orbit-manifest.yaml: %w", err)
	}
	var header Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err = decoder.Decode(&header); err != nil || header.Version != "1.0" {
		return nil, fmt.Errorf("remote manifest requires version 1.0 and known fields: %v", err)
	}
	manifest, err := parseManifest(data)
	if err != nil {
		return nil, err
	}
	tree, err := e.git(ctx, stage, "ls-tree", "-r", "HEAD")
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(tree, "\n") {
		if strings.HasPrefix(line, "160000 ") {
			return nil, fmt.Errorf("pull does not support nested Git repositories/submodules: %s", line)
		}
	}
	validation, err := os.MkdirTemp("", "orbit-pull-validate-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(validation)
	var deps []config.Dependency
	for _, entry := range manifest.Dependencies {
		if strings.TrimSpace(entry.Name) == "" || entry.Source == "" || entry.Version == "" || !fullSHA.MatchString(entry.Commit) {
			return nil, fmt.Errorf("incomplete asset identity in remote manifest: %q", entry.Name)
		}
		source := filepath.Join(stage, filepath.FromSlash(entry.MirrorDir))
		if err := config.NoSymlinks(source); err != nil {
			return nil, err
		}
		info, err := os.Stat(source)
		if err != nil || !info.IsDir() {
			return nil, fmt.Errorf("remote asset directory missing: %s", entry.MirrorDir)
		}
		dest, err := os.MkdirTemp(validation, "asset-")
		if err != nil {
			return nil, err
		}
		if err = copyAsset(source, dest, entry.MirrorDir, nil); err != nil {
			return nil, err
		}
		vendor := false
		group, _, _ := strings.Cut(entry.MirrorDir, "/")
		deps = append(deps, config.Dependency{Name: entry.Name, Source: entry.Source, Ref: entry.Version, Subdir: entry.Subdir, MirrorDir: entry.MirrorDir, Vendor: &vendor, Link: len(e.Config.LinkTargets[group]) > 0})
	}
	if err = e.verifySnapshotBytes(ctx, stage, manifest.Dependencies); err != nil {
		return nil, err
	}
	return deps, nil
}
