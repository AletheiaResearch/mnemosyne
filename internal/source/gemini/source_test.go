package gemini

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

func writeGeminiSession(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverWalksDigestDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digestA := "aaaaaaaabbbbbbbbccccccccdddddddd"
	digestB := "11111111222222223333333344444444"

	writeGeminiSession(t, filepath.Join(root, digestA, "chats", "session-1.json"), map[string]any{
		"sessionId":   "sess-1",
		"startTime":   "2026-04-17T10:00:00Z",
		"lastUpdated": "2026-04-17T10:10:00Z",
		"messages": []any{
			map[string]any{"type": "user", "content": "hi", "timestamp": "2026-04-17T10:00:01Z"},
		},
	})
	writeGeminiSession(t, filepath.Join(root, digestB, "chats", "session-2.json"), map[string]any{
		"sessionId": "sess-2",
		"messages": []any{
			map[string]any{"type": "user", "content": "hi", "timestamp": "2026-04-17T10:00:01Z"},
		},
	})
	// bin should be skipped
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Directory without chats should be skipped
	if err := os.MkdirAll(filepath.Join(root, "empty-digest"), 0o755); err != nil {
		t.Fatal(err)
	}

	src := New(root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 2 {
		t.Fatalf("expected 2 groupings, got %d: %+v", len(groupings), groupings)
	}
	byID := make(map[string]source.Grouping)
	for _, g := range groupings {
		byID[g.ID] = g
	}
	if g, ok := byID[digestA]; !ok {
		t.Fatalf("missing digestA grouping: %+v", byID)
	} else if g.DisplayLabel != "gemini:aaaaaaaa" {
		t.Fatalf("display label = %q", g.DisplayLabel)
	}
	if _, ok := byID[digestB]; !ok {
		t.Fatalf("missing digestB grouping")
	}
}

func TestExtractParsesAssistantToolCallsAndThoughts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digest := "digest-for-tests"

	writeGeminiSession(t, filepath.Join(root, digest, "chats", "session-1.json"), map[string]any{
		"sessionId":   "sess-1",
		"startTime":   "2026-04-17T10:00:00Z",
		"lastUpdated": "2026-04-17T10:10:00Z",
		"messages": []any{
			map[string]any{
				"type":      "user",
				"timestamp": "2026-04-17T10:00:01Z",
				"content":   "hello gemini",
			},
			map[string]any{
				"type":      "gemini",
				"timestamp": "2026-04-17T10:00:02Z",
				"content":   "response",
				"model":     "gemini-2.5-pro",
				"thoughts": []any{
					map[string]any{"description": "thinking deeply"},
				},
				"toolCalls": []any{
					map[string]any{
						"name":   "shell",
						"args":   map[string]any{"cmd": "ls"},
						"output": "README.md",
						"status": "success",
					},
				},
				"tokens": map[string]any{
					"input":  float64(100),
					"cached": float64(10),
					"output": float64(25),
				},
			},
		},
	})

	src := New(root)
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           digest,
		DisplayLabel: "gemini:digest-f",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d: %+v", len(records), records)
	}
	record := records[0]
	if record.Model != "gemini-2.5-pro" {
		t.Fatalf("model = %q", record.Model)
	}
	if record.Usage.InputTokens != 110 || record.Usage.OutputTokens != 25 {
		t.Fatalf("tokens = %+v", record.Usage)
	}
	if record.Usage.UserTurns != 1 || record.Usage.AssistantTurns != 1 {
		t.Fatalf("turn counts = %+v", record.Usage)
	}
	assistant := record.Turns[1]
	if assistant.Reasoning != "thinking deeply" {
		t.Fatalf("reasoning = %q", assistant.Reasoning)
	}
	if len(assistant.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(assistant.ToolCalls))
	}
	call := assistant.ToolCalls[0]
	if call.Tool != "shell" {
		t.Fatalf("tool = %q", call.Tool)
	}
	if call.Status != "success" {
		t.Fatalf("status = %q", call.Status)
	}
	if call.Output == nil || call.Output.Raw == nil {
		t.Fatalf("expected output populated, got %+v", call.Output)
	}
}

func TestExtractParsesUserAttachmentsAndToolEvents(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digest := "attachments-digest"

	writeGeminiSession(t, filepath.Join(root, digest, "chats", "session-1.json"), map[string]any{
		"sessionId": "sess-1",
		"messages": []any{
			map[string]any{
				"type":      "user",
				"timestamp": "2026-04-17T10:00:00Z",
				"content": []any{
					map[string]any{"text": "analyze this"},
					map[string]any{
						"inlineData": map[string]any{
							"mimeType": "image/png",
							"data":     "AAA",
						},
					},
					map[string]any{
						"fileData": map[string]any{
							"mimeType": "application/pdf",
							"fileUri":  "gs://bucket/file.pdf",
						},
					},
					map[string]any{
						"functionCall": map[string]any{"name": "tool-x"},
					},
				},
			},
		},
	})

	src := New(root)
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           digest,
		DisplayLabel: "gemini:attachments",
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
	user := records[0].Turns[0]
	if user.Text != "analyze this" {
		t.Fatalf("user text = %q", user.Text)
	}
	if len(user.Attachments) != 2 {
		t.Fatalf("expected 2 attachments, got %d", len(user.Attachments))
	}
	if user.Attachments[0].Type != "image" || user.Attachments[0].Data != "AAA" {
		t.Fatalf("image attachment = %+v", user.Attachments[0])
	}
	if user.Attachments[1].Type != "document" || user.Attachments[1].URL != "gs://bucket/file.pdf" {
		t.Fatalf("file attachment = %+v", user.Attachments[1])
	}
	if user.Extensions == nil || user.Extensions["tool_events"] == nil {
		t.Fatalf("expected tool_events extension, got %+v", user.Extensions)
	}
}

func TestExtractDeduplicatesDuplicateSessionContent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digest := "dupe-digest"

	// Write two identical session files under the same digest - dedup should collapse to one record.
	payload := map[string]any{
		"sessionId": "sess-same",
		"messages": []any{
			map[string]any{"type": "user", "content": "hi", "timestamp": "2026-04-17T10:00:00Z"},
			map[string]any{"type": "gemini", "content": "hello", "timestamp": "2026-04-17T10:00:01Z"},
		},
	}
	writeGeminiSession(t, filepath.Join(root, digest, "chats", "session-a.json"), payload)
	writeGeminiSession(t, filepath.Join(root, digest, "chats", "session-b.json"), payload)

	src := New(root)
	var records []schema.Record
	err := src.Extract(t.Context(), source.Grouping{
		ID:           digest,
		DisplayLabel: "gemini:dupe",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(r schema.Record) error {
		records = append(records, r)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("expected dedup to one record, got %d", len(records))
	}
}

func TestLookupSessionFindsBySessionID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	digest := "lookup-digest"

	writeGeminiSession(t, filepath.Join(root, digest, "chats", "session-1.json"), map[string]any{
		"sessionId": "target-sess",
		"messages": []any{
			map[string]any{"type": "user", "content": "hello", "timestamp": "2026-04-17T10:00:00Z"},
		},
	})

	src := New(root)
	record, found, err := src.LookupSession(t.Context(), "target-sess")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("expected to find target-sess")
	}
	if record.RecordID != "target-sess" {
		t.Fatalf("record id = %q", record.RecordID)
	}

	_, found, err = src.LookupSession(t.Context(), "missing-id")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatalf("expected not found")
	}
}

func TestAttachmentTypeCategorizesMIME(t *testing.T) {
	t.Parallel()
	cases := []struct{ mime, want string }{
		{"image/png", "image"},
		{"image/jpeg", "image"},
		{"application/pdf", "document"},
		{"", "document"},
	}
	for _, tc := range cases {
		if got := attachmentType(tc.mime); got != tc.want {
			t.Fatalf("attachmentType(%q) = %q, want %q", tc.mime, got, tc.want)
		}
	}
}

func TestDiscoverReturnsNilWhenRootMissing(t *testing.T) {
	t.Parallel()
	src := New(filepath.Join(t.TempDir(), "missing"))
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 0 {
		t.Fatalf("expected no groupings, got %+v", groupings)
	}
}
