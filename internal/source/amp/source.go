// Package amp ingests Amp (ampcode.com) thread traces.
//
// Amp does not persist full thread bodies on disk: the CLI only keeps
// credentials, a prompt history, and a session pointer under
// ~/.local/share/amp; complete traces live on the Amp Server (ampcode.com)
// and are downloaded on demand. This source therefore reads trace JSON
// files the user has exported (one trace per file, named like
// T-<uuid>.json) from a directory they control. The default root is
// $XDG_CONFIG_HOME/mnemosyne/amp on Linux/macOS — populate it with
// downloaded traces (e.g. via the web UI or a future tooling integration)
// before running `mnemosyne survey`.
//
// The trace schema is the wire format served by ampcode.com: a single
// JSON object with `id`, `title`, `created` (epoch ms), `meta`, `env`,
// and an ordered `messages` array. Each message carries a role and a
// content list of text/thinking blocks plus optional `usage` for the
// assistant turn. We map one trace to one schema.Record.
package amp

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

// defaultGrouping is the bucket assigned to traces that live directly
// under the source root rather than a subdirectory. Mirrors the
// "default" tier other sources use when no project context is known.
const defaultGrouping = "default"

type Source struct {
	root string
}

// New constructs an Amp source rooted at root. When root is empty it
// defaults to $XDG_CONFIG_HOME/mnemosyne/amp (or the platform
// equivalent), keeping consistency with the `supplied` source.
func New(root string) *Source {
	if root == "" {
		if cfgDir, err := config.Dir(); err == nil {
			root = filepath.Join(cfgDir, "amp")
		}
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "amp"
}

// Discover groups trace files by their immediate parent directory
// relative to the root: files in the root land in "default", files in
// any subdirectory land under that subdirectory's name. We deliberately
// skip parsing each trace here — we only count files and stat them so
// the survey is cheap on large export dumps.
func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	if s.root == "" || !source.DirExists(s.root) {
		return nil, nil
	}
	files, err := source.CollectFiles(s.root, isAmpTrace)
	if err != nil {
		return nil, err
	}

	type aggregate struct {
		records int
		bytes   int64
	}
	grouped := make(map[string]*aggregate)
	for _, path := range files {
		bucket := s.bucketFor(path)
		if grouped[bucket] == nil {
			grouped[bucket] = &aggregate{}
		}
		grouped[bucket].records++
		if info, err := os.Stat(path); err == nil {
			grouped[bucket].bytes += info.Size()
		}
	}

	groupings := make([]source.Grouping, 0, len(grouped))
	for id, agg := range grouped {
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "amp:" + id,
			Origin:           s.Name(),
			EstimatedRecords: agg.records,
			EstimatedBytes:   agg.bytes,
		})
	}
	return groupings, nil
}

// Extract walks the same files Discover saw, parsing each trace and
// emitting at most one record per file. Parse failures and traces with
// no usable turns are reported as warnings and skipped so a single
// malformed download does not abort the whole grouping.
func (s *Source) Extract(_ context.Context, grouping source.Grouping, extractCtx source.ExtractionContext, emit func(schema.Record) error) error {
	if s.root == "" || !source.DirExists(s.root) {
		return nil
	}
	files, err := source.CollectFiles(s.root, isAmpTrace)
	if err != nil {
		return err
	}
	for _, path := range files {
		if s.bucketFor(path) != grouping.ID {
			continue
		}
		record, ok, err := s.parseTrace(path, extractCtx)
		if err != nil {
			source.ReportWarning(extractCtx, "amp skipped %s: %v", path, err)
			continue
		}
		if !ok {
			continue
		}
		record.Grouping = grouping.DisplayLabel
		if err := emit(record); err != nil {
			return err
		}
	}
	return nil
}

// LookupSession resolves a trace by its Amp thread ID. The orchestrator
// calls this once per session, so a naive "parse every file every time"
// scan is O(sessions × traces). We exploit the fact that exported Amp
// traces are conventionally named after their thread ID (e.g.
// "T-<uuid>.json"): we scan filenames first and only parse files whose
// basename matches, falling back to a full content scan when nothing
// does so renamed or repackaged exports still resolve.
func (s *Source) LookupSession(_ context.Context, sessionID string) (schema.Record, bool, error) {
	if s.root == "" || !source.DirExists(s.root) {
		return schema.Record{}, false, nil
	}
	files, err := source.CollectFiles(s.root, isAmpTrace)
	if err != nil {
		return schema.Record{}, false, err
	}
	target := sessionID + ".json"
	for _, path := range files {
		if filepath.Base(path) != target {
			continue
		}
		record, ok, err := s.parseTrace(path, source.ExtractionContext{})
		if err != nil || !ok {
			continue
		}
		if record.RecordID == sessionID {
			return record, true, nil
		}
	}
	for _, path := range files {
		if filepath.Base(path) == target {
			continue // already parsed above
		}
		record, ok, err := s.parseTrace(path, source.ExtractionContext{})
		if err != nil || !ok {
			continue
		}
		if record.RecordID == sessionID {
			return record, true, nil
		}
	}
	return schema.Record{}, false, nil
}

func (s *Source) bucketFor(path string) string {
	rel, err := filepath.Rel(s.root, filepath.Dir(path))
	if err != nil || rel == "." || rel == "" {
		return defaultGrouping
	}
	// Use only the top-level segment so deeply nested layouts collapse
	// to a stable bucket name.
	parts := strings.Split(filepath.ToSlash(rel), "/")
	return parts[0]
}

// isAmpTrace matches the file naming convention used by Amp thread
// downloads (T-<uuid>.json). We accept the bare `.json` extension too
// so users who rename or repackage exports are not locked out.
func isAmpTrace(path string, _ os.DirEntry) bool {
	return filepath.Ext(path) == ".json"
}

func (s *Source) parseTrace(path string, extractCtx source.ExtractionContext) (schema.Record, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return schema.Record{}, false, err
	}
	trace, err := source.DecodeJSONObject(raw)
	if err != nil {
		return schema.Record{}, false, fmt.Errorf("decode: %w", err)
	}
	id := source.ExtractString(trace, "id")
	if strings.TrimSpace(id) == "" {
		return schema.Record{}, false, nil
	}
	messages := source.ExtractSlice(trace, "messages")
	if len(messages) == 0 {
		return schema.Record{}, false, nil
	}

	record := schema.Record{
		RecordID:   id,
		Origin:     s.Name(),
		Title:      source.ExtractString(trace, "title"),
		StartedAt:  source.NormalizeTimestamp(trace["created"]),
		EndedAt:    source.NormalizeTimestamp(trace["updatedAt"]),
		WorkingDir: workingDirFromTrace(trace),
		Turns:      make([]schema.Turn, 0, len(messages)),
	}
	if meta := source.ExtractMap(trace, "meta"); meta != nil {
		if last := source.NormalizeTimestamp(meta["lastUserMessageAt"]); last != "" {
			record.EndedAt = source.LatestTimestamp(record.EndedAt, last)
		}
	}

	for _, item := range messages {
		message, ok := item.(map[string]any)
		if !ok {
			continue
		}
		turn, valid := buildTurn(message, extractCtx.SuppressReasoning)
		if valid {
			record.StartedAt = source.EarliestTimestamp(record.StartedAt, turn.Timestamp)
			record.EndedAt = source.LatestTimestamp(record.EndedAt, turn.Timestamp)
			record.Turns = append(record.Turns, turn)
		}
		if usage := source.ExtractMap(message, "usage"); usage != nil {
			if record.Model == "" {
				record.Model = source.ExtractString(usage, "model")
			}
			record.Usage.InputTokens += source.IntNumber(usage["inputTokens"]) +
				source.IntNumber(usage["cacheReadInputTokens"]) +
				source.IntNumber(usage["cacheCreationInputTokens"])
			record.Usage.OutputTokens += source.IntNumber(usage["outputTokens"])
		}
	}
	if len(record.Turns) == 0 {
		return schema.Record{}, false, nil
	}
	if record.Model == "" {
		record.Model = "amp/unknown"
	}
	record.Usage.UserTurns = source.CountTurns(record.Turns, "user")
	record.Usage.AssistantTurns = source.CountTurns(record.Turns, "assistant")
	record.Usage.ToolCalls = source.CountToolCalls(record.Turns)

	if ext := traceExtensions(trace); len(ext) > 0 {
		record.Extensions = map[string]any{"amp": ext}
	}
	record.Provenance = &schema.Provenance{
		SourcePath:   path,
		SourceID:     id,
		SourceOrigin: s.Name(),
	}
	return record, true, nil
}

// buildTurn collapses a single Amp message into the canonical turn
// shape. Amp tags content blocks as either "text" (user-visible
// assistant or user prose) or "thinking" (reasoning); we keep them in
// turn order so prose comes out concatenated and reasoning is exposed on
// the Reasoning field, dropped entirely when SuppressReasoning is set.
func buildTurn(message map[string]any, suppressReasoning bool) (schema.Turn, bool) {
	role := source.ExtractString(message, "role")
	if role != "user" && role != "assistant" {
		return schema.Turn{}, false
	}

	turn := schema.Turn{
		Role:      role,
		Timestamp: messageTimestamp(message),
	}
	for _, item := range source.ExtractSlice(message, "content") {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch source.ExtractString(block, "type") {
		case "text":
			turn.Text += source.ExtractString(block, "text")
		case "thinking":
			if suppressReasoning {
				continue
			}
			thought := source.ExtractString(block, "thinking")
			if thought == "" {
				thought = source.ExtractString(block, "text")
			}
			if thought == "" {
				continue
			}
			if turn.Reasoning != "" {
				turn.Reasoning += "\n"
			}
			turn.Reasoning += thought
		}
	}
	if turn.Text == "" && turn.Reasoning == "" {
		return schema.Turn{}, false
	}
	return turn, true
}

// messageTimestamp picks the most authoritative timestamp Amp records
// for a message: meta.sentAt for user messages, the first content
// block's startTime for assistant messages, falling back to usage.
// timestamp when present.
func messageTimestamp(message map[string]any) string {
	if meta := source.ExtractMap(message, "meta"); meta != nil {
		if ts := source.NormalizeTimestamp(meta["sentAt"]); ts != "" {
			return ts
		}
	}
	for _, item := range source.ExtractSlice(message, "content") {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if ts := source.NormalizeTimestamp(block["startTime"]); ts != "" {
			return ts
		}
	}
	if usage := source.ExtractMap(message, "usage"); usage != nil {
		if ts := source.NormalizeTimestamp(usage["timestamp"]); ts != "" {
			return ts
		}
	}
	return ""
}

// workingDirFromTrace pulls the first workspace tree URI out of the
// initial environment block and converts it to a filesystem path. Amp
// stores roots as file:// URIs (sometimes with a displayName); we keep
// only the first, matching the convention other sources use of
// surfacing a single working directory.
func workingDirFromTrace(trace map[string]any) string {
	env := source.ExtractMap(trace, "env")
	initial := source.ExtractMap(env, "initial")
	for _, item := range source.ExtractSlice(initial, "trees") {
		tree, ok := item.(map[string]any)
		if !ok {
			continue
		}
		uri := source.ExtractString(tree, "uri")
		if uri == "" {
			continue
		}
		if parsed, err := url.Parse(uri); err == nil && parsed.Path != "" {
			return parsed.Path
		}
		return uri
	}
	return ""
}

// traceExtensions captures the small handful of Amp-specific fields
// that are useful for downstream filtering or attribution but do not
// fit anywhere on the canonical Record. We pass them through verbatim
// rather than inventing typed surrogates so future Amp additions
// (visibility tiers, agent modes, ...) carry over without code changes.
//
// Identity-bearing fields (creatorUserID, installationID, deviceFingerprint)
// are deliberately *not* surfaced: mnemosyne's whole point is producing an
// anonymised dataset, and stable Amp user/device IDs would defeat that
// even before redact.Pipeline runs. Operators who want them back can
// post-process the source trace files directly.
func traceExtensions(trace map[string]any) map[string]any {
	out := make(map[string]any)
	if v := source.ExtractString(trace, "agentMode"); v != "" {
		out["agent_mode"] = v
	}
	if meta := source.ExtractMap(trace, "meta"); meta != nil {
		if v := source.ExtractString(meta, "workspaceID"); v != "" {
			out["workspace_id"] = v
		}
		if v := source.ExtractString(meta, "projectID"); v != "" {
			out["project_id"] = v
		}
		if v := source.ExtractString(meta, "visibility"); v != "" {
			out["visibility"] = v
		}
	}
	if env := source.ExtractMap(trace, "env"); env != nil {
		if initial := source.ExtractMap(env, "initial"); initial != nil {
			if platform := source.ExtractMap(initial, "platform"); platform != nil {
				if v := source.ExtractString(platform, "clientVersion"); v != "" {
					out["client_version"] = v
				}
				if v := source.ExtractString(platform, "clientType"); v != "" {
					out["client_type"] = v
				}
			}
		}
	}
	return out
}
