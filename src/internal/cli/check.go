package cli

import (
	"encoding/json"
	"fmt"

	"github.com/HuangChenning/orbit-cli/src/internal/config"
	"github.com/HuangChenning/orbit-cli/src/internal/engine"
	"github.com/spf13/cobra"
)

func addCheck(root *cobra.Command, load func(bool) (*engine.Engine, []config.Dependency, error)) {
	var o engine.CheckOptions
	var duplicates bool
	check := &cobra.Command{Use: "check", Short: "Check caches/links or compare explicit Axon inventory; --fix repairs known configuration omissions", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		if duplicates && (o.Fix || o.Baseline != "" || o.AxonConfig != "" || o.Vendors != "" || o.Hub != "" || o.Repo != "") {
			return fmt.Errorf("--duplicates cannot be combined with --fix or Axon coverage options")
		}
		e, deps, err := load(o.Baseline != "" || o.AxonConfig != "" || o.Vendors != "")
		if err != nil {
			json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"status": "invalid", "fixed": false, "error": err.Error()})
			return err
		}
		var report map[string]any
		if duplicates {
			report, err = e.CheckDuplicates(deps)
		} else if o.Baseline != "" || o.AxonConfig != "" || o.Vendors != "" {
			report, err = e.CheckCoverage(cmd.Context(), o)
		} else if o.Fix || o.Repo != "" {
			err = fmt.Errorf("--fix and --repo require an explicit Axon baseline; unknown sources cannot be invented")
		} else {
			report, err = e.Check(cmd.Context(), deps)
		}
		if err != nil {
			json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{"status": "invalid", "fixed": false, "error": err.Error()})
			return err
		}
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		if err = enc.Encode(report); err != nil {
			return err
		}
		if report["status"] != "ok" {
			return fmt.Errorf("check found differences")
		}
		return nil
	}}
	f := check.Flags()
	f.BoolVar(&duplicates, "duplicates", false, "report repeated sources and local skill names/content; read-only, respects .orbitignore")
	f.StringVar(&o.Baseline, "baseline", "", "offline Axon inventory directory")
	f.StringVar(&o.AxonConfig, "axon-config", "", "explicit Axon YAML source")
	f.StringVar(&o.Vendors, "vendors", "", "explicit Axon vendors YAML source")
	f.StringVar(&o.Hub, "hub", "", "override Axon Hub path")
	f.StringVar(&o.Repo, "repo", "", "compare mirror file inventory without changing its contents")
	f.BoolVar(&o.Fix, "fix", false, "repair known configuration omissions with backup; no source conflict overwrite")
	root.AddCommand(check)
}
