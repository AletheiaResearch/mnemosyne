package kimi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type kimiFixture struct {
	root         string
	workspaceDir string
	digest       string
}

func buildKimiFixture(t *testing.T, workspaceDir string, sessions map[string][]map[string]any) kimiFixture {
	t.Helper()

	root := t.TempDir()
	digest := source.HashMD5(filepath.Clean(workspaceDir))

	configPath := filepath.Join(root, "kimi.json")
	config := map[string]any{
		"work_dirs": []map[string]any{
			{"path": workspaceDir},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	projectDir := filepath.Join(root, "sessions", digest)
	for sessionID, lines := range sessions {
		sessionDir := filepath.Join(projectDir, sessionID)
		if err := os.MkdirAll(sessionDir, 0o755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(filepath.Join(sessionDir, "context.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range lines {
			data, err := json.Marshal(line)
			if err != nil {
				file.Close()
				t.Fatal(err)
			}
			if _, err := file.Write(append(data, '\n')); err != nil {
				file.Close()
				t.Fatal(err)
			}
		}
		file.Close()
	}

	return kimiFixture{root: root, workspaceDir: workspaceDir, digest: digest}
}

func TestDiscoverResolvesWorkspaceFromConfig(t *testing.T) {
	t.Parallel()

	fixture := buildKimiFixture(t, "/tmp/kimi-project", map[string][]map[string]any{
		"sess-1": {
			{"role": "user", "content": "hi"},
			{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "hello"}}},
		},
	})

	src := New(fixture.root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected one grouping, got %d: %+v", len(groupings), groupings)
	}
	grouping := groupings[0]
	if grouping.ID != "/tmp/kimi-project" {
		t.Fatalf("expected resolved workspace id, got %q", grouping.ID)
	}
	if grouping.DisplayLabel != "kimi:kimi-project" {
		t.Fatalf("display label = %q", grouping.DisplayLabel)
	}
	if grouping.EstimatedRecords != 1 {
		t.Fatalf("estimated records = %d", grouping.EstimatedRecords)
	}
}

func TestDiscoverFallsBackToDigestWhenConfigMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digest := "abcdef0123456789"
	projectDir := filepath.Join(root, "sessions", digest, "sess-1")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "context.jsonl"),
		[]byte(`{"role":"user","content":"hi"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	src := New(root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected one grouping, got %d: %+v", len(groupings), groupings)
	}
	if groupings[0].ID != digest {
		t.Fatalf("expected digest fallback id, got %q", groupings[0].ID)
	}
	if groupings[0].DisplayLabel != "kimi:abcdef01" {
		t.Fatalf("expected truncated digest label, got %q", groupings[0].DisplayLabel)
	}
}

func TestExtractParsesAssistantBlocksAndToolCalls(t *testing.T) {
	t.Parallel()

	fixture := buildKimiFixture(t, "/tmp/kimi-project", map[string][]map[string]any{
		"sess-1": {
			{"role": "user", "content": "hi"},
			{
				"role": "assistant",
				"content": []any{
					map[string]any{"type": "think", "text": "let me think"},
					map[string]any{"type": "text", "text": "done"},
				},
				"tool_calls": []any{
					map[string]any{
						"function": map[string]any{
							"name":      "shell",
							"arguments": `{"cmd":"ls"}`,
						},
					},
				},
			},
			{"role": "_usage", "token_count": float64(150)},
		},
	})

	src := New(fixture.root)

	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/kimi-project",
		DisplayLabel: "kimi:kimi-project",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	record := records[0]
	if record.RecordID != "sess-1" {
		t.Fatalf("record id = %q", record.RecordID)
	}
	if record.Model != "kimi-k2" {
		t.Fatalf("model = %q", record.Model)
	}
	if record.WorkingDir != "/tmp/kimi-project" {
		t.Fatalf("working_dir = %q", record.WorkingDir)
	}
	if record.Usage.OutputTokens != 150 {
		t.Fatalf("expected output tokens from _usage, got %d", record.Usage.OutputTokens)
	}
	if len(record.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d: %+v", len(record.Turns), record.Turns)
	}
	assistant := record.Turns[1]
	if assistant.Text != "done" {
		t.Fatalf("assistant text = %q", assistant.Text)
	}
	if assistant.Reasoning != "let me think" {
		t.Fatalf("assistant reasoning = %q", assistant.Reasoning)
	}
	if len(assistant.ToolCalls) != 1 || assistant.ToolCalls[0].Tool != "shell" {
		t.Fatalf("tool calls = %+v", assistant.ToolCalls)
	}
	if record.Usage.ToolCalls != 1 {
		t.Fatalf("usage tool calls = %d", record.Usage.ToolCalls)
	}
}

func TestExtractSkipsSessionsInOtherWorkspaces(t *testing.T) {
	t.Parallel()

	// Two workspaces resolved via config; ensure extraction only emits the matching grouping.
	root := t.TempDir()

	config := map[string]any{
		"work_dirs": []map[string]any{
			{"path": "/tmp/a"},
			{"path": "/tmp/b"},
		},
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kimi.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	writeSession := func(workspace, sessionID string) {
		digest := source.HashMD5(filepath.Clean(workspace))
		dir := filepath.Join(root, "sessions", digest, sessionID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		line := map[string]any{"role": "user", "content": "hi from " + workspace}
		raw, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "context.jsonl"), append(raw, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeSession("/tmp/a", "sess-a")
	writeSession("/tmp/b", "sess-b")

	src := New(root)

	var records []schema.Record
	err = src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/a",
		DisplayLabel: "kimi:a",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RecordID != "sess-a" {
		t.Fatalf("expected only sess-a, got %+v", records)
	}
}

func TestLookupSessionReturnsFoundSession(t *testing.T) {
	t.Parallel()

	fixture := buildKimiFixture(t, "/tmp/kimi-project", map[string][]map[string]any{
		"target": {
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "hi"}}},
		},
	})

	src := New(fixture.root)
	record, found, err := src.LookupSession(t.Context(), "target")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected to find session")
	}
	if record.RecordID != "target" {
		t.Fatalf("record id = %q", record.RecordID)
	}
	if record.WorkingDir != "/tmp/kimi-project" {
		t.Fatalf("working dir = %q", record.WorkingDir)
	}

	_, found, err = src.LookupSession(t.Context(), "missing")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected no lookup for missing id")
	}
}

func TestDiscoverReturnsNilWhenRootMissing(t *testing.T) {
	t.Parallel()
	src := New(filepath.Join(t.TempDir(), "does-not-exist"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings, got %+v", groupings)
	}
}
