package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func TestValidateCommandAcceptsLargeLines(t *testing.T) {
	t.Parallel()

	rec := schema.Record{
		RecordID: "rec-big",
		Origin:   "test",
		Grouping: "demo",
		Model:    "model",
		Turns: []schema.Turn{
			{Role: "user", Text: strings.Repeat("x", 9*1024*1024)},
		},
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	data = append(data, '\n')

	path := filepath.Join(t.TempDir(), "big.jsonl")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write input: %v", err)
	}

	cmd := newValidateCommand(&runtime{stdout: new(bytes.Buffer), stderr: new(bytes.Buffer)})
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetErr(new(bytes.Buffer))
	cmd.SetArgs([]string{"--input", path})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("validate rejected a 9 MiB record: %v", err)
	}
}
