package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
	"github.com/HuangChenning/orbit-cli/src/internal/engine"
	"github.com/HuangChenning/orbit-cli/src/internal/updater"
	"github.com/spf13/cobra"
)

const template = `version: "1.0"
global_cache: "~/.orbit/cache"
link_targets:
  skills:
    - "~/.claude/skills"
dependencies: []
# Set orbit_repo.remote and branch before orbit vendor sync.
# Default orbit_repo.local_path: ~/.orbit/repo.
# Add dependencies with name, source, ref, subdir, mirror_dir, vendor, link and optional labels.
# vendor defaults to true; link defaults to false; put labels last.
# Example: mirror_dir: skills/example maps to ~/.claude/skills/example.
`

func Execute() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	root := &cobra.Command{Use: "orbit", Short: "Download, link and publish Git-backed assets", SilenceUsage: true, SilenceErrors: true}
	root.Version = Version
	root.SetVersionTemplate(fmt.Sprintf("orbit {{.Version}}\ncommit: %s\nbuilt: %s\ngo: %s\n", Commit, BuildTime, runtime.Version()))
	var explicit string
	var labels, names []string
	var jobs int
	var timeout time.Duration
	root.PersistentFlags().StringVar(&explicit, "config", "", "configuration path (otherwise search parent directories)")
	root.PersistentFlags().StringArrayVarP(&labels, "label", "l", nil, "select matching labels; comma-separated or repeated for OR")
	root.PersistentFlags().StringArrayVar(&names, "name", nil, "select exact asset names; comma-separated or repeated")
	root.PersistentFlags().IntVar(&jobs, "jobs", 4, "maximum concurrent vendor download operations")
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
		deps, err := cfg.Select(labels, names)
		return &engine.Engine{Config: cfg, Out: root.OutOrStdout(), Jobs: jobs, Timeout: timeout}, deps, err
	}
	var initPath string
	init := &cobra.Command{Use: "init", Short: "Create orbit.yaml without overwriting an existing file", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if initPath == "" {
			return fmt.Errorf("home directory unavailable; use init --path")
		}
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
	defaultInitPath := ""
	if err == nil {
		defaultInitPath = filepath.Join(home, ".orbit")
	}
	init.Flags().StringVarP(&initPath, "path", "p", defaultInitPath, "directory for orbit.yaml")
	root.AddCommand(init)
	vendor := &cobra.Command{Use: "vendor", Short: "Manage upstream assets"}
	vendor.AddCommand(&cobra.Command{Use: "sync", Short: "Download upstream assets into the local Orbit repository", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		if e.Config.OrbitRepo == nil {
			return fmt.Errorf("orbit_repo is required for vendor sync")
		}
		var enabled []config.Dependency
		for _, dep := range d {
			if dep.Vendoring() {
				enabled = append(enabled, dep)
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "skipped", dep.Name, "(vendor: false)")
			}
		}
		if len(d) > 0 && len(enabled) == 0 {
			return nil
		}
		d = enabled
		if err = e.Sync(cmd.Context(), d); err != nil {
			return err
		}
		for _, dep := range d {
			if dep.MirrorDir != "" {
				return e.Assemble(cmd.Context(), d, len(labels) == 0 && len(names) == 0)
			}
		}
		return nil
	}})
	root.AddCommand(vendor)
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
	publish := &cobra.Command{Use: "sync", Short: "Commit and push local Orbit assets to the personal remote", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		e, d, err := load(false)
		if err != nil {
			return err
		}
		if force && !yes {
			if err = confirm(cmd, "Rewrite personal repository history?"); err != nil {
				return err
			}
		}
		e.Message = message
		return e.Publish(cmd.Context(), d, len(labels) == 0 && len(names) == 0, force)
	}}
	publish.Flags().BoolVarP(&force, "force", "f", false, "allow forced push after explicit confirmation")
	publish.Flags().BoolVar(&yes, "yes", false, "confirm the requested force operation")
	publish.Flags().StringVar(&message, "message", "Update Orbit asset snapshot", "sync commit message")
	root.AddCommand(publish)
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
			if err = confirm(cmd, "Remove these caches? Assembled assets and links are preserved."); err != nil {
				return err
			}
		}
		return e.Clean(cmd.Context(), d)
	}}
	clean.Flags().BoolVar(&cleanYes, "yes", false, "confirm cache removal")
	root.AddCommand(clean)
	update := &cobra.Command{Use: "update", Short: "Install the latest stable Orbit release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		result, err := updater.Update(cmd.Context(), Version)
		if err != nil {
			return err
		}
		printUpdateResult(cmd, result, true)
		return nil
	}}
	update.AddCommand(&cobra.Command{Use: "check", Short: "Check the latest stable Orbit release", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		result, err := updater.Check(cmd.Context(), Version)
		if err != nil {
			return err
		}
		printUpdateResult(cmd, result, false)
		return nil
	}})
	root.AddCommand(update)
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

func printUpdateResult(cmd *cobra.Command, result updater.Result, installed bool) {
	fmt.Fprintf(cmd.OutOrStdout(), "current: %s\nlatest: %s\n", result.Current, result.Latest)
	if result.Available && installed {
		fmt.Fprintln(cmd.OutOrStdout(), "Updated to", result.Latest)
	} else if result.Available {
		fmt.Fprintln(cmd.OutOrStdout(), "Update available; run orbit update")
	} else {
		fmt.Fprintln(cmd.OutOrStdout(), "Already up to date")
	}
	if result.Backup != "" {
		fmt.Fprintln(cmd.OutOrStdout(), "Previous binary retained at:", result.Backup)
	}
}
