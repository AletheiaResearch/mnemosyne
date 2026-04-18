package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/AletheiaResearch/mnemosyne/internal/nativeexport"
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
	OutputPath     string               `json:"output_path"`
	RecordCount    int                  `json:"record_count"`
	SkippedRecords int                  `json:"skipped_records"`
	RedactionCount int                  `json:"redaction_count"`
	PerModel       map[string]breakdown `json:"per_model"`
	PerGrouping    map[string]breakdown `json:"per_grouping"`
	InputTokens    int                  `json:"input_tokens"`
	OutputTokens   int                  `json:"output_tokens"`
	Warnings       []string             `json:"warnings,omitempty"`
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
	var isolate bool
	var attachImages bool

	cmd := &cobra.Command{
		Use:   "extract",
		Short: "Extract local records into canonical JSONL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}

			if !cmd.Flags().Changed("isolate") && cfg.IsolateExport {
				isolate = true
			}
			if !cmd.Flags().Changed("attach-images") && cfg.AttachImages {
				attachImages = true
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

			pipeline, err := redact.FromConfig(cfg)
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

			var stagingDir string
			var redactionKey string
			var isolateSessions []config.IsolateSession
			var isolateMu sync.Mutex
			if isolate {
				stagingDir = filepath.Join(filepath.Dir(output), "isolate")
				if err := os.MkdirAll(stagingDir, 0o755); err != nil {
					return err
				}
				redactionKey = redact.RedactionKey(cfg, attachImages)
			}

			if err := runExtraction(cmd.Context(), runExtractionArgs{
				rt:                rt,
				selections:        selections,
				pipeline:          pipeline,
				suppressReasoning: suppressReasoning,
				writer:            writer,
				summary:           &summary,
				seenRecordIDs:     seenRecordIDs,
				addWarning:        addWarning,
				isolate:           isolate,
				attachImages:      attachImages,
				stagingDir:        stagingDir,
				redactionKey:      redactionKey,
				isolateSessions:   &isolateSessions,
				isolateMu:         &isolateMu,
				isolateSeenSrc:    make(map[string]struct{}),
				isolateSkipWarned: make(map[string]struct{}),
			}); err != nil {
				return err
			}

			if err := writer.Flush(); err != nil {
				return err
			}

			lastExtract := &config.LastExtract{
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
			if isolate {
				lastExtract.IsolateStagingDir = stagingDir
				sort.SliceStable(isolateSessions, func(i, j int) bool {
					return isolateSessions[i].File < isolateSessions[j].File
				})
				lastExtract.IsolateSessions = isolateSessions
			}
			cfg.LastExtract = lastExtract
			cfg.IsolateExport = isolate
			cfg.AttachImages = attachImages
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
	cmd.Flags().BoolVar(&isolate, "isolate", false, "also emit redacted copies of each native session file into <output>/isolate/<format>/")
	cmd.Flags().BoolVar(&attachImages, "attach-images", false, "keep inline image attachments in isolate-mode output (has no effect without --isolate)")
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
	rt                *runtime
	selections        []sourceSelection
	pipeline          *redact.Pipeline
	suppressReasoning bool
	writer            *bufio.Writer
	summary           *extractSummary
	seenRecordIDs     map[string]struct{}
	addWarning        func(string)

	isolate           bool
	attachImages      bool
	stagingDir        string
	redactionKey      string
	isolateSessions   *[]config.IsolateSession
	isolateMu         *sync.Mutex
	isolateSeenSrc    map[string]struct{}
	isolateSkipWarned map[string]struct{}
}

// recordIsolate runs the native-format redactor for a single record and
// appends the result to the isolate-session list. origin and src must be
// captured before ApplyRecord runs, because the path anonymizer rewrites
// record.Provenance.SourcePath in place (Provenance is a pointer inside
// the Record value). Returns nil when the origin has no native handler
// (caller warns once per origin) or when the same source file has already
// been redacted in this run.
func (a *runExtractionArgs) recordIsolate(ctx context.Context, origin, src string) error {
	if !a.isolate {
		return nil
	}
	redactor, ok := nativeexport.ForOrigin(origin)
	if !ok {
		a.isolateMu.Lock()
		_, warned := a.isolateSkipWarned[origin]
		if !warned {
			a.isolateSkipWarned[origin] = struct{}{}
		}
		a.isolateMu.Unlock()
		if !warned && origin != "" {
			a.addWarning(fmt.Sprintf("isolate mode: skipping %s (native format not supported yet)", origin))
		}
		return nil
	}
	if src == "" {
		a.addWarning(fmt.Sprintf("isolate mode: %s record missing source path; skipping native export", origin))
		return nil
	}
	info, err := os.Stat(src)
	if err != nil {
		a.addWarning(fmt.Sprintf("isolate mode: stat %s: %v; skipping native export", src, err))
		return nil
	}
	if !info.Mode().IsRegular() {
		a.addWarning(fmt.Sprintf("isolate mode: %s is not a regular file; skipping native export", src))
		return nil
	}
	a.isolateMu.Lock()
	if _, seen := a.isolateSeenSrc[src]; seen {
		a.isolateMu.Unlock()
		return nil
	}
	a.isolateSeenSrc[src] = struct{}{}
	a.isolateMu.Unlock()

	relPath := isolateRelPath(src)
	format := redactor.Format()
	dst := filepath.Join(a.stagingDir, format, relPath)
	result, err := redactor.Redact(ctx, src, dst, nativeexport.Options{
		Pipeline:     a.pipeline,
		AttachImages: a.attachImages,
		RedactionKey: a.redactionKey,
	})
	if err != nil {
		a.addWarning(fmt.Sprintf("isolate redaction failed for %s: %v", src, err))
		return nil
	}
	session := config.IsolateSession{
		File:         format + "/" + filepath.ToSlash(relPath),
		Format:       format,
		SessionID:    result.SessionID,
		SourcePath:   src,
		StagingPath:  result.StagingPath,
		SourceHash:   result.SourceHash,
		RedactionKey: result.RedactionKey,
		RedactedHash: result.RedactedHash,
		Lines:        result.Lines,
		AttachImages: result.AttachImages,
	}
	a.isolateMu.Lock()
	*a.isolateSessions = append(*a.isolateSessions, session)
	a.isolateMu.Unlock()
	return nil
}

// isolateRelPath builds a staging-relative path that preserves the
// source's basename (so the HF Agent Trace Viewer still shows a useful
// filename) while disambiguating sources that share a basename across
// different parent directories. A short hash of the parent directory
// becomes a subdirectory, so two projects' "session.jsonl" do not
// overwrite each other's staging file or collide in the manifest.
func isolateRelPath(src string) string {
	sum := sha256.Sum256([]byte(filepath.Dir(src)))
	prefix := hex.EncodeToString(sum[:4])
	return filepath.Join(prefix, filepath.Base(src))
}

type emitted struct {
	record     schema.Record
	data       []byte
	redactions int
	invalid    bool
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
					// Capture the source path before ApplyRecord's path
					// anonymizer rewrites it (Provenance is a pointer, so
					// ApplyRecord mutates the shared struct). Route by
					// Provenance.SourceOrigin rather than record.Origin so
					// multi-file variants (e.g. "claudecode-subagents") fall
					// through to the not-supported warning instead of being
					// handed a directory path.
					isolateOrigin := record.Origin
					isolateSrc := ""
					if record.Provenance != nil {
						if record.Provenance.SourceOrigin != "" {
							isolateOrigin = record.Provenance.SourceOrigin
						}
						isolateSrc = record.Provenance.SourcePath
					}
					redacted, count := args.pipeline.ApplyRecord(record)

					rec := emitted{record: redacted, redactions: count}
					if err := schema.ValidateRecord(redacted); err != nil {
						args.rt.logger.Debug("skip invalid record", "record_id", record.RecordID, "error", err)
						rec = emitted{invalid: true}
					} else {
						data, err := json.Marshal(redacted)
						if err != nil {
							return err
						}
						rec.data = data
						if err := args.recordIsolate(ctx, isolateOrigin, isolateSrc); err != nil {
							return err
						}
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
