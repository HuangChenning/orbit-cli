package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
	"github.com/gofrs/flock"
)

type Engine struct {
	Config  *config.Config
	Out     io.Writer
	Jobs    int
	Timeout time.Duration
	Message string
}
type state struct {
	Source  string
	Version string
	Commit  string
	Tag     string
}

func (e *Engine) git(ctx context.Context, dir string, args ...string) (string, error) {
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	base := []string{"--literal-pathspecs", "-c", "core.autocrlf=false", "-C", dir}
	cmd := exec.CommandContext(ctx, "git", append(base, args...)...)
	configureProcess(cmd)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_LFS_SKIP_SMUDGE=1")
	cmd.WaitDelay = time.Second
	b, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

func identity(d config.Dependency) string {
	h := sha256.Sum256([]byte(d.Source + "\x00" + d.Version))
	return hex.EncodeToString(h[:])
}
func (e *Engine) cache(d config.Dependency) string {
	return filepath.Join(e.Config.GlobalCache, identity(d))
}
func (e *Engine) statePath(d config.Dependency) string {
	return filepath.Join(e.Config.GlobalCache, ".state", identity(d)+".json")
}

func acquire(path string) (*flock.Flock, error) {
	if err := config.NoSymlinks(path); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	f := flock.New(path)
	ok, err := f.TryLock()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("another Orbit process holds %s", path)
	}
	return f, nil
}

func (e *Engine) lock(d config.Dependency, create bool) (*flock.Flock, error) {
	if !create {
		if _, err := os.Stat(e.Config.GlobalCache); err != nil {
			return nil, fmt.Errorf("cache missing; run orbit vendor sync: %w", err)
		}
	}
	return acquire(filepath.Join(e.Config.GlobalCache, ".locks", identity(d)+".lock"))
}

func (e *Engine) readState(d config.Dependency) (state, error) {
	var s state
	if err := config.NoSymlinks(e.statePath(d)); err != nil {
		return s, err
	}
	b, err := os.ReadFile(e.statePath(d))
	if err != nil {
		return s, fmt.Errorf("%s cache is not managed; run orbit vendor sync: %w", d.Name, err)
	}
	if err = json.Unmarshal(b, &s); err != nil {
		return s, err
	}
	if s.Source != d.Source || s.Version != d.Version {
		return s, fmt.Errorf("cache ownership mismatch for %s", d.Name)
	}
	return s, nil
}

func (e *Engine) writeState(d config.Dependency, s state) error {
	p := e.statePath(d)
	if err := config.NoSymlinks(p); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), "state-")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

func (e *Engine) verifyRepository(ctx context.Context, d config.Dependency) error {
	p := e.cache(d)
	if err := config.NoSymlinks(filepath.Join(p, ".git")); err != nil {
		return err
	}
	info, err := os.Stat(filepath.Join(p, ".git"))
	if err != nil || !info.IsDir() {
		return fmt.Errorf("cache must contain a real .git directory: %s", p)
	}
	top, err := e.git(ctx, p, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(top)
	if err != nil || actual != real {
		return fmt.Errorf("cache Git root mismatch")
	}
	remote, err := e.git(ctx, p, "remote", "get-url", "origin")
	if err != nil {
		return err
	}
	if remote != d.Source {
		return fmt.Errorf("cache remote ownership mismatch")
	}
	return nil
}

func (e *Engine) ready(ctx context.Context, d config.Dependency) (string, state, error) {
	s, err := e.readState(d)
	if err != nil {
		return "", s, err
	}
	if s.Commit == "" {
		return "", s, fmt.Errorf("incomplete cache; run orbit vendor sync")
	}
	if err = e.verifyRepository(ctx, d); err != nil {
		return "", s, err
	}
	head, err := e.git(ctx, e.cache(d), "rev-parse", "HEAD")
	if err != nil || head != s.Commit {
		return "", s, fmt.Errorf("cache commit changed; run orbit vendor sync")
	}
	status, err := e.git(ctx, e.cache(d), "status", "--porcelain", "--untracked-files=all", "--ignored")
	if err != nil {
		return "", s, err
	}
	if status != "" {
		return "", s, fmt.Errorf("dirty cache for %s; run orbit vendor sync to restore upstream", d.Name)
	}
	p := filepath.Join(e.cache(d), filepath.FromSlash(d.Subdir))
	if err = config.NoSymlinks(p); err != nil {
		return "", s, err
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		return "", s, fmt.Errorf("%s subdir %s is missing or not a directory", d.Name, d.Subdir)
	}
	return p, s, nil
}

var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func (e *Engine) syncOne(ctx context.Context, d config.Dependency) error {
	l, err := e.lock(d, true)
	if err != nil {
		return err
	}
	defer l.Unlock()
	p := e.cache(d)
	if err = config.NoSymlinks(p); err != nil {
		return err
	}
	s, serr := e.readState(d)
	if _, err = os.Lstat(p); os.IsNotExist(err) {
		s = state{Source: d.Source, Version: d.Version}
		temporary, err := os.MkdirTemp(e.Config.GlobalCache, ".init-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(temporary)
		if _, err = e.git(ctx, temporary, "init"); err != nil {
			return err
		}
		if _, err = e.git(ctx, temporary, "remote", "add", "origin", d.Source); err != nil {
			return err
		}
		if err = e.writeState(d, s); err != nil {
			return err
		}
		if err = os.Rename(temporary, p); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else if serr != nil {
		return serr
	}
	if err = e.verifyRepository(ctx, d); err != nil {
		return err
	}
	v := d.Version
	ref := v
	tag := ""
	refspec := v
	if !fullSHA.MatchString(v) {
		candidates := []string{"refs/heads/" + v, "refs/tags/" + v}
		if strings.HasPrefix(v, "refs/heads/") || strings.HasPrefix(v, "refs/tags/") {
			candidates = []string{v}
		}
		args := append([]string{"ls-remote", "--refs", "origin"}, candidates...)
		advertised, err := e.git(ctx, p, args...)
		if err != nil {
			return err
		}
		available := map[string]bool{}
		for _, line := range strings.Split(advertised, "\n") {
			fields := strings.Fields(line)
			if len(fields) == 2 {
				available[fields[1]] = true
			}
		}
		remoteRef := ""
		for _, candidate := range candidates {
			if available[candidate] {
				if remoteRef != "" {
					return fmt.Errorf("ambiguous branch/tag %q; use refs/heads/ or refs/tags/", v)
				}
				remoteRef = candidate
			}
		}
		if remoteRef == "" {
			return fmt.Errorf("upstream version %q does not exist", v)
		}
		ref = "refs/orbit/" + strings.TrimPrefix(remoteRef, "refs/")
		refspec = "+" + remoteRef + ":" + ref
		if strings.HasPrefix(remoteRef, "refs/tags/") {
			tag = ref
		}
	}
	if _, err = e.git(ctx, p, "fetch", "--depth=1", "--no-tags", "origin", refspec); err != nil {
		return err
	}
	commit, err := e.git(ctx, p, "rev-parse", "--verify", ref+"^{commit}")
	ok := err == nil
	if !ok {
		return fmt.Errorf("cannot resolve version %q", v)
	}
	if tag != "" {
		tag, err = e.git(ctx, p, "rev-parse", "--verify", tag)
		if err != nil {
			return err
		}
	}
	if s.Tag != "" && s.Tag != tag {
		return fmt.Errorf("tag %s moved; refusing to replace pinned content", v)
	}
	if _, err = e.git(ctx, p, "reset", "--hard", commit); err != nil {
		return err
	}
	if _, err = e.git(ctx, p, "clean", "-ffdx"); err != nil {
		return err
	}
	if err = e.unsupported(ctx, p); err != nil {
		return err
	}
	s.Commit = commit
	s.Tag = tag
	return e.writeState(d, s)
}

func (e *Engine) unsupported(ctx context.Context, p string) error {
	tree, err := e.git(ctx, p, "ls-files", "--stage")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(tree, "\n") {
		if strings.HasPrefix(line, "160000 ") {
			return fmt.Errorf("Git submodules are not supported")
		}
	}
	return filepath.WalkDir(p, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		b := make([]byte, 200)
		n, _ := f.Read(b)
		if bytes := string(b[:n]); strings.HasPrefix(bytes, "version https://git-lfs.github.com/spec/v1") {
			return fmt.Errorf("Git LFS is not supported: %s", path)
		}
		return nil
	})
}

func unique(deps []config.Dependency) []config.Dependency {
	seen := map[string]bool{}
	var out []config.Dependency
	for _, d := range deps {
		k := identity(d)
		if !seen[k] {
			seen[k] = true
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return identity(out[i]) < identity(out[j]) })
	return out
}

func (e *Engine) Sync(ctx context.Context, deps []config.Dependency) error {
	if len(deps) == 0 {
		return fmt.Errorf("no dependencies configured")
	}
	jobs := e.Jobs
	if jobs <= 0 {
		jobs = 4
	}
	queue := make(chan config.Dependency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error
	for i := 0; i < jobs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for d := range queue {
				err := e.syncOne(ctx, d)
				mu.Lock()
				if err != nil {
					errs = append(errs, fmt.Errorf("%s: %w", d.Name, err))
				} else {
					fmt.Fprintln(e.Out, "synced", d.Name)
				}
				mu.Unlock()
			}
		}()
	}
	for _, d := range unique(deps) {
		queue <- d
	}
	close(queue)
	wg.Wait()
	// Validate every selected subdir even when multiple dependencies share a clone.
	if len(errs) == 0 {
		for _, d := range deps {
			l, err := e.lock(d, false)
			if err == nil {
				_, _, err = e.ready(ctx, d)
				l.Unlock()
			}
			if err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}
