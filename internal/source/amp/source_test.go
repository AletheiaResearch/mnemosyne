package amp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

// stageFixture copies the canonical Amp trace fixture into rel under
// root and returns its absolute path. Sub-paths must use "/" so callers
// can be platform-agnostic.
func stageFixture(t *testing.T, root, rel string) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "T-019e01be-1ae4-7119-b94f-a1a75696ce01.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dst := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(dst, src, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return dst
}

func TestDiscoverGroupsBySubdirAndDefault(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stageFixture(t, root, "T-001.json")
	stageFixture(t, root, "personal/T-002.json")
	stageFixture(t, root, "work/T-003.json")
	stageFixture(t, root, "work/nested/T-004.json")

	src := New(root)
	groupings, err := src.Discover(t.Context())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	byID := make(map[string]source.Grouping, len(groupings))
	for _, g := range groupings {
		byID[g.ID] = g
		if g.Origin != src.Name() {
			t.Fatalf("origin = %q, want %q", g.Origin, src.Name())
		}
		if !strings.HasPrefix(g.DisplayLabel, "amp:") {
			t.Fatalf("display label %q missing amp: prefix", g.DisplayLabel)
		}
	}

	if got := byID[defaultGrouping].EstimatedRecords; got != 1 {
		t.Fatalf("default bucket records = %d, want 1", got)
	}
	if got := byID["personal"].EstimatedRecords; got != 1 {
		t.Fatalf("personal bucket records = %d, want 1", got)
	}
	if got := byID["work"].EstimatedRecords; got != 2 {
		t.Fatalf("work bucket records = %d, want 2 (root file + nested file)", got)
	}
	for id, g := range byID {
		if g.EstimatedBytes <= 0 {
			t.Fatalf("bucket %q EstimatedBytes = %d, want >0", id, g.EstimatedBytes)
		}
	}
}

func TestDiscoverIgnoresMissingRoot(t *testing.T) {
	t.Parallel()

	groupings, err := New(filepath.Join(t.TempDir(), "missing")).Discover(t.Context())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if groupings != nil {
		t.Fatalf("expected nil groupings, got %+v", groupings)
	}
}

func TestExtractParsesCanonicalTrace(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := stageFixture(t, root, "default/T-019e01be-1ae4-7119-b94f-a1a75696ce01.json")

	var records []schema.Record
	src := New(root)
	if err := src.Extract(t.Context(), source.Grouping{
		ID:           "default",
		DisplayLabel: "amp:default",
		Origin:       src.Name(),
	}, source.ExtractionContext{}, func(record schema.Record) error {
		records = append(records, record)
		return nil
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	rec := records[0]

	if rec.RecordID != "T-019e01be-1ae4-7119-b94f-a1a75696ce01" {
		t.Fatalf("record_id = %q", rec.RecordID)
	}
	if rec.Origin != "amp" || rec.Grouping != "amp:default" {
		t.Fatalf("origin/grouping = %q/%q", rec.Origin, rec.Grouping)
	}
	if rec.Title != "Using Codex subscription access" {
		t.Fatalf("title = %q", rec.Title)
	}
	if rec.Model != "claude-opus-4-7" {
		t.Fatalf("model = %q", rec.Model)
	}
	if rec.WorkingDir != "/Users/quantumly/Documents/Development" {
		t.Fatalf("working_dir = %q", rec.WorkingDir)
	}
	if len(rec.Turns) != 4 {
		t.Fatalf("turns = %d, want 4", len(rec.Turns))
	}

	if rec.Turns[0].Role != "user" || rec.Turns[0].Text != "How can I let you use my Codex sub?" {
		t.Fatalf("turn[0] = %+v", rec.Turns[0])
	}
	if rec.Turns[0].Timestamp == "" {
		t.Fatalf("turn[0] timestamp empty")
	}
	if rec.Turns[1].Role != "assistant" {
		t.Fatalf("turn[1] role = %q", rec.Turns[1].Role)
	}
	if !strings.Contains(rec.Turns[1].Reasoning, "OpenAI Codex subscription") {
		t.Fatalf("turn[1] reasoning = %q", rec.Turns[1].Reasoning)
	}
	if !strings.Contains(rec.Turns[1].Text, "Amp doesn't currently support") {
		t.Fatalf("turn[1] text = %q", rec.Turns[1].Text)
	}

	if rec.Usage.UserTurns != 2 || rec.Usage.AssistantTurns != 2 {
		t.Fatalf("usage turn counts = %+v", rec.Usage)
	}
	wantInput := 6 + 16368 + 11836 + 6 + 16368 + 12211
	if rec.Usage.InputTokens != wantInput {
		t.Fatalf("input tokens = %d, want %d", rec.Usage.InputTokens, wantInput)
	}
	if rec.Usage.OutputTokens != 363+80 {
		t.Fatalf("output tokens = %d, want %d", rec.Usage.OutputTokens, 363+80)
	}
	if rec.StartedAt == "" || rec.EndedAt == "" {
		t.Fatalf("missing timestamps started=%q ended=%q", rec.StartedAt, rec.EndedAt)
	}

	if rec.Provenance == nil || rec.Provenance.SourcePath != path {
		t.Fatalf("provenance = %+v (want path %q)", rec.Provenance, path)
	}
	ext, ok := rec.Extensions["amp"].(map[string]any)
	if !ok {
		t.Fatalf("extensions[amp] missing or wrong type: %#v", rec.Extensions)
	}
	if ext["agent_mode"] != "smart" {
		t.Fatalf("agent_mode = %v", ext["agent_mode"])
	}
	if ext["visibility"] != "private" {
		t.Fatalf("visibility = %v", ext["visibility"])
	}
	if ext["client_version"] != "0.0.1778143766-gf8b20a" {
		t.Fatalf("client_version = %v", ext["client_version"])
	}

	if err := schema.ValidateRecord(rec); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestExtractSuppressesReasoning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stageFixture(t, root, "default/trace.json")

	var got schema.Record
	src := New(root)
	if err := src.Extract(t.Context(), source.Grouping{
		ID:           "default",
		DisplayLabel: "amp:default",
		Origin:       src.Name(),
	}, source.ExtractionContext{SuppressReasoning: true}, func(record schema.Record) error {
		got = record
		return nil
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	for i, turn := range got.Turns {
		if turn.Reasoning != "" {
			t.Fatalf("turn %d: expected reasoning suppressed, got %q", i, turn.Reasoning)
		}
	}
}

func TestExtractSkipsBadFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	groupDir := filepath.Join(root, "default")
	if err := os.MkdirAll(groupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(groupDir, "no-id.json"), []byte(`{"messages":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	stageFixture(t, root, "default/good.json")

	var warnings []string
	emitted := 0
	src := New(root)
	err := src.Extract(t.Context(), source.Grouping{
		ID:           "default",
		DisplayLabel: "amp:default",
		Origin:       src.Name(),
	}, source.ExtractionContext{
		Warn: func(msg string) { warnings = append(warnings, msg) },
	}, func(schema.Record) error {
		emitted++
		return nil
	})
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if emitted != 1 {
		t.Fatalf("emitted = %d, want 1", emitted)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected warning for broken.json")
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "broken.json") {
		t.Fatalf("warnings missing broken.json: %s", joined)
	}
}

func TestLookupSessionByID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stageFixture(t, root, "default/trace.json")
	src := New(root)

	rec, ok, err := src.LookupSession(t.Context(), "T-019e01be-1ae4-7119-b94f-a1a75696ce01")
	if err != nil || !ok {
		t.Fatalf("lookup ok=%v err=%v", ok, err)
	}
	if rec.RecordID != "T-019e01be-1ae4-7119-b94f-a1a75696ce01" {
		t.Fatalf("record_id = %q", rec.RecordID)
	}

	if _, ok, err := src.LookupSession(t.Context(), "T-missing"); err != nil || ok {
		t.Fatalf("expected miss, got ok=%v err=%v", ok, err)
	}
}

func TestNameAndDefaultRoot(t *testing.T) {
	t.Parallel()

	src := New("")
	if src.Name() != "amp" {
		t.Fatalf("name = %q", src.Name())
	}
	if src.root == "" {
		t.Fatalf("default root empty: New(\"\") should resolve to a config-dir path")
	}
	if !strings.HasSuffix(filepath.ToSlash(src.root), "/mnemosyne/amp") {
		t.Fatalf("default root = %q, expected to end in mnemosyne/amp", src.root)
	}
}
