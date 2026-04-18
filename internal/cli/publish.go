package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/attest"
	"github.com/AletheiaResearch/mnemosyne/internal/card"
	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/publish"
	"github.com/AletheiaResearch/mnemosyne/internal/redact"
	"github.com/AletheiaResearch/mnemosyne/internal/version"
)

func newPublishCommand(rt *runtime) *cobra.Command {
	var repoID string
	var publishAttestation string
	var isolate bool

	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an attested export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}
			if !cmd.Flags().Changed("isolate") && cfg.IsolateExport {
				isolate = true
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

			// Validate all local preconditions before any remote side
			// effect. EnsureDatasetRepo creates the dataset repo, so a
			// failure afterwards would leave an empty repo behind.
			if isolate {
				if err := validateIsolatePreflight(cfg); err != nil {
					return err
				}
			}

			if err := publish.EnsureDatasetRepo(repoID); err != nil {
				return err
			}

			if isolate {
				return runIsolatePublish(cmd, rt, cfg, repoID, publishAttestation)
			}

			return runCanonicalPublish(cmd, rt, cfg, repoID, publishAttestation)
		},
	}
	cmd.Flags().StringVar(&repoID, "repo", "", "dataset repository identifier")
	cmd.Flags().StringVar(&publishAttestation, "publish-attestation", "", "attestation text approving publication")
	cmd.Flags().BoolVar(&isolate, "isolate", false, "upload per-session native files produced by `extract --isolate`")
	return cmd
}

func runCanonicalPublish(cmd *cobra.Command, rt *runtime, cfg config.Config, repoID, publishAttestation string) error {
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
}

// validateIsolatePreflight checks every local precondition for
// `publish --isolate` before any remote side effect. It verifies that a
// prior `extract --isolate` ran and that each staging file still hashes
// to the manifest's redacted_hash, so the command can fail fast without
// creating the dataset repo on the hub.
func validateIsolatePreflight(cfg config.Config) error {
	if cfg.LastExtract == nil || len(cfg.LastExtract.IsolateSessions) == 0 {
		return errors.New("publish --isolate requires extract --isolate first")
	}
	for _, session := range cfg.LastExtract.IsolateSessions {
		if _, err := os.Stat(session.StagingPath); err != nil {
			return fmt.Errorf("staging file %s: %w", session.StagingPath, err)
		}
		diskHash, err := hashStagingFile(session.StagingPath)
		if err != nil {
			return fmt.Errorf("hash staging file %s: %w", session.StagingPath, err)
		}
		if diskHash != session.RedactedHash {
			return fmt.Errorf("staging file %s changed after extract (hash %s, manifest %s); re-run extract --isolate", session.StagingPath, diskHash, session.RedactedHash)
		}
	}
	return nil
}

func runIsolatePublish(cmd *cobra.Command, rt *runtime, cfg config.Config, repoID, publishAttestation string) error {
	localEntries := make([]card.ManifestEntry, 0, len(cfg.LastExtract.IsolateSessions))
	for _, session := range cfg.LastExtract.IsolateSessions {
		localEntries = append(localEntries, card.ManifestEntry{
			File:         session.File,
			Format:       session.Format,
			SourceHash:   session.SourceHash,
			RedactionKey: session.RedactionKey,
			RedactedHash: session.RedactedHash,
			Lines:        session.Lines,
		})
	}

	tempDir, err := os.MkdirTemp("", "mnemosyne-publish-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	remoteEntries, err := fetchRemoteManifest(repoID, tempDir)
	if err != nil {
		return err
	}

	toUpload, _, alignedEntries := card.DiffManifestSessions(localEntries, remoteEntries)
	mergedEntries := card.MergeManifestEntries(alignedEntries, remoteEntries)

	commitMessage := "Publish Mnemosyne isolate export " + time.Now().UTC().Format(time.RFC3339)
	// Aligned entries may carry a File adopted from the remote manifest
	// (when the bytes are unchanged but the source moved on disk), so
	// key the staging-path lookup by SourceHash — the one field Diff
	// never rewrites — to pair uploads with the correct local bytes.
	uploadBySourceHash := make(map[string]config.IsolateSession, len(cfg.LastExtract.IsolateSessions))
	for _, session := range cfg.LastExtract.IsolateSessions {
		uploadBySourceHash[session.SourceHash] = session
	}
	for _, entry := range toUpload {
		session, ok := uploadBySourceHash[entry.SourceHash]
		if !ok {
			return fmt.Errorf("isolate session missing staging path for %s", entry.File)
		}
		if err := publish.UploadFile(repoID, session.StagingPath, entry.File, commitMessage); err != nil {
			return err
		}
	}

	header := card.ManifestHeader{
		Tool:                "mnemosyne/" + version.Version,
		ExportedAt:          time.Now().UTC().Format(time.RFC3339),
		PipelineFingerprint: redact.PipelineFingerprint(),
		ConfigFingerprint:   redact.ConfigFingerprint(cfg),
		RecordCount:         cfg.LastExtract.RecordCount,
		AttachImages:        cfg.AttachImages,
	}
	if cfg.LastAttest != nil && cfg.VerificationRecord != nil {
		header.Attestation = &card.ManifestAttestion{
			Timestamp:         cfg.LastAttest.Timestamp,
			FullNameScanned:   !cfg.VerificationRecord.NameScanSkipped,
			FullNameMatches:   cfg.VerificationRecord.FullNameMatchCount,
			ManualSampleCount: cfg.VerificationRecord.ManualSampleCount,
		}
	}
	manifestBytes, err := card.RenderManifestMnemosyne(header, mergedEntries)
	if err != nil {
		return err
	}
	manifestPath := filepath.Join(tempDir, card.ManifestFileName)
	if err := os.WriteFile(manifestPath, manifestBytes, 0o644); err != nil {
		return err
	}
	if err := publish.UploadFile(repoID, manifestPath, card.ManifestFileName, commitMessage); err != nil {
		return err
	}

	readme := card.RenderIsolateDescription(header, mergedEntries, "")
	readmePath := filepath.Join(tempDir, "README.md")
	if err := os.WriteFile(readmePath, []byte(readme), 0o644); err != nil {
		return err
	}
	if err := publish.UploadFile(repoID, readmePath, "README.md", commitMessage); err != nil {
		return err
	}

	cfg.DestinationRepo = repoID
	cfg.PublicationAttestation = publishAttestation
	cfg.IsolateExport = true
	cfg.PhaseMarker = config.PhaseFinalized
	if err := saveConfig(rt.configPath, cfg); err != nil {
		return err
	}

	return printJSON(cmd.OutOrStdout(), map[string]any{
		"repo_id":           repoID,
		"url":               publish.DatasetURL(repoID),
		"sessions_uploaded": len(toUpload),
		"sessions_total":    len(mergedEntries),
	})
}

// hashStagingFile returns the sha256:<hex> digest of a staging file's current
// contents so publish can refuse to upload bytes the manifest no longer
// describes.
func hashStagingFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(sum.Sum(nil)), nil
}

// fetchRemoteManifest retrieves the existing manifest.mnemosyne from the remote
// repo (if any) and returns its session entries. Missing-manifest is treated
// as an empty list so first-time publishes work.
func fetchRemoteManifest(repoID, tempDir string) ([]card.ManifestEntry, error) {
	remoteManifestPath := filepath.Join(tempDir, "remote-"+card.ManifestFileName)
	found, err := publish.DownloadFile(repoID, card.ManifestFileName, remoteManifestPath)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	file, err := os.Open(remoteManifestPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	_, entries, err := card.ParseManifestMnemosyne(file)
	if err != nil {
		return nil, err
	}
	return entries, nil
}
