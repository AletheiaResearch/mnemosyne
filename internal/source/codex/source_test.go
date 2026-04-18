package codex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseFile_PopulatesProvenance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session-xyz.jsonl")
	content := `{"type":"session_meta","timestamp":"2026-04-17T11:18:19Z","payload":{"id":"session-xyz","cwd":"/tmp/proj","git":{"branch":"main"}}}` + "\n" +
		`{"type":"response_item","timestamp":"2026-04-17T11:18:20Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}}` + "\n" +
		`{"type":"response_item","timestamp":"2026-04-17T11:18:21Z","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	src := &Source{activeRoot: dir}
	record, err := src.parseFile(path)
	if err != nil {
		t.Fatalf("parseFile: %v", err)
	}
	if record.Provenance == nil {
		t.Fatalf("expected provenance to be populated")
	}
	if record.Provenance.SourcePath != path {
		t.Fatalf("source_path = %q, want %q", record.Provenance.SourcePath, path)
	}
	if record.Provenance.SourceID != record.RecordID {
		t.Fatalf("source_id = %q, want %q", record.Provenance.SourceID, record.RecordID)
	}
	if record.Provenance.SourceOrigin != "codex" {
		t.Fatalf("source_origin = %q, want codex", record.Provenance.SourceOrigin)
	}
}
