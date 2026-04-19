package nativeexport

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
	"github.com/AletheiaResearch/mnemosyne/internal/source/claudecode"
)

const plantedImageB64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO6dD4oAAAAASUVORK5CYII="

// plantedSecrets lists detector-scope secrets planted in the fixtures; these
// must not survive redaction. Paths like /Users/nejc/... are intentionally
// excluded — path anonymization is HOME-relative and runtime-dependent; those
// strings are only rewritten when the runtime HOME matches their prefix.
var plantedSecrets = []string{
	"alice@contoso.dev",
	"sk-abcdefghijklmnopqrstuvwxyz0123",
	"ghp_abcdefghijklmnopqrstuvwxyz0123AB",
}

func newPipeline(t *testing.T) *redact.Pipeline {
	t.Helper()
	p, err := redact.New(redact.Options{})
	if err != nil {
		t.Fatalf("redact.New: %v", err)
	}
	return p
}

func fileHash(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join("testdata", name)
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	dir := t.TempDir()
	dst := filepath.Join(dir, name)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture copy: %v", err)
	}
	return dst
}

func readJSONL(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	out := make([]map[string]any, 0)
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("unmarshal %q: %v", line, err)
		}
		out = append(out, obj)
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func assertNoSecrets(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	for _, secret := range plantedSecrets {
		if strings.Contains(text, secret) {
			t.Errorf("output still contains secret %q", secret)
		}
	}
	if !strings.Contains(text, redact.PlaceholderMarker) {
		t.Errorf("output missing placeholder marker %q", redact.PlaceholderMarker)
	}
}

type fixtureCase struct {
	name     string
	fixture  string
	redactor Redactor
}

func fixtureCases() []fixtureCase {
	return []fixtureCase{
		{"claudecode", "claudecode-session.jsonl", ClaudeCode()},
		{"codex", "codex-session.jsonl", Codex()},
	}
}

func TestRedact_SourceByteIdentical(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			before := fileHash(t, src)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: true,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			if after := fileHash(t, src); after != before {
				t.Fatalf("source file mutated: before=%s after=%s", before, after)
			}
		})
	}
}

func TestRedact_SchemaKeyPreservation(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: true,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			srcLines := readJSONL(t, src)
			dstLines := readJSONL(t, dst)
			if len(srcLines) != len(dstLines) {
				t.Fatalf("line count: src=%d dst=%d", len(srcLines), len(dstLines))
			}
			for i := range srcLines {
				if !reflect.DeepEqual(sortedKeys(srcLines[i]), sortedKeys(dstLines[i])) {
					t.Errorf("line %d key set changed: src=%v dst=%v",
						i, sortedKeys(srcLines[i]), sortedKeys(dstLines[i]))
				}
			}
		})
	}
}

func TestRedact_KeepImages(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: true,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("read dst: %v", err)
			}
			if !strings.Contains(string(data), plantedImageB64) {
				t.Errorf("AttachImages=true should preserve base64 payload")
			}
		})
	}
}

func TestRedact_StripImages(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: false,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("read dst: %v", err)
			}
			text := string(data)
			if strings.Contains(text, plantedImageB64) {
				t.Errorf("AttachImages=false should strip base64 payload")
			}
			if strings.Contains(text, `"type":"image"`) || strings.Contains(text, `"type":"input_image"`) {
				t.Errorf("image-typed blocks should be absent")
			}
		})
	}
}

func TestRedact_Secrets(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: true,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			assertNoSecrets(t, dst)
		})
	}
}

func TestRedact_HashStability(t *testing.T) {
	t.Parallel()
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := copyFixture(t, tc.fixture)
			dir := t.TempDir()
			opts := Options{Pipeline: newPipeline(t), AttachImages: false, RedactionKey: "v1:test"}
			first, err := tc.redactor.Redact(context.Background(), src, filepath.Join(dir, "a.jsonl"), opts)
			if err != nil {
				t.Fatalf("first redact: %v", err)
			}
			second, err := tc.redactor.Redact(context.Background(), src, filepath.Join(dir, "b.jsonl"), opts)
			if err != nil {
				t.Fatalf("second redact: %v", err)
			}
			if first.RedactedHash != second.RedactedHash {
				t.Fatalf("redacted hash unstable: %s vs %s", first.RedactedHash, second.RedactedHash)
			}
			if first.SourceHash != second.SourceHash {
				t.Fatalf("source hash differs: %s vs %s", first.SourceHash, second.SourceHash)
			}
			if first.Format != tc.redactor.Format() {
				t.Errorf("format %q want %q", first.Format, tc.redactor.Format())
			}
			if first.RedactionKey != "v1:test" {
				t.Errorf("redaction key = %q", first.RedactionKey)
			}
			if first.Lines <= 0 {
				t.Errorf("expected positive line count, got %d", first.Lines)
			}
		})
	}
}

func TestRedact_HomePathAnonymized(t *testing.T) {
	// Path anonymization only fires when HOME matches the planted prefix, so
	// stub HOME to the fixture's user before constructing the pipeline.
	t.Setenv("HOME", "/Users/nejc")
	for _, tc := range fixtureCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			src := copyFixture(t, tc.fixture)
			dst := filepath.Join(t.TempDir(), "out.jsonl")
			_, err := tc.redactor.Redact(context.Background(), src, dst, Options{
				Pipeline:     newPipeline(t),
				AttachImages: true,
			})
			if err != nil {
				t.Fatalf("redact: %v", err)
			}
			data, err := os.ReadFile(dst)
			if err != nil {
				t.Fatalf("read dst: %v", err)
			}
			if strings.Contains(string(data), "/Users/nejc/") {
				t.Errorf("home-relative path should be anonymized")
			}
		})
	}
}

// The Claude Code session id is "<ProjectScope>/<UUID>" — a hash of the
// project directory, not the directory name itself, so the manifest
// never leaks path-derived project folders like "-Users-nejc-client".
// This matches what claudecode.sessionRecordID emits for the canonical
// record so DiffManifestSessions's (SessionID, Format) key lines up.
func TestRedactClaudecode_SessionIDIsProjectScoped(t *testing.T) {
	t.Parallel()
	const rawProjectDir = "-Users-nejc-client-repo"
	projectDir := filepath.Join(t.TempDir(), rawProjectDir)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	src := filepath.Join(projectDir, "11111111-2222-3333-4444-555555555555.jsonl")
	fixture, err := os.ReadFile(filepath.Join("testdata", "claudecode-session.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	if err := os.WriteFile(src, fixture, 0o644); err != nil {
		t.Fatalf("write src: %v", err)
	}
	result, err := ClaudeCode().Redact(context.Background(), src,
		filepath.Join(t.TempDir(), "out.jsonl"),
		Options{Pipeline: newPipeline(t), AttachImages: true})
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	want := claudecode.ProjectScope(rawProjectDir) + "/11111111-2222-3333-4444-555555555555"
	if result.SessionID != want {
		t.Errorf("SessionID = %q, want %q", result.SessionID, want)
	}
	if strings.Contains(result.SessionID, "nejc") || strings.Contains(result.SessionID, "client") {
		t.Errorf("raw project dir leaked into SessionID: %q", result.SessionID)
	}
}

// Two sessions with identical UUID filenames in different project dirs must
// produce distinct SessionIDs so manifest dedup does not collapse them.
func TestRedactClaudecode_SessionIDDisambiguatesAcrossProjects(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "claudecode-session.jsonl"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	redactProject := func(project string) string {
		t.Helper()
		dir := filepath.Join(t.TempDir(), project)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		src := filepath.Join(dir, "11111111-2222-3333-4444-555555555555.jsonl")
		if err := os.WriteFile(src, fixture, 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		result, err := ClaudeCode().Redact(context.Background(), src,
			filepath.Join(t.TempDir(), "out.jsonl"),
			Options{Pipeline: newPipeline(t), AttachImages: true})
		if err != nil {
			t.Fatalf("redact: %v", err)
		}
		return result.SessionID
	}

	alpha := redactProject("project-alpha")
	beta := redactProject("project-beta")
	if alpha == beta {
		t.Fatalf("SessionIDs collided: alpha=%q beta=%q", alpha, beta)
	}
}

// The Codex session id is session_meta.payload.id. It stays constant as
// the session moves between sessions/ and archived_sessions/ directories
// and as the file grows with new turns.
func TestRedactCodex_SessionIDFromSessionMeta(t *testing.T) {
	t.Parallel()
	src := copyFixture(t, "codex-session.jsonl")
	result, err := Codex().Redact(context.Background(), src,
		filepath.Join(t.TempDir(), "out.jsonl"),
		Options{Pipeline: newPipeline(t), AttachImages: true})
	if err != nil {
		t.Fatalf("redact: %v", err)
	}
	if result.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want %q (from session_meta.payload.id)", result.SessionID, "sess-1")
	}
}

// Malformed JSON must surface as an error so callers can abort the extract
// rather than ship a corrupted, un-redacted line downstream. A valid top-level
// non-object (rare but legal JSONL) takes the text-redact fallback instead.
func TestRedact_MalformedJSONLineErrors(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "broken.jsonl")
	// A malformed line: missing closing brace. Embeds a secret to make sure
	// the pass-through bug (if reintroduced) would be caught by the assertion
	// that no output exists.
	broken := `{"type":"user","message":{"content":"sk-abcdefghijklmnopqrstuvwxyz0123"` + "\n"
	if err := os.WriteFile(src, []byte(broken), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}
	dst := filepath.Join(dir, "out.jsonl")
	_, err := ClaudeCode().Redact(context.Background(), src, dst,
		Options{Pipeline: newPipeline(t), AttachImages: true})
	if err == nil {
		t.Fatalf("expected redact to fail on malformed JSON, got nil")
	}
}

func TestRedact_ValidNonObjectLineIsTextRedacted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "scalar.jsonl")
	// A valid JSONL line that is not an object (a quoted string). Rare, but
	// must not error — the text-redact fallback should scrub any secret.
	scalar := `"sk-abcdefghijklmnopqrstuvwxyz0123"` + "\n"
	if err := os.WriteFile(src, []byte(scalar), 0o644); err != nil {
		t.Fatalf("write scalar fixture: %v", err)
	}
	dst := filepath.Join(dir, "out.jsonl")
	if _, err := ClaudeCode().Redact(context.Background(), src, dst,
		Options{Pipeline: newPipeline(t), AttachImages: true}); err != nil {
		t.Fatalf("redact: %v", err)
	}
	out, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if strings.Contains(string(out), "sk-abcdefghijklmnopqrstuvwxyz0123") {
		t.Fatalf("secret survived non-object text redact fallback: %q", out)
	}
}

func TestForOrigin(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"claudecode", "codex"} {
		if _, ok := ForOrigin(origin); !ok {
			t.Errorf("expected handler for %q", origin)
		}
	}
	if _, ok := ForOrigin("cursor"); ok {
		t.Errorf("cursor should not have a native handler yet")
	}
}
