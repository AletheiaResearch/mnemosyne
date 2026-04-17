package cli

import (
	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/publish"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	"github.com/AletheiaResearch/mnemosyne/internal/sources"
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
			for _, src := range sources.Registry() {
				groupings, err := src.Discover(cmd.Context())
				if err != nil {
					rt.logger.Warn("discovery failed", "source", src.Name(), "error", err)
					continue
				}
				detected = append(detected, groupings...)
			}

			identity, _ := publish.DetectIdentity()
			cfg.RefreshPhase(identity.Username != "")
			if err := saveConfig(rt.configPath, cfg); err != nil {
				rt.logger.Warn("save settings", "error", err)
			}

			return printJSON(cmd.OutOrStdout(), map[string]any{
				"phase":               cfg.PhaseMarker,
				"platform_identity":   identity.Username,
				"destination_repo":    cfg.DestinationRepo,
				"origin_scope":        cfg.OriginScope,
				"grouping_exclusions": cfg.ExcludedGroupings,
				"custom_redactions":   cfg.Masked().CustomRedactions,
				"custom_handles":      cfg.CustomHandles,
				"scope_confirmed":     cfg.ScopeConfirmed,
				"next_steps":          nextSteps(cfg.PhaseMarker),
				"groupings":           detected,
			})
		},
	}
}

func nextSteps(phase config.Phase) []string {
	switch phase {
	case config.PhaseInitial:
		return []string{"configure a scope", "run inspect", "run extract when ready"}
	case config.PhasePreparing:
		return []string{"run inspect", "confirm exclusions", "run extract"}
	case config.PhasePendingReview:
		return []string{"run attest on the latest export"}
	case config.PhaseCleared:
		return []string{"run publish with a publication attestation"}
	case config.PhaseFinalized:
		return []string{"run extract again when you want to refresh the dataset"}
	default:
		return []string{"run survey again"}
	}
}
