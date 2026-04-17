package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

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

			for _, selection := range selections {
				extractCtx := source.ExtractionContext{
					Logger:            rt.logger,
					SuppressReasoning: suppressReasoning,
					Warn: func(message string) {
						message = strings.TrimSpace(message)
						if message == "" {
							return
						}
						if _, exists := seenWarnings[message]; exists {
							return
						}
						seenWarnings[message] = struct{}{}
						summary.Warnings = append(summary.Warnings, message)
					},
				}
				err := selection.src.Extract(cmd.Context(), selection.grouping, extractCtx, func(record schema.Record) error {
					if suppressReasoning {
						for idx := range record.Turns {
							record.Turns[idx].Reasoning = ""
						}
					}
					redacted, count := pipeline.ApplyRecord(record)
					if err := schema.ValidateRecord(redacted); err != nil {
						summary.SkippedRecords++
						rt.logger.Debug("skip invalid record", "record_id", record.RecordID, "error", err)
						return nil
					}
					if _, exists := seenRecordIDs[redacted.RecordID]; exists {
						summary.SkippedRecords++
						rt.logger.Debug("skip duplicate record", "record_id", redacted.RecordID, "origin", redacted.Origin)
						return nil
					}
					seenRecordIDs[redacted.RecordID] = struct{}{}
					data, err := json.Marshal(redacted)
					if err != nil {
						return err
					}
					if _, err := writer.Write(append(data, '\n')); err != nil {
						return err
					}
					summary.RecordCount++
					summary.RedactionCount += count
					summary.InputTokens += redacted.Usage.InputTokens
					summary.OutputTokens += redacted.Usage.OutputTokens
					updateBreakdown(summary.PerModel, redacted.Model, redacted)
					updateBreakdown(summary.PerGrouping, redacted.Grouping, redacted)
					return nil
				})
				if err != nil {
					return err
				}
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

	out := make([]sourceSelection, 0)
	for _, src := range selected {
		groupings, err := src.Discover(cmd.Context())
		if err != nil {
			return nil, err
		}
		for _, grouping := range groupings {
			if !includeAll {
				if _, skip := excluded[grouping.DisplayLabel]; skip {
					continue
				}
			}
			out = append(out, sourceSelection{src: src, grouping: grouping})
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
