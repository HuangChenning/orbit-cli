//go:build !windows

package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/HuangChenning/orbit-cli/v2/src/internal/config"
)

func localGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	base := []string{"-c", "core.hooksPath=/dev/null", "-c", "commit.gpgsign=false", "-C", dir}
	b, err := exec.Command("git", append(base, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, b)
	}
	return strings.TrimSpace(string(b))
}

func TestInterruptedFetchCanSyncAgain(t *testing.T) {
	for _, reason := range []string{"canceled", "timeout"} {
		t.Run(reason, func(t *testing.T) {
			interruptedFetchCanSyncAgain(t, reason)
		})
	}
}

func interruptedFetchCanSyncAgain(t *testing.T, reason string) {
	upstream := t.TempDir()
	localGit(t, upstream, "init", "-b", "main")
	localGit(t, upstream, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	want := localGit(t, upstream, "rev-parse", "HEAD")
	d := config.Dependency{Name: "fixture", Source: upstream, Ref: "main"}
	e := &Engine{Config: &config.Config{GlobalCache: t.TempDir()}, Timeout: 10 * time.Second}
	cache := e.cache(d)
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	localGit(t, cache, "init")
	localGit(t, cache, "remote", "add", "origin", upstream)
	if err := e.writeState(d, state{Source: upstream, Version: "main"}); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(t.TempDir(), "hook-entered")
	t.Setenv("ORBIT_TEST_HOOK_MARKER", marker)
	hook := filepath.Join(t.TempDir(), "pack-objects")
	// Pause a real fetch while Git owns shallow.lock, without using the network.
	if err := os.WriteFile(hook, []byte("#!/bin/sh\n: > \"$ORBIT_TEST_HOOK_MARKER\"\nsleep 30\nexec \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ORBIT_TEST_PACK_HOOK", "'"+strings.ReplaceAll(hook, "'", "'\\''")+"'")
	upload := filepath.Join(t.TempDir(), "upload-pack")
	if err := os.WriteFile(upload, []byte("#!/bin/sh\nexec git -c \"uploadpack.packObjectsHook=$ORBIT_TEST_PACK_HOOK\" upload-pack \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	wantErr := context.Canceled
	if reason == "timeout" {
		cancel()
		ctx, cancel = context.WithTimeout(context.Background(), 1500*time.Millisecond)
		wantErr = context.DeadlineExceeded
	}
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := e.git(ctx, cache, "fetch", "--upload-pack=sh '"+strings.ReplaceAll(upload, "'", "'\\''")+"'", "--depth=1", "--no-tags", "origin", "+refs/heads/main:refs/orbit/heads/main")
		result <- err
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("fetch did not reach pack transfer: %v", <-result)
		}
		time.Sleep(10 * time.Millisecond)
	}
	lock := filepath.Join(cache, ".git", "shallow.lock")
	if _, err := os.Stat(lock); err != nil {
		cancel()
		<-result
		t.Fatalf("fixture must interrupt fetch while shallow.lock exists: %v", err)
	}
	if reason == "canceled" {
		cancel()
	}
	err := <-result
	if _, statErr := os.Stat(lock); !os.IsNotExist(statErr) {
		t.Fatalf("interrupted fetch stranded shallow.lock and will block the next sync: %v (fetch: %v)", statErr, err)
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("interrupted Git command must preserve %v: %v", wantErr, err)
	}
	if err := e.syncOne(context.Background(), d); err != nil {
		t.Fatalf("sync after interruption: %v", err)
	}
	_, got, err := e.ready(context.Background(), d)
	if err != nil || got.Commit != want {
		t.Fatalf("retry must restore a valid cache at the upstream commit: %+v, %v", got, err)
	}
}

func TestGitCancellationKillsUncooperativeDescendants(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "leaked-child")
	t.Setenv("ORBIT_TEST_CHILD_MARKER", marker)
	e := &Engine{Timeout: 100 * time.Millisecond}
	started := time.Now()
	_, err := e.git(context.Background(), t.TempDir(), "-c", "alias.wait=!trap '' TERM; sleep 2; echo leaked > \"$ORBIT_TEST_CHILD_MARKER\"", "wait")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline: %v", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("a child ignoring SIGTERM must not extend the Git operation indefinitely: %s", elapsed)
	}
	// An escaped child would write this marker after its sleep ends.
	time.Sleep(time.Until(started.Add(2500 * time.Millisecond)))
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Git cancellation left a child running: %v", err)
	}
}

func TestSyncPreservesUnknownGitLockAndExplainsRecovery(t *testing.T) {
	d := config.Dependency{Name: "fixture", Source: t.TempDir(), Ref: "main"}
	e := &Engine{Config: &config.Config{GlobalCache: t.TempDir()}}
	cache := e.cache(d)
	if err := os.Mkdir(cache, 0700); err != nil {
		t.Fatal(err)
	}
	localGit(t, cache, "init")
	localGit(t, cache, "remote", "add", "origin", d.Source)
	if err := e.writeState(d, state{Source: d.Source, Version: "main"}); err != nil {
		t.Fatal(err)
	}
	lock := filepath.Join(cache, ".git", "shallow.lock")
	const contents = "another Git process owns this lock\n"
	if err := os.WriteFile(lock, []byte(contents), 0600); err != nil {
		t.Fatal(err)
	}
	err := e.syncOne(context.Background(), d)
	if err == nil || !strings.Contains(err.Error(), lock) || !strings.Contains(err.Error(), "confirming it is stale") {
		t.Fatalf("an unknown Git lock must stop sync and explain safe recovery: %v", err)
	}
	got, readErr := os.ReadFile(lock)
	if readErr != nil || string(got) != contents {
		t.Fatalf("sync must not remove or change an unknown Git lock: %q, %v", got, readErr)
	}
}
