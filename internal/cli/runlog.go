package cli

import "github.com/spf13/cobra"

func newRunlogCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "runlog",
		Short: "Show saved workflow state",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"phase":                   cfg.PhaseMarker,
				"last_extract":            cfg.LastExtract,
				"reviewer_statements":     cfg.ReviewerStatements,
				"verification_record":     cfg.VerificationRecord,
				"last_attest":             cfg.LastAttest,
				"publication_attestation": cfg.PublicationAttestation,
			})
		},
	}
}
