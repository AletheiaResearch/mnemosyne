package card

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func writeRecords(t *testing.T, path string, records []schema.Record) {
	t.Helper()
	var builder strings.Builder
	for _, record := range records {
		raw, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		builder.Write(raw)
		builder.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSummarizeFileAggregatesAcrossModelsAndGroupings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "export.jsonl")
	writeRecords(t, path, []schema.Record{
		{
			RecordID: "r1", Origin: "codex", Grouping: "proj-a", Model: "gpt-5",
			Usage: schema.Usage{InputTokens: 100, OutputTokens: 30},
		},
		{
			RecordID: "r2", Origin: "codex", Grouping: "proj-a", Model: "gpt-5",
			Usage: schema.Usage{InputTokens: 50, OutputTokens: 15},
		},
		{
			RecordID: "r3", Origin: "claudecode", Grouping: "proj-b", Model: "claude-opus-4-7",
			Usage: schema.Usage{InputTokens: 200, OutputTokens: 80},
		},
	})

	summary, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RecordCount != 3 {
		t.Fatalf("RecordCount = %d", summary.RecordCount)
	}
	if summary.InputTokens != 350 || summary.OutputTokens != 125 {
		t.Fatalf("tokens = %+v", summary)
	}
	gpt := summary.PerModel["gpt-5"]
	if gpt.Records != 2 || gpt.InputTokens != 150 || gpt.OutputTokens != 45 {
		t.Fatalf("gpt-5 breakdown = %+v", gpt)
	}
	claude := summary.PerModel["claude-opus-4-7"]
	if claude.Records != 1 || claude.InputTokens != 200 {
		t.Fatalf("claude breakdown = %+v", claude)
	}
	projA := summary.PerGrouping["proj-a"]
	if projA.Records != 2 || projA.OutputTokens != 45 {
		t.Fatalf("proj-a breakdown = %+v", projA)
	}
	if _, ok := summary.PerGrouping["proj-b"]; !ok {
		t.Fatalf("missing proj-b grouping: %+v", summary.PerGrouping)
	}
}

func TestSummarizeFileCountsRedactionMarkers(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "redacted.jsonl")
	record := schema.Record{
		RecordID: "r1", Origin: "codex", Model: "gpt-5",
		Turns: []schema.Turn{
			{Role: "user", Text: "leaked " + redact.PlaceholderMarker + " and " + redact.PlaceholderMarker},
			{Role: "assistant", Text: "clean output"},
		},
	}
	writeRecords(t, path, []schema.Record{record})

	summary, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RedactionCount != 2 {
		t.Fatalf("RedactionCount = %d", summary.RedactionCount)
	}
}

func TestSummarizeFileSkipsBlankAndMalformedLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "noisy.jsonl")
	valid, err := json.Marshal(schema.Record{RecordID: "r1", Model: "gpt-5"})
	if err != nil {
		t.Fatal(err)
	}
	content := "\n   \n" + string(valid) + "\nnot-json\n" + string(valid) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	summary, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RecordCount != 2 {
		t.Fatalf("expected 2 records, got %d", summary.RecordCount)
	}
}

func TestSummarizeFileReturnsErrorWhenMissing(t *testing.T) {
	t.Parallel()
	_, err := SummarizeFile(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err == nil {
		t.Fatalf("expected error for missing file")
	}
	if !os.IsNotExist(err) {
		t.Fatalf("expected IsNotExist, got %v", err)
	}
}

func TestSummarizeFileEmptyProducesZeroValuedSummary(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "empty.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	summary, err := SummarizeFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if summary.RecordCount != 0 || summary.InputTokens != 0 || summary.OutputTokens != 0 {
		t.Fatalf("expected zeroed summary, got %+v", summary)
	}
	if len(summary.PerModel) != 0 || len(summary.PerGrouping) != 0 {
		t.Fatalf("expected empty maps, got %+v", summary)
	}
}

func TestRenderDescriptionIncludesHeadersTotalsAndLicense(t *testing.T) {
	t.Parallel()
	summary := Summary{
		RecordCount:    4,
		InputTokens:    123,
		OutputTokens:   45,
		RedactionCount: 2,
		PerModel: map[string]Breakdown{
			"gpt-5":           {Records: 3, InputTokens: 100, OutputTokens: 30},
			"claude-opus-4-7": {Records: 1, InputTokens: 23, OutputTokens: 15},
		},
		PerGrouping: map[string]Breakdown{
			"proj-a": {Records: 4, InputTokens: 123, OutputTokens: 45},
		},
	}
	out := RenderDescription(summary, "dataset.jsonl", "cc-by-4.0")
	for _, needle := range []string{
		"# Mnemosyne Dataset",
		"## Summary",
		"Records: 4",
		"Input tokens: 123",
		"Output tokens: 45",
		"Redaction markers: 2",
		"License: cc-by-4.0",
		"## Per Model",
		"## Per Grouping",
		"claude-opus-4-7",
		"gpt-5",
		"proj-a",
		"dataset.jsonl",
		"```python",
	} {
		if !strings.Contains(out, needle) {
			t.Fatalf("description missing %q:\n%s", needle, out)
		}
	}
	// Model table rows are sorted alphabetically by key.
	claudeIdx := strings.Index(out, "claude-opus-4-7")
	gptIdx := strings.Index(out, "gpt-5")
	if claudeIdx == -1 || gptIdx == -1 || claudeIdx > gptIdx {
		t.Fatalf("expected alphabetical model ordering; claude=%d gpt=%d", claudeIdx, gptIdx)
	}
}

func TestRenderDescriptionOmitsLicenseWhenEmpty(t *testing.T) {
	t.Parallel()
	out := RenderDescription(Summary{}, "file.jsonl", "")
	if strings.Contains(out, "License:") {
		t.Fatalf("unexpected license line in %q", out)
	}
}

func TestRenderManifestRoundTripsSummary(t *testing.T) {
	t.Parallel()
	summary := Summary{
		RecordCount:  1,
		InputTokens:  10,
		OutputTokens: 2,
		PerModel:     map[string]Breakdown{"gpt-5": {Records: 1, InputTokens: 10, OutputTokens: 2}},
		PerGrouping:  map[string]Breakdown{"proj-a": {Records: 1, InputTokens: 10, OutputTokens: 2}},
		ExportedAt:   "2026-04-17T10:00:00Z",
	}
	raw, err := RenderManifest(summary)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Summary
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("manifest not valid JSON: %v", err)
	}
	if decoded.RecordCount != 1 || decoded.ExportedAt != "2026-04-17T10:00:00Z" {
		t.Fatalf("round-trip lost fields: %+v", decoded)
	}
	if decoded.PerModel["gpt-5"].Records != 1 {
		t.Fatalf("per-model lost: %+v", decoded.PerModel)
	}
}
