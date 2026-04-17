package cli

import (
	"github.com/spf13/cobra"

	"github.com/Quantumlyy/mnemosyne/internal/source"
)

func newSurveyCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "survey",
		Short: "Inspect saved state and discover available groupings",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			detected := make([]source.Grouping, 0)
			for _, src := range source.Registry() {
				groupings, err := src.Discover(cmd.Context())
				if err != nil {
					rt.logger.Warn("discovery failed", "source", src.Name(), "error", err)
					continue
				}
				detected = append(detected, groupings...)
			}

			cfg.RefreshPhase(false)
			if err := saveConfig(rt.configPath, cfg); err != nil {
				rt.logger.Warn("save settings", "error", err)
			}

			return printJSON(cmd.OutOrStdout(), map[string]any{
				"phase":               cfg.PhaseMarker,
				"destination_repo":    cfg.DestinationRepo,
				"origin_scope":        cfg.OriginScope,
				"grouping_exclusions": cfg.ExcludedGroupings,
				"custom_redactions":   cfg.Masked().CustomRedactions,
				"custom_handles":      cfg.CustomHandles,
				"scope_confirmed":     cfg.ScopeConfirmed,
				"groupings":           detected,
			})
		},
	}
}
