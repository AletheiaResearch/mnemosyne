package cli

import (
	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func newConfigureCommand(rt *runtime) *cobra.Command {
	var repo string
	var scope string
	var excludes []string
	var redactions []string
	var handles []string
	var confirmScope bool

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Read or modify persistent settings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			changed := repo != "" || scope != "" || len(excludes) > 0 || len(redactions) > 0 || len(handles) > 0 || confirmScope
			if !changed {
				return printJSON(cmd.OutOrStdout(), cfg.Masked())
			}

			if repo != "" {
				cfg.DestinationRepo = repo
			}
			if scope != "" {
				cfg.OriginScope = scope
			}
			cfg.MergeStringSlice(&cfg.ExcludedGroupings, excludes)
			cfg.MergeStringSlice(&cfg.CustomRedactions, redactions)
			cfg.MergeStringSlice(&cfg.CustomHandles, handles)
			cfg.ScopeConfirmed = cfg.ScopeConfirmed || confirmScope || len(excludes) > 0
			cfg.RefreshPhase(false)

			if err := saveConfig(rt.configPath, cfg); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), cfg.Masked())
		},
	}

	cmd.Flags().StringVar(&repo, "destination-repo", "", "dataset repository identifier")
	cmd.Flags().StringVar(&scope, "scope", "", "origin scope to extract")
	cmd.Flags().StringSliceVar(&excludes, "exclude", nil, "grouping label to exclude")
	cmd.Flags().StringSliceVar(&redactions, "redact", nil, "literal string to redact")
	cmd.Flags().StringSliceVar(&handles, "handle", nil, "handle to anonymize")
	cmd.Flags().BoolVar(&confirmScope, "confirm-scope", false, "mark grouping selection as confirmed")
	cmd.ValidArgsFunction = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{
			string(config.PhaseInitial),
			string(config.PhasePreparing),
			string(config.PhasePendingReview),
			string(config.PhaseCleared),
			string(config.PhaseFinalized),
		}, cobra.ShellCompDirectiveNoFileComp
	}
	return cmd
}
