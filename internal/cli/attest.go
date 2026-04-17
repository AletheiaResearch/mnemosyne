package cli

import (
	"errors"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/attest"
	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func newAttestCommand(rt *runtime) *cobra.Command {
	var filePath string
	var fullName string
	var skipName bool
	var identity string
	var entity string
	var manual string

	cmd := &cobra.Command{
		Use:   "attest",
		Short: "Review an export and record attestation text",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}
			filePath, err = resolveAttestFile(filePath, cfg)
			if err != nil {
				return err
			}
			report, statements, verification, lastAttest, err := attest.BuildRecords(filePath, fullName, skipName, identity, entity, manual)
			if err != nil {
				return err
			}
			cfg.ReviewerStatements = statements
			cfg.VerificationRecord = verification
			cfg.LastAttest = lastAttest
			cfg.PhaseMarker = config.PhaseCleared
			if err := saveConfig(rt.configPath, cfg); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), report)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "path to the export file to attest")
	cmd.Flags().StringVar(&fullName, "full-name", "", "full name to scan for")
	cmd.Flags().BoolVar(&skipName, "skip-name-scan", false, "skip the full-name scan")
	cmd.Flags().StringVar(&identity, "identity-scan", "", "attestation text describing the identity scan")
	cmd.Flags().StringVar(&entity, "entity-scan", "", "attestation text describing the sensitive-entity interview")
	cmd.Flags().StringVar(&manual, "manual-review", "", "attestation text describing the manual review sample")
	return cmd
}

func resolveAttestFile(filePath string, cfg config.Config) (string, error) {
	if filePath != "" {
		return filePath, nil
	}
	if cfg.LastExtract != nil && cfg.LastExtract.OutputPath != "" {
		return cfg.LastExtract.OutputPath, nil
	}
	matches, err := filepath.Glob("mnemosyne-*.jsonl")
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", errors.New("attest requires --file or a prior extract output path")
}
