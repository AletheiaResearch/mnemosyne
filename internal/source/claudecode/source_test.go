package claudecode

import "testing"

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
