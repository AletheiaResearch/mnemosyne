package claudecode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func assistantEntry(model, branch, cwd, text string) map[string]any {
	return map[string]any{
		"type":      "assistant",
		"timestamp": "2026-04-17T11:18:20Z",
		"cwd":       cwd,
		"gitBranch": branch,
		"message": map[string]any{
			"model": model,
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		},
	}
}

func userEntry(branch, cwd, text string) map[string]any {
	return map[string]any{
		"type":      "user",
		"timestamp": "2026-04-17T11:18:19Z",
		"cwd":       cwd,
		"gitBranch": branch,
		"message":   map[string]any{"content": text},
	}
}

func TestAssembleRecord_DropsDetachedHEADBranch(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("HEAD", "/tmp/proj", "hi"),
		assistantEntry("claude-sonnet-4-6", "HEAD", "/tmp/proj", "hello"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.Branch != "" {
		t.Fatalf("expected empty branch for detached HEAD, got %q", record.Branch)
	}
}

func TestAssembleRecord_KeepsRealBranch(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("main", "/tmp/proj", "hi"),
		assistantEntry("claude-sonnet-4-6", "main", "/tmp/proj", "hello"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.Branch != "main" {
		t.Fatalf("expected branch 'main', got %q", record.Branch)
	}
}

func TestAssembleRecord_BranchFallsThroughHEADToLaterEntry(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("HEAD", "/tmp/proj", "hi"),
		assistantEntry("claude-sonnet-4-6", "feature/x", "/tmp/proj", "hello"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.Branch != "feature/x" {
		t.Fatalf("expected later entry's branch to be adopted, got %q", record.Branch)
	}
}

func TestAssembleRecord_ExtractsCwdAndModel(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("main", "/Users/x/work/repo", "hi"),
		assistantEntry("claude-sonnet-4-6", "main", "/Users/x/work/repo", "hello"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.WorkingDir != "/Users/x/work/repo" {
		t.Fatalf("working_dir = %q", record.WorkingDir)
	}
	if record.Model != "claude-sonnet-4-6" {
		t.Fatalf("model = %q", record.Model)
	}
}

func TestAssembleRecord_ModelFallbackWhenAssistantMissing(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("main", "/tmp/proj", "hi"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.Model != "claudecode/unknown" {
		t.Fatalf("expected unknown model fallback, got %q", record.Model)
	}
	if record.Usage.AssistantTurns != 0 {
		t.Fatalf("expected 0 assistant turns, got %d", record.Usage.AssistantTurns)
	}
}

func TestAssembleRecord_CountsTurns(t *testing.T) {
	t.Parallel()
	entries := []map[string]any{
		userEntry("main", "/tmp/proj", "first"),
		assistantEntry("claude-sonnet-4-6", "main", "/tmp/proj", "reply"),
		userEntry("main", "/tmp/proj", "second"),
		assistantEntry("claude-sonnet-4-6", "main", "/tmp/proj", "reply two"),
	}
	record := assembleClaudeRecord(entries, "sess")
	if record.Usage.UserTurns != 2 || record.Usage.AssistantTurns != 2 {
		t.Fatalf("usage = %+v", record.Usage)
	}
}

func TestParseSession_PopulatesProvenance(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "session-abc.jsonl")
	content := `{"type":"user","timestamp":"2026-04-17T11:18:19Z","cwd":"/tmp/proj","gitBranch":"main","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-04-17T11:18:20Z","cwd":"/tmp/proj","gitBranch":"main","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	src := &Source{root: dir}
	record, err := src.parseSession("projA", path)
	if err != nil {
		t.Fatalf("parseSession: %v", err)
	}
	if record.Provenance == nil {
		t.Fatalf("expected provenance to be populated")
	}
	if record.Provenance.SourcePath != path {
		t.Fatalf("source_path = %q, want %q", record.Provenance.SourcePath, path)
	}
	wantScoped := ProjectScope("projA") + "/session-abc"
	if record.Provenance.SourceID != wantScoped {
		t.Fatalf("source_id = %q, want %q", record.Provenance.SourceID, wantScoped)
	}
	if record.Provenance.SourceOrigin != "claudecode" {
		t.Fatalf("source_origin = %q, want claudecode", record.Provenance.SourceOrigin)
	}
	if record.RecordID != wantScoped {
		t.Fatalf("record_id = %q, want %q (project-scoped)", record.RecordID, wantScoped)
	}
	if strings.Contains(record.RecordID, "projA") {
		t.Fatalf("raw project dir leaked into record_id: %q", record.RecordID)
	}
}

// Claude Code project directory names are path-derived (e.g.
// "-Users-nejc-client-repo"). ProjectScope must produce a stable opaque
// token so neither canonical JSONL nor manifest.mnemosyne leaks those
// substrings, while still disambiguating two different projects.
func TestProjectScope_IsOpaqueAndStable(t *testing.T) {
	t.Parallel()
	sensitive := "-Users-nejc-client-repo"
	scope := ProjectScope(sensitive)
	if scope == "" {
		t.Fatal("ProjectScope returned empty for non-empty input")
	}
	if strings.Contains(scope, "Users") || strings.Contains(scope, "nejc") || strings.Contains(scope, "client") || strings.Contains(scope, "repo") {
		t.Fatalf("ProjectScope leaks path components: %q", scope)
	}
	if ProjectScope(sensitive) != scope {
		t.Fatal("ProjectScope is not deterministic")
	}
	if ProjectScope("other-project") == scope {
		t.Fatal("ProjectScope collides across distinct projects")
	}
	if ProjectScope("") != "" {
		t.Fatal("ProjectScope(\"\") should stay empty")
	}
}

// Two projects sharing a session filename must produce distinct record IDs so
// the global seenRecordIDs dedup does not drop the second session.
func TestParseSession_RecordIDIsProjectScoped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	projA := filepath.Join(dir, "projA")
	projB := filepath.Join(dir, "projB")
	if err := os.MkdirAll(projA, 0o755); err != nil {
		t.Fatalf("mkdir A: %v", err)
	}
	if err := os.MkdirAll(projB, 0o755); err != nil {
		t.Fatalf("mkdir B: %v", err)
	}
	content := `{"type":"user","timestamp":"2026-04-17T11:18:19Z","cwd":"/tmp","gitBranch":"main","message":{"content":"hi"}}` + "\n" +
		`{"type":"assistant","timestamp":"2026-04-17T11:18:20Z","cwd":"/tmp","gitBranch":"main","message":{"model":"claude-sonnet-4-6","content":[{"type":"text","text":"hello"}]}}` + "\n"
	for _, proj := range []string{projA, projB} {
		if err := os.WriteFile(filepath.Join(proj, "session.jsonl"), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", proj, err)
		}
	}
	src := &Source{root: dir}
	recA, err := src.parseSession("projA", filepath.Join(projA, "session.jsonl"))
	if err != nil {
		t.Fatalf("parseSession A: %v", err)
	}
	recB, err := src.parseSession("projB", filepath.Join(projB, "session.jsonl"))
	if err != nil {
		t.Fatalf("parseSession B: %v", err)
	}
	if recA.RecordID == recB.RecordID {
		t.Fatalf("record ids collide across projects: %q", recA.RecordID)
	}
}

func TestDecodeProjectName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{"-tmp-scratch", "tmp/scratch"},
		{"", ""},
	}
	for _, tc := range cases {
		got := decodeProjectName(tc.in)
		if got != tc.want {
			t.Fatalf("decodeProjectName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
