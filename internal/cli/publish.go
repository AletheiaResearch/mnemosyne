package cli

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/attest"
	"github.com/AletheiaResearch/mnemosyne/internal/card"
	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/publish"
)

func newPublishCommand(rt *runtime) *cobra.Command {
	var repoID string
	var publishAttestation string

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an attested export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}
			if cfg.LastAttest == nil || cfg.ReviewerStatements == nil || cfg.VerificationRecord == nil {
				return errors.New("publish requires a successful attest step")
			}
			if err := attest.ValidateStatements(
				cfg.VerificationRecord.FullName,
				cfg.VerificationRecord.NameScanSkipped,
				cfg.ReviewerStatements.IdentityScan,
				cfg.ReviewerStatements.EntityInterview,
				cfg.ReviewerStatements.ManualReview,
			); err != nil {
				return err
			}
			if err := attest.ValidatePublishAttestation(publishAttestation); err != nil {
				return err
			}
			if _, err := os.Stat(cfg.LastAttest.FilePath); err != nil {
				return errors.New("attested export file no longer exists")
			}
			if changed := attest.DetectFileChange(cfg.LastAttest.FilePath, cfg.LastAttest.FileSize, cfg.LastAttest.Timestamp); changed {
				return errors.New("attested export changed after attestation; re-run attest")
			}

			identity, err := publish.DetectIdentity()
			if err != nil {
				return err
			}
			repoID = firstNonEmpty(repoID, cfg.DestinationRepo)
			if repoID == "" {
				repoID = identity.Username + "/mnemosyne-traces"
			}

			summary, err := card.SummarizeFile(cfg.LastAttest.FilePath)
			if err != nil {
				return err
			}
			summary.SkippedRecords = 0
			if cfg.LastExtract != nil {
				summary.SkippedRecords = cfg.LastExtract.SkippedRecords
			}
			manifest, err := card.RenderManifest(summary)
			if err != nil {
				return err
			}
			description := card.RenderDescription(summary, filepath.Base(cfg.LastAttest.FilePath), "")

			tempDir, err := os.MkdirTemp("", "mnemosyne-publish-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tempDir)

			manifestPath := filepath.Join(tempDir, "manifest.json")
			readmePath := filepath.Join(tempDir, "README.md")
			if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
				return err
			}
			if err := os.WriteFile(readmePath, []byte(description), 0o644); err != nil {
				return err
			}

			if err := publish.EnsureDatasetRepo(repoID); err != nil {
				return err
			}
			commitMessage := "Publish Mnemosyne export " + time.Now().UTC().Format(time.RFC3339)
			if err := publish.UploadFile(repoID, cfg.LastAttest.FilePath, filepath.Base(cfg.LastAttest.FilePath), commitMessage); err != nil {
				return err
			}
			if err := publish.UploadFile(repoID, manifestPath, "manifest.json", commitMessage); err != nil {
				return err
			}
			if err := publish.UploadFile(repoID, readmePath, "README.md", commitMessage); err != nil {
				return err
			}

			cfg.DestinationRepo = repoID
			cfg.PublicationAttestation = publishAttestation
			cfg.PhaseMarker = config.PhaseFinalized
			if err := saveConfig(rt.configPath, cfg); err != nil {
				return err
			}
			return printJSON(cmd.OutOrStdout(), map[string]any{
				"repo_id": repoID,
				"url":     publish.DatasetURL(repoID),
			})
		},
	}
	cmd.Flags().StringVar(&repoID, "repo", "", "dataset repository identifier")
	cmd.Flags().StringVar(&publishAttestation, "publish-attestation", "", "attestation text approving publication")
	return cmd
}
