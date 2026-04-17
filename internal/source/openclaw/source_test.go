package openclaw

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

func TestExtractKeepsUnknownBucketSeparate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	sessionDir := filepath.Join(root, "demo", "sessions")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}

	writeSession := func(name string, header map[string]any) {
		t.Helper()

		lines := []map[string]any{
			header,
			{
				"type":      "message",
				"timestamp": "2026-04-17T10:00:01Z",
				"message": map[string]any{
					"role": "user",
					"content": []map[string]any{{
						"type": "text",
						"text": "hello",
					}},
				},
			},
		}
		file, err := os.Create(filepath.Join(sessionDir, name+".jsonl"))
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

	writeSession("known", map[string]any{
		"type":      "session",
		"id":        "known",
		"cwd":       "/tmp/project",
		"timestamp": "2026-04-17T10:00:00Z",
	})
	writeSession("unknown", map[string]any{
		"type":      "session",
		"id":        "unknown",
		"timestamp": "2026-04-17T10:01:00Z",
	})

	src := New(root)
	var known, unknown []schema.Record

	if err := src.Extract(t.Context(), source.Grouping{
		ID:           "/tmp/project",
		DisplayLabel: "openclaw:project",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		known = append(known, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := src.Extract(t.Context(), source.Grouping{
		ID:           source.EstimateUnknownLabel(src.Name()),
		DisplayLabel: "openclaw:unknown",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		unknown = append(unknown, record)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if len(known) != 1 || known[0].RecordID != "known" {
		t.Fatalf("expected known grouping to emit only known session, got %+v", known)
	}
	if len(unknown) != 1 || unknown[0].RecordID != "unknown" {
		t.Fatalf("expected unknown grouping to emit only unknown session, got %+v", unknown)
	}
}
