package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/redact"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
	"github.com/AletheiaResearch/mnemosyne/internal/sources"
)

type breakdown struct {
	Records      int `json:"records"`
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type extractSummary struct {
	OutputPath          string               `json:"output_path"`
	RecordCount         int                  `json:"record_count"`
	SkippedRecords      int                  `json:"skipped_records"`
	RedactionCount      int                  `json:"redaction_count"`
	VerifiedSecretCount int                  `json:"verified_secret_count,omitempty"`
	VerifiedSecrets     []string             `json:"verified_secrets,omitempty"`
	PerModel            map[string]breakdown `json:"per_model"`
	PerGrouping         map[string]breakdown `json:"per_grouping"`
	InputTokens         int                  `json:"input_tokens"`
	OutputTokens        int                  `json:"output_tokens"`
	Warnings            []string             `json:"warnings,omitempty"`
}

type sourceSelection struct {
	src      source.Source
	grouping source.Grouping
}

func newExtractCommand(rt *runtime) *cobra.Command {
	var output string
	var scope string
	var includeAll bool
	var suppressReasoning bool
	var verifySecrets bool

	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract local records into canonical JSONL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			scope = strings.TrimSpace(firstNonEmpty(scope, cfg.OriginScope))
			if scope == "" && includeAll {
				scope = "all"
			}
			if scope == "" {
				return errors.New("extract requires --scope or a configured origin scope")
			}
			if !includeAll && !cfg.ScopeConfirmed {
				return errors.New("extract requires confirmed grouping selection or --include-all")
			}

			selectedSources, err := selectedSources(scope)
			if err != nil {
				return err
			}
			selections, err := discoverSelections(cmd, selectedSources, cfg, includeAll)
			if err != nil {
				return err
			}
			if len(selections) == 0 {
				return errors.New("no groupings matched the configured scope")
			}

			if output == "" {
				output, err = defaultOutputPath()
				if err != nil {
					return err
				}
			}
			if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
				return err
			}

			file, err := os.Create(output)
			if err != nil {
				return err
			}
			defer file.Close()

			pipeline, err := redact.FromConfigWithOptions(cfg, redact.Options{VerifySecrets: verifySecrets})
			if err != nil {
				return err
			}

			writer := bufio.NewWriter(file)
			defer writer.Flush()

			summary := extractSummary{
				OutputPath:  output,
				PerModel:    make(map[string]breakdown),
				PerGrouping: make(map[string]breakdown),
				Warnings:    make([]string, 0),
			}
			seenRecordIDs := make(map[string]struct{})
			seenVerifiedFingerprints := make(map[string]struct{})
			seenWarnings := make(map[string]struct{})
			var warnMu sync.Mutex
			addWarning := func(message string) {
				message = strings.TrimSpace(message)
				if message == "" {
					return
				}
				warnMu.Lock()
				defer warnMu.Unlock()
				if _, exists := seenWarnings[message]; exists {
					return
				}
				seenWarnings[message] = struct{}{}
				summary.Warnings = append(summary.Warnings, message)
			}

			if err := runExtraction(cmd.Context(), runExtractionArgs{
				rt:                       rt,
				selections:               selections,
				pipeline:                 pipeline,
				suppressReasoning:        suppressReasoning,
				writer:                   writer,
				summary:                  &summary,
				seenRecordIDs:            seenRecordIDs,
				seenVerifiedFingerprints: seenVerifiedFingerprints,
				addWarning:               addWarning,
			}); err != nil {
				return err
			}

			if err := writer.Flush(); err != nil {
				return err
			}

			cfg.LastExtract = &config.LastExtract{
				Timestamp:      time.Now().UTC().Format(time.RFC3339),
				RecordCount:    summary.RecordCount,
				SkippedRecords: summary.SkippedRecords,
				RedactionCount: summary.RedactionCount,
				InputTokens:    summary.InputTokens,
				OutputTokens:   summary.OutputTokens,
				Scope:          scope,
				OutputPath:     output,
				Warnings:       summary.Warnings,
			}
			cfg.ReviewerStatements = nil
			cfg.VerificationRecord = nil
			cfg.LastAttest = nil
			cfg.PublicationAttestation = ""
			cfg.PhaseMarker = config.PhasePendingReview
			if err := saveConfig(rt.configPath, cfg); err != nil {
				rt.logger.Warn("save settings", "error", err)
			}

			return printJSON(cmd.OutOrStdout(), summary)
		},
	}

	cmd.Flags().StringVar(&output, "output", "", "path for the canonical JSONL export")
	cmd.Flags().StringVar(&scope, "scope", "", "source scope to extract, or 'all' (defaults to 'all' when --include-all is set)")
	cmd.Flags().BoolVar(&includeAll, "include-all", false, "ignore grouping exclusions and default scope to 'all'")
	cmd.Flags().BoolVar(&suppressReasoning, "no-reasoning", false, "omit reasoning traces from assistant turns")
	cmd.Flags().BoolVar(&verifySecrets, "verify-secrets", false, "live-verify matched provider secrets against their issuing API (network access required)")
	return cmd
}

func defaultOutputPath() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "exports", fmt.Sprintf("mnemosyne-%s.jsonl", time.Now().UTC().Format("20060102T150405Z"))), nil
}

func selectedSources(scope string) ([]source.Source, error) {
	all := sources.Registry()
	if scope == "all" {
		sort.SliceStable(all, func(i, j int) bool {
			if sourcePriority(all[i].Name()) != sourcePriority(all[j].Name()) {
				return sourcePriority(all[i].Name()) < sourcePriority(all[j].Name())
			}
			return all[i].Name() < all[j].Name()
		})
		return all, nil
	}
	selected := make([]source.Source, 0, 1)
	for _, src := range all {
		if src.Name() == scope {
			selected = append(selected, src)
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("unknown source scope %q", scope)
	}
	return selected, nil
}

func discoverSelections(cmd *cobra.Command, selected []source.Source, cfg config.Config, includeAll bool) ([]sourceSelection, error) {
	excluded := make(map[string]struct{}, len(cfg.ExcludedGroupings))
	for _, item := range cfg.ExcludedGroupings {
		excluded[item] = struct{}{}
	}

	type discovery struct {
		src       source.Source
		groupings []source.Grouping
		err       error
	}
	results := make([]discovery, len(selected))
	var wg sync.WaitGroup
	for i, src := range selected {
		i, src := i, src
		wg.Add(1)
		go func() {
			defer wg.Done()
			groupings, err := src.Discover(cmd.Context())
			results[i] = discovery{src: src, groupings: groupings, err: err}
		}()
	}
	wg.Wait()

	out := make([]sourceSelection, 0)
	for _, r := range results {
		if r.err != nil {
			return nil, r.err
		}
		for _, grouping := range r.groupings {
			if !includeAll {
				if _, skip := excluded[grouping.DisplayLabel]; skip {
					continue
				}
			}
			out = append(out, sourceSelection{src: r.src, grouping: grouping})
		}
	}
	return out, nil
}

func updateBreakdown(rows map[string]breakdown, key string, record schema.Record) {
	row := rows[key]
	row.Records++
	row.InputTokens += record.Usage.InputTokens
	row.OutputTokens += record.Usage.OutputTokens
	rows[key] = row
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func sourcePriority(name string) int {
	if name == "orchestrator" {
		return 0
	}
	return 1
}

type runExtractionArgs struct {
	rt                       *runtime
	selections               []sourceSelection
	pipeline                 *redact.Pipeline
	suppressReasoning        bool
	writer                   *bufio.Writer
	summary                  *extractSummary
	seenRecordIDs            map[string]struct{}
	seenVerifiedFingerprints map[string]struct{}
	addWarning               func(string)
}

type emitted struct {
	record          schema.Record
	data            []byte
	redactions      int
	verifiedSecrets []redact.VerifiedSecret
	invalid         bool
}

func runExtraction(parent context.Context, args runExtractionArgs) error {
	maxWorkers := extractionConcurrency(len(args.selections))

	progress := newExtractProgress(len(args.selections))
	stopProgress := progress.run(args.rt.stderr)
	defer stopProgress()

	cancelCtx, cancel := context.WithCancel(parent)
	defer cancel()

	// Process selections in priority buckets so higher-priority sources
	// (e.g. the orchestrator, which enriches external sessions) fully
	// populate seenRecordIDs before lower-priority sources can emit a
	// duplicate RecordID and win dedup. See sourcePriority.
	var writeErr error
	for _, bucket := range bucketByPriority(args.selections) {
		if cancelCtx.Err() != nil {
			break
		}
		if err := extractBucket(cancelCtx, cancel, bucket, maxWorkers, args, progress, &writeErr); err != nil {
			return err
		}
		if writeErr != nil {
			break
		}
	}
	return writeErr
}

func extractBucket(
	parent context.Context,
	cancel context.CancelFunc,
	selections []sourceSelection,
	maxWorkers int,
	args runExtractionArgs,
	progress *extractProgress,
	writeErr *error,
) error {
	workers := min(maxWorkers, len(selections))
	out := make(chan emitted, workers*4)

	g, ctx := errgroup.WithContext(parent)
	g.SetLimit(workers)

	go func() {
		for _, selection := range selections {
			selection := selection
			g.Go(func() error {
				extractCtx := source.ExtractionContext{
					Logger:            args.rt.logger,
					SuppressReasoning: args.suppressReasoning,
					Warn:              args.addWarning,
				}
				err := selection.src.Extract(ctx, selection.grouping, extractCtx, func(record schema.Record) error {
					if args.suppressReasoning {
						for idx := range record.Turns {
							record.Turns[idx].Reasoning = ""
						}
					}
					redacted, stats := args.pipeline.ApplyRecord(record)

					rec := emitted{record: redacted, redactions: stats.Redactions, verifiedSecrets: stats.VerifiedSecrets}
					if err := schema.ValidateRecord(redacted); err != nil {
						args.rt.logger.Debug("skip invalid record", "record_id", record.RecordID, "error", err)
						rec = emitted{invalid: true}
					} else {
						data, err := json.Marshal(redacted)
						if err != nil {
							return err
						}
						rec.data = data
					}

					select {
					case out <- rec:
						return nil
					case <-ctx.Done():
						return ctx.Err()
					}
				})
				if err != nil && !errors.Is(err, context.Canceled) {
					return err
				}
				progress.completeSelection()
				return nil
			})
		}
		_ = g.Wait()
		close(out)
	}()

	for rec := range out {
		if rec.invalid {
			args.summary.SkippedRecords++
			continue
		}
		if _, exists := args.seenRecordIDs[rec.record.RecordID]; exists {
			args.summary.SkippedRecords++
			args.rt.logger.Debug("skip duplicate record", "record_id", rec.record.RecordID, "origin", rec.record.Origin)
			continue
		}
		args.seenRecordIDs[rec.record.RecordID] = struct{}{}
		if _, err := args.writer.Write(append(rec.data, '\n')); err != nil {
			if *writeErr == nil {
				*writeErr = err
			}
			cancel()
			continue
		}
		args.summary.RecordCount++
		args.summary.RedactionCount += rec.redactions
		for _, vs := range rec.verifiedSecrets {
			if _, dup := args.seenVerifiedFingerprints[vs.Fingerprint]; dup {
				continue
			}
			args.seenVerifiedFingerprints[vs.Fingerprint] = struct{}{}
			args.summary.VerifiedSecretCount++
			if len(args.summary.VerifiedSecrets) < 20 {
				args.summary.VerifiedSecrets = append(args.summary.VerifiedSecrets, vs.Label)
			}
		}
		args.summary.InputTokens += rec.record.Usage.InputTokens
		args.summary.OutputTokens += rec.record.Usage.OutputTokens
		updateBreakdown(args.summary.PerModel, rec.record.Model, rec.record)
		updateBreakdown(args.summary.PerGrouping, rec.record.Grouping, rec.record)
		progress.addRecord(rec.redactions)
	}

	return g.Wait()
}

func bucketByPriority(selections []sourceSelection) [][]sourceSelection {
	if len(selections) == 0 {
		return nil
	}
	var buckets [][]sourceSelection
	start := 0
	currentPriority := sourcePriority(selections[0].src.Name())
	for i := 1; i < len(selections); i++ {
		p := sourcePriority(selections[i].src.Name())
		if p != currentPriority {
			buckets = append(buckets, selections[start:i])
			start = i
			currentPriority = p
		}
	}
	buckets = append(buckets, selections[start:])
	return buckets
}

func extractionConcurrency(selections int) int {
	n := min(max(goruntime.NumCPU(), 2), 8)
	if selections > 0 {
		n = min(n, selections)
	}
	return n
}
