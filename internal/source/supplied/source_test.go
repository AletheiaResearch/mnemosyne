package supplied

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

func TestDiscoverAndExtract(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	groupDir := filepath.Join(root, "demo")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatal(err)
	}

	record := schema.Record{
		RecordID: "rec-1",
		Model:    "test-model",
		Turns: []schema.Turn{
			{Role: "user", Text: "hello"},
			{Role: "assistant", Text: "world"},
		},
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "records.jsonl"), append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	src := New(root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(groupings) != 1 {
		t.Fatalf("expected one grouping, got %d", len(groupings))
	}

	count := 0
	err = src.Extract(t.Context(), groupings[0], source.ExtractionContext{}, func(record schema.Record) error {
		count++
		if record.Origin != "supplied" {
			t.Fatalf("unexpected origin %q", record.Origin)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one extracted record, got %d", count)
	}
}
