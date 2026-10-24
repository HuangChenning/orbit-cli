package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/HuangChenning/orbit-cli/internal/config"
	"github.com/HuangChenning/orbit-cli/internal/engine"
	"github.com/spf13/cobra"
)

const template = `version: "1.0"
global_cache: "~/.orbit/cache"
link_targets:
  skills:
    - "~/.claude/skills"
dependencies: []
# Default mirror local_path: ~/.orbit/repo (set orbit_repo.remote and branch first).
# Add dependencies with name, source, version, subdir, mirror_dir and link.
# Example: mirror_dir: skills/example maps to ~/.claude/skills/example.
`

func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := &cobra.Command{Use: "orbit", Short: "Sync, link and mirror upstream Git assets", SilenceUsage: true, SilenceErrors: true}
	var explicit string
	var labels []string
	var jobs int
	var timeout time.Duration
	root.PersistentFlags().StringVar(&explicit, "config", "", "configuration path (otherwise search parent directories)")
	root.PersistentFlags().StringArrayVarP(&labels, "label", "l", nil, "select any matching label; repeat for OR")
	root.PersistentFlags().IntVar(&jobs, "jobs", 4, "maximum concurrent Git sync operations")
	root.PersistentFlags().DurationVar(&timeout, "timeout", 2*time.Minute, "timeout per Git operation")
	load := func(coverage bool) (*engine.Engine, []config.Dependency, error) {
		if jobs < 1 || timeout <= 0 {
			return nil, nil, fmt.Errorf("jobs and timeout must be positive")
		}
		if _, err := os.Stat(os.DevNull); err != nil {
			return nil, nil, err
		}
		cwd, err := os.Getwd()
		if err != nil {
			return nil, nil, err
		}
		path, err := config.Find(cwd, explicit)
		if err != nil {
			return nil, nil, err
		}
		loader := config.Load
		if coverage {
			loader = config.LoadCoverage
		}
		cfg, err := loader(path)
		if err != nil {
			return nil, nil, err
		}
		deps, err := cfg.Select(labels)
		return &engine.Engine{Config: cfg, Out: root.OutOrStdout(), Jobs: jobs, Timeout: timeout}, deps, err
	}
	var initPath string
	init := &cobra.Command{Use: "init", Short: "Create orbit.yaml without overwriting an existing file", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		p := filepath.Join(initPath, "orbit.yaml")
		if err := config.NoSymlinks(filepath.Dir(p)); err != nil {
			return err
		}
		if err := os.MkdirAll(initPath, 0700); err != nil {
			return err
		}
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.WriteString(template)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Fprintln(cmd.OutOrStdout(), p)
		return nil
	}}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	init.Flags().StringVarP(&initPath, "path", "p", filepath.Join(home, ".orbit"), "directory for orbit.yaml")
	root.AddCommand(init)
	root.AddCommand(&cobra.Command{Use: "sync", Short: "Fetch upstream and discard managed cache edits", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		return e.Sync(cmd.Context(), d)
	}})
	var linkForce bool
	link := &cobra.Command{Use: "link", Short: "Reconcile global targets with dependency link switches", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		return e.Link(cmd.Context(), d, linkForce)
	}}
	link.Flags().BoolVarP(&linkForce, "force", "f", false, "replace conflicting symlinks only; real files are protected")
	root.AddCommand(link)
	var force, yes bool
	var message string
	mirror := &cobra.Command{Use: "mirror", Short: "Publish clean cached assets to orbit_repo", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		if force && !yes {
			if err = confirm(cmd, "Rewrite remote mirror history?"); err != nil {
				return err
			}
		}
		e.Message = message
		return e.Mirror(cmd.Context(), d, len(labels) == 0, force)
	}}
	mirror.Flags().BoolVarP(&force, "force", "f", false, "allow forced push after explicit confirmation")
	mirror.Flags().BoolVar(&yes, "yes", false, "confirm the requested force operation")
	mirror.Flags().StringVar(&message, "message", "Update Orbit asset snapshot", "mirror commit message")
	root.AddCommand(mirror)
	var cleanYes bool
	clean := &cobra.Command{Use: "clean", Short: "Remove only the selected, owned cache clones", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		for _, dep := range d {
			fmt.Fprintln(cmd.OutOrStdout(), "remove cache:", dep.Name)
		}
		if !cleanYes {
			if err = confirm(cmd, "Remove these caches? Existing links will need orbit sync."); err != nil {
				return err
			}
		}
		return e.Clean(cmd.Context(), d)
	}}
	clean.Flags().BoolVar(&cleanYes, "yes", false, "confirm cache removal")
	root.AddCommand(clean)
	addCheck(root, load)
	return root.ExecuteContext(ctx)
}

func confirm(cmd *cobra.Command, message string) error {
	fmt.Fprint(cmd.OutOrStdout(), message+" [y/N] ")
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil {
		return fmt.Errorf("confirmation required; use --yes explicitly")
	}
	if strings.TrimSpace(strings.ToLower(line)) != "y" {
		return fmt.Errorf("cancelled")
	}
	return nil
}
