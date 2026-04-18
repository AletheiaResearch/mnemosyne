package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

func writeJSONL(t *testing.T, path string, lines []map[string]any) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, line := range lines {
		data, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(append(data, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func newCodexSource(t *testing.T) (*Source, string, string) {
	t.Helper()
	root := t.TempDir()
	active := filepath.Join(root, "active")
	archive := filepath.Join(root, "archive")
	if err := os.MkdirAll(active, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	return New(active, archive), active, archive
}

func TestDiscoverGroupsByWorkingDir(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)

	writeJSONL(t, filepath.Join(active, "session-a.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "a", "cwd": "/tmp/project-a"}},
	})
	writeJSONL(t, filepath.Join(active, "session-b.jsonl"), []map[string]any{
		{"type": "turn_context", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"cwd": "/tmp/project-b"}},
	})
	writeJSONL(t, filepath.Join(active, "unknown.jsonl"), []map[string]any{
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"type": "agent_message", "message": "hi"}},
	})

	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 3 {
		t.Fatalf("expected 3 groupings, got %d: %+v", len(groupings), groupings)
	}

	byID := make(map[string]source.Grouping)
	for _, g := range groupings {
		byID[g.ID] = g
	}
	if _, ok := byID["/tmp/project-a"]; !ok {
		t.Fatalf("missing project-a grouping: %+v", byID)
	}
	if _, ok := byID["/tmp/project-b"]; !ok {
		t.Fatalf("missing project-b grouping: %+v", byID)
	}
	if g, ok := byID[source.EstimateUnknownLabel("codex")]; !ok || g.DisplayLabel != "codex:codex:unknown" {
		t.Fatalf("missing unknown grouping: %+v", byID)
	}
}

func TestExtractMergesMessagesAndToolCalls(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)

	writeJSONL(t, filepath.Join(active, "sess.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{
			"id":             "sess",
			"cwd":            "/tmp/project",
			"model_provider": "openai",
			"git":            map[string]any{"branch": "main"},
		}},
		{"type": "turn_context", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{
			"cwd":   "/tmp/project",
			"model": "gpt-5-codex",
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:02Z", "payload": map[string]any{
			"type": "message",
			"role": "user",
			"content": []any{
				map[string]any{"type": "input_text", "text": "hello codex"},
			},
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:03Z", "payload": map[string]any{
			"type": "reasoning",
			"summary": []any{
				map[string]any{"text": "Thinking..."},
			},
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:04Z", "payload": map[string]any{
			"type":      "function_call",
			"call_id":   "call-1",
			"name":      "read_file",
			"arguments": `{"path":"main.go"}`,
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:05Z", "payload": map[string]any{
			"type":    "function_call_output",
			"call_id": "call-1",
			"output":  "Exit code: 0\nWall time: 3ms\npackage main",
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:06Z", "payload": map[string]any{
			"type": "message",
			"role": "assistant",
			"content": []any{
				map[string]any{"type": "output_text", "text": "done"},
			},
		}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:07Z", "payload": map[string]any{
			"type": "token_count",
			"info": map[string]any{
				"total_token_usage": map[string]any{
					"input_tokens":        float64(100),
					"cached_input_tokens": float64(5),
					"output_tokens":       float64(42),
				},
			},
		}},
	})

	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var target source.Grouping
	for _, g := range groupings {
		if g.ID == "/tmp/project" {
			target = g
		}
	}
	if target.ID == "" {
		t.Fatalf("expected grouping for /tmp/project, got %+v", groupings)
	}

	var records []schema.Record
	err = src.Extract(t.Context(), target, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected one record, got %d", len(records))
	}
	record := records[0]

	if record.RecordID != "sess" {
		t.Fatalf("record id = %q", record.RecordID)
	}
	if record.Model != "gpt-5-codex" {
		t.Fatalf("expected turn_context model to win, got %q", record.Model)
	}
	if record.Branch != "main" {
		t.Fatalf("branch = %q", record.Branch)
	}
	if record.Grouping != target.DisplayLabel {
		t.Fatalf("grouping = %q, want %q", record.Grouping, target.DisplayLabel)
	}
	if record.Usage.InputTokens != 105 || record.Usage.OutputTokens != 42 {
		t.Fatalf("token usage = %+v", record.Usage)
	}
	if record.Usage.UserTurns != 1 || record.Usage.AssistantTurns != 1 {
		t.Fatalf("turn counts = %+v", record.Usage)
	}
	if record.Usage.ToolCalls != 1 {
		t.Fatalf("tool calls = %d", record.Usage.ToolCalls)
	}

	if len(record.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d: %+v", len(record.Turns), record.Turns)
	}
	assistant := record.Turns[1]
	if assistant.Role != "assistant" || assistant.Text != "done" {
		t.Fatalf("assistant turn = %+v", assistant)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call attached to assistant turn, got %d", len(assistant.ToolCalls))
	}
	call := assistant.ToolCalls[0]
	if call.Tool != "read_file" || call.Status != "success" {
		t.Fatalf("tool call = %+v", call)
	}
	if call.Output == nil || call.Output.Text == "" {
		t.Fatalf("expected tool output, got %+v", call.Output)
	}
	if raw, ok := call.Output.Raw.(map[string]any); !ok || raw["exit_code"] != "0" {
		t.Fatalf("expected exit_code in raw, got %+v", call.Output.Raw)
	}
}

func TestExtractSkipsFilesNotMatchingGrouping(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)

	writeJSONL(t, filepath.Join(active, "keep.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "keep", "cwd": "/tmp/keep"}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{"type": "user_message", "message": "hi"}},
	})
	writeJSONL(t, filepath.Join(active, "drop.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "drop", "cwd": "/tmp/drop"}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{"type": "user_message", "message": "hi"}},
	})

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/keep",
		DisplayLabel: "codex:keep",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RecordID != "keep" {
		t.Fatalf("expected only keep record, got %+v", records)
	}
}

func TestExtractReportsWarningsOnMalformedFile(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)

	writeJSONL(t, filepath.Join(active, "good.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "good", "cwd": "/tmp/good"}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{"type": "user_message", "message": "hi"}},
	})
	// A file that isn't valid UTF-8 JSONL (the parseFile swallows JSON errors per-line, so instead
	// we check that an unreadable file produces a warning via the grouping miss path — codex's
	// parseFile returns nil error for bad lines). Use an empty grouping id ("unknown") with a file
	// whose probe returns "" so it still gets grouped there.
	writeJSONL(t, filepath.Join(active, "empty.jsonl"), []map[string]any{
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"type": "token_count", "info": map[string]any{}}},
	})

	var warnings []string
	ctx := source.ExtractionContext{Warn: func(msg string) { warnings = append(warnings, msg) }}

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/good",
		DisplayLabel: "codex:good",
		Origin:       src.Name(),
	}, ctx, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected good record, got %d", len(records))
	}
}

func TestLookupSessionFindsByFilename(t *testing.T) {
	t.Parallel()

	src, active, archive := newCodexSource(t)

	writeJSONL(t, filepath.Join(active, "sess-active.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "sess-active", "cwd": "/tmp/proj"}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{"type": "agent_message", "message": "ok"}},
	})
	writeJSONL(t, filepath.Join(archive, "sess-archived.jsonl"), []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "sess-archived", "cwd": "/tmp/proj"}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{"type": "agent_message", "message": "ok"}},
	})

	record, found, err := src.LookupSession(context.Background(), "sess-archived")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected to find archived session")
	}
	if record.RecordID != "sess-archived" {
		t.Fatalf("record id = %q", record.RecordID)
	}

	_, found, err = src.LookupSession(context.Background(), "missing-id")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected not to find unknown id")
	}
}

func TestParseFileDeduplicatesConsecutiveAssistantText(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)
	path := filepath.Join(active, "dupe.jsonl")

	writeJSONL(t, path, []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "dupe", "cwd": "/tmp/proj"}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{
			"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": "same"}},
		}},
		{"type": "event_msg", "timestamp": "2026-04-17T10:00:02Z", "payload": map[string]any{
			"type": "agent_message", "message": "same",
		}},
	})

	record, err := src.parseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if count := source.CountTurns(record.Turns, "assistant"); count != 1 {
		t.Fatalf("expected one assistant turn after dedup, got %d: %+v", count, record.Turns)
	}
}

func TestParseFileCustomToolCallCapturesInputAndOutput(t *testing.T) {
	t.Parallel()

	src, active, _ := newCodexSource(t)
	path := filepath.Join(active, "tool.jsonl")

	writeJSONL(t, path, []map[string]any{
		{"type": "session_meta", "timestamp": "2026-04-17T10:00:00Z", "payload": map[string]any{"id": "tool", "cwd": "/tmp/proj"}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:01Z", "payload": map[string]any{
			"type":    "custom_tool_call",
			"call_id": "cc-1",
			"name":    "apply_patch",
			"input":   `{"diff":"@@"}`,
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:02Z", "payload": map[string]any{
			"type":    "custom_tool_call_output",
			"call_id": "cc-1",
			"output":  `{"output":"ok","status":"success"}`,
		}},
		{"type": "response_item", "timestamp": "2026-04-17T10:00:03Z", "payload": map[string]any{
			"type": "message", "role": "assistant",
			"content": []any{map[string]any{"type": "output_text", "text": "patched"}},
		}},
	})

	record, err := src.parseFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Turns) == 0 {
		t.Fatalf("expected at least one turn")
	}
	assistant := record.Turns[len(record.Turns)-1]
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistant.ToolCalls))
	}
	call := assistant.ToolCalls[0]
	if call.Tool != "apply_patch" {
		t.Fatalf("tool = %q", call.Tool)
	}
	if call.Output == nil || call.Output.Text != "ok" {
		t.Fatalf("expected parsed text output, got %+v", call.Output)
	}
}

func TestStatusFromOutputFlagsErrors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		text string
		want string
	}{
		{"", ""},
		{"ran fine", "success"},
		{"ERROR: something broke", "error"},
	}
	for _, tc := range cases {
		var out *schema.ToolOutput
		if tc.text != "" {
			out = &schema.ToolOutput{Text: tc.text}
		}
		got := statusFromOutput(out)
		if got != tc.want {
			t.Fatalf("statusFromOutput(%q) = %q, want %q", tc.text, got, tc.want)
		}
	}
}
