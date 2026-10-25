// gitshim injects deterministic failures without relying on a host shell.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

func run(cmd *exec.Cmd) int {
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signals)
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		for {
			select {
			case sig := <-signals:
				_ = cmd.Process.Signal(sig)
			case <-done:
				return
			}
		}
	}()
	if err := cmd.Wait(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return exit.ExitCode()
		}
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--delayed-marker" {
		if err := os.WriteFile(os.Args[2]+".started", []byte("started\n"), 0600); err != nil {
			os.Exit(1)
		}
		// Bound handshake failure so a broken test cannot leave this child alive.
		readyDeadline := time.Now().Add(15 * time.Second)
		for {
			if _, err := os.Stat(os.Args[2] + ".release"); err == nil {
				break
			}
			if time.Now().After(readyDeadline) {
				fmt.Fprintln(os.Stderr, "timed out waiting for marker release")
				os.Exit(1)
			}
			time.Sleep(20 * time.Millisecond)
		}
		time.Sleep(12 * time.Second)
		if err := os.WriteFile(os.Args[2], []byte("escaped\n"), 0600); err != nil {
			os.Exit(1)
		}
		return
	}
	for _, arg := range os.Args[1:] {
		if os.Getenv("ORBIT_TEST_GIT_MODE") == "pause-fetch" && arg == "fetch" {
			marker := os.Getenv("ORBIT_TEST_GIT_MARKER")
			if err := os.WriteFile(marker+".started", []byte("started\n"), 0600); err != nil {
				panic(err)
			}
			deadline := time.Now().Add(15 * time.Second)
			for {
				if _, err := os.Stat(marker + ".release"); err == nil {
					break
				}
				if time.Now().After(deadline) {
					fmt.Fprintln(os.Stderr, "timed out waiting for fetch release")
					os.Exit(1)
				}
				time.Sleep(20 * time.Millisecond)
			}
		}
		if os.Getenv("ORBIT_TEST_GIT_MODE") == "fail-clone" && arg == "clone" {
			dest := os.Args[len(os.Args)-1]
			if err := os.MkdirAll(dest, 0700); err != nil {
				panic(err)
			}
			if err := os.WriteFile(dest+string(os.PathSeparator)+"partial-clone", []byte("incomplete\n"), 0600); err != nil {
				panic(err)
			}
			fmt.Fprintln(os.Stderr, "injected clone failure after destination creation")
			os.Exit(1)
		}
		if os.Getenv("ORBIT_TEST_GIT_MODE") == "fail-init" && arg == "init" {
			os.Exit(1)
		}
		if os.Getenv("ORBIT_TEST_GIT_MODE") == "timeout-fetch" && arg == "fetch" {
			self, err := os.Executable()
			if err != nil {
				panic(err)
			}
			os.Exit(run(exec.Command(self, "--delayed-marker", os.Getenv("ORBIT_TEST_GIT_MARKER"))))
		}
	}
	realGit := os.Getenv("ORBIT_TEST_REAL_GIT")
	if realGit == "" {
		fmt.Fprintln(os.Stderr, "ORBIT_TEST_REAL_GIT is required")
		os.Exit(1)
	}
	os.Exit(run(exec.Command(realGit, os.Args[1:]...)))
}
