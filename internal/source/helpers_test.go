package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

func TestReadJSONLinesHandlesLinesLargerThanEightMiB(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "large.jsonl")
	line := `{"text":"` + strings.Repeat("a", 9*1024*1024) + `"}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	count := 0
	if err := ReadJSONLines(path, func(_ int, raw []byte) error {
		count++
		if len(raw) == 0 {
			t.Fatal("expected non-empty line payload")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one line, got %d", count)
	}
}

func TestReadJSONLinesSkipsBlankLines(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "sparse.jsonl")
	content := "\n" + `{"a":1}` + "\n\n" + `{"b":2}` + "\n   \n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var seen []string
	err := ReadJSONLines(path, func(_ int, raw []byte) error {
		seen = append(seen, string(raw))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != `{"a":1}` || seen[1] != `{"b":2}` {
		t.Fatalf("expected blanks stripped, got %+v", seen)
	}
}

func TestReadJSONLinesPropagatesCallbackError(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "halt.jsonl")
	if err := os.WriteFile(path, []byte("{\"x\":1}\n{\"x\":2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("halt")
	err := ReadJSONLines(path, func(_ int, _ []byte) error {
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
}

func TestNormalizeTimestampAcceptsMultipleFormats(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   any
		want string
	}{
		{"rfc3339", "2026-04-17T10:00:00Z", "2026-04-17T10:00:00Z"},
		{"rfc3339_nano", "2026-04-17T10:00:00.123456789Z", "2026-04-17T10:00:00Z"},
		{"sqlite", "2026-04-17 10:00:00", "2026-04-17T10:00:00Z"},
		{"millis_string", "1650204000000", "2022-04-17T14:00:00Z"},
		{"millis_float", float64(1650204000000), "2022-04-17T14:00:00Z"},
		{"json_number", json.Number("1650204000000"), "2022-04-17T14:00:00Z"},
		{"empty_string", "", ""},
		{"bad_string", "not-a-date", ""},
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeTimestamp(tc.in)
			if got != tc.want {
				t.Fatalf("NormalizeTimestamp(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLatestAndEarliestTimestamp(t *testing.T) {
	t.Parallel()

	values := []string{
		"",
		"bad",
		"2026-04-17T10:00:00Z",
		"2026-04-17T11:00:00Z",
		"2026-04-17T09:00:00Z",
	}
	if got := LatestTimestamp(values...); got != "2026-04-17T11:00:00Z" {
		t.Fatalf("LatestTimestamp = %q", got)
	}
	if got := EarliestTimestamp(values...); got != "2026-04-17T09:00:00Z" {
		t.Fatalf("EarliestTimestamp = %q", got)
	}
	if got := LatestTimestamp(); got != "" {
		t.Fatalf("LatestTimestamp empty = %q", got)
	}
	if got := EarliestTimestamp(); got != "" {
		t.Fatalf("EarliestTimestamp empty = %q", got)
	}
}

func TestDisplayLabelFromPathFiltersCommonPrefixes(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	cases := []struct {
		in, want string
	}{
		{filepath.Join(home, "Documents", "Development", "project"), "Development/project"},
		{filepath.Join(home, "Downloads", "sample"), "sample"},
		{"/tmp/project", "tmp/project"},
	}
	for _, tc := range cases {
		if got := DisplayLabelFromPath(tc.in); got != tc.want {
			t.Fatalf("DisplayLabelFromPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestHashHelpers(t *testing.T) {
	t.Parallel()

	if got := HashSHA256("value"); len(got) != 64 {
		t.Fatalf("expected 64-char sha256, got %d (%q)", len(got), got)
	}
	if got := HashMD5("value"); len(got) != 32 {
		t.Fatalf("expected 32-char md5, got %d (%q)", len(got), got)
	}
	if HashSHA256("a") == HashSHA256("b") {
		t.Fatalf("distinct inputs hashed to the same digest")
	}
}

func TestExpandExpandsTilde(t *testing.T) {
	t.Parallel()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home dir")
	}
	if got := Expand("~/docs"); got != filepath.Join(home, "docs") {
		t.Fatalf("Expand = %q", got)
	}
	if got := Expand("/absolute"); got != "/absolute" {
		t.Fatalf("absolute passthrough = %q", got)
	}
}

func TestExtractStringFallsBackAcrossKeys(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"second": "value",
		"number": json.Number("42"),
	}
	if got := ExtractString(input, "missing", "second"); got != "value" {
		t.Fatalf("ExtractString string fallback = %q", got)
	}
	if got := ExtractString(input, "number"); got != "42" {
		t.Fatalf("ExtractString json.Number = %q", got)
	}
	if got := ExtractString(input, "nonexistent"); got != "" {
		t.Fatalf("ExtractString missing = %q", got)
	}
}

func TestExtractMapAndSliceReturnNilOnMismatch(t *testing.T) {
	t.Parallel()

	input := map[string]any{
		"nested":  map[string]any{"a": 1},
		"list":    []any{1, 2, 3},
		"scalar":  "plain",
		"integer": 5,
	}
	if got := ExtractMap(input, "nested"); got == nil {
		t.Fatalf("expected nested map, got nil")
	}
	if got := ExtractMap(input, "scalar"); got != nil {
		t.Fatalf("expected nil for scalar, got %+v", got)
	}
	if got := ExtractMap(nil, "x"); got != nil {
		t.Fatalf("expected nil for nil input")
	}
	if got := ExtractSlice(input, "list"); len(got) != 3 {
		t.Fatalf("expected 3-element slice, got %+v", got)
	}
	if got := ExtractSlice(input, "integer"); got != nil {
		t.Fatalf("expected nil for non-slice value, got %+v", got)
	}
}

func TestJSONStringRoundTripsJSON(t *testing.T) {
	t.Parallel()

	if got := JSONString(`{"a":1}`); got == nil {
		t.Fatalf("expected parsed map")
	} else if _, ok := got.(map[string]any); !ok {
		t.Fatalf("expected map, got %T", got)
	}
	if got := JSONString("plain text"); got != "plain text" {
		t.Fatalf("expected passthrough for invalid json, got %v", got)
	}
}

func TestCountTurnsAndToolCalls(t *testing.T) {
	t.Parallel()

	turns := []schema.Turn{
		{Role: "user"},
		{Role: "assistant", ToolCalls: []schema.ToolCall{{Tool: "a"}, {Tool: "b"}}},
		{Role: "user"},
		{Role: "assistant", ToolCalls: []schema.ToolCall{{Tool: "c"}}},
	}
	if CountTurns(turns, "user") != 2 {
		t.Fatalf("user count wrong")
	}
	if CountTurns(turns, "assistant") != 2 {
		t.Fatalf("assistant count wrong")
	}
	if CountToolCalls(turns) != 3 {
		t.Fatalf("tool call count wrong")
	}
}

func TestCollectFilesGathersMatchesSortedAndSkipsMissingRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, name := range []string{"b.jsonl", "a.jsonl", "readme.md"} {
		path := filepath.Join(root, name)
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	files, err := CollectFiles(root, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl"
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 jsonl files, got %d: %+v", len(files), files)
	}
	if filepath.Base(files[0]) != "a.jsonl" || filepath.Base(files[1]) != "b.jsonl" {
		t.Fatalf("expected sorted output, got %+v", files)
	}

	empty, err := CollectFiles(filepath.Join(root, "missing"), func(string, os.DirEntry) bool { return true })
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("expected nil for missing root, got %+v", empty)
	}
}

func TestOptionalSwallowsNotExist(t *testing.T) {
	t.Parallel()

	if err := Optional(os.ErrNotExist); err != nil {
		t.Fatalf("expected Optional to swallow ErrNotExist, got %v", err)
	}
	sentinel := errors.New("boom")
	if err := Optional(sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("expected passthrough, got %v", err)
	}
}

func TestWithAndFromCauseRoundTripsError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("root cause")
	ctx := WithCause(context.Background(), sentinel)
	if got := CauseFromContext(ctx); !errors.Is(got, sentinel) {
		t.Fatalf("expected to recover cause, got %v", got)
	}
	if got := CauseFromContext(context.Background()); got != nil {
		t.Fatalf("expected nil for missing cause, got %v", got)
	}
	if got := WithCause(context.Background(), nil); got != context.Background() {
		t.Fatalf("expected passthrough when cause is nil")
	}
}

func TestReportWarningInvokesCallbackAndLogger(t *testing.T) {
	t.Parallel()

	var captured []string
	ctx := ExtractionContext{Warn: func(message string) { captured = append(captured, message) }}
	ReportWarning(ctx, "oops: %d", 42)
	if len(captured) != 1 || captured[0] != "oops: 42" {
		t.Fatalf("warning not captured, got %+v", captured)
	}

	// Without Warn or Logger the call should not panic.
	ReportWarning(ExtractionContext{}, "silent")
}

func TestEstimateUnknownLabel(t *testing.T) {
	t.Parallel()
	if got := EstimateUnknownLabel("codex"); got != "codex:unknown" {
		t.Fatalf("EstimateUnknownLabel = %q", got)
	}
}

func TestOpenSQLiteReturnsErrNotExistForMissingFile(t *testing.T) {
	t.Parallel()

	_, err := OpenSQLite(filepath.Join(t.TempDir(), "missing.db"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected ErrNotExist, got %v", err)
	}
}
