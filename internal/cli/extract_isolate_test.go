package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

const extractIsolateClaudecode = `{"type":"user","uuid":"u-1","cwd":"/Users/nejc/repo","message":{"role":"user","content":[{"type":"text","text":"reach me at alice@contoso.dev"}]}}
{"type":"assistant","uuid":"u-2","message":{"role":"assistant","content":[{"type":"text","text":"use sk-abcdefghijklmnopqrstuvwxyz0123 for the demo"}]}}
`

func runExtract(t *testing.T, rt *runtime, args ...string) (string, error) {
	t.Helper()
	cmd := newExtractCommand(rt)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestExtractIsolate_ProducesStagingFilesAndPreservesSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a Claude Code session under the expected path.
	projDir := filepath.Join(home, ".claude", "projects", "demo")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	sessionPath := filepath.Join(projDir, "session.jsonl")
	if err := os.WriteFile(sessionPath, []byte(extractIsolateClaudecode), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	srcBefore, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read src: %v", err)
	}
	srcHashBefore := sha256.Sum256(srcBefore)

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	outPath := filepath.Join(t.TempDir(), "canonical.jsonl")

	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}

	if _, err := runExtract(t, rt,
		"--scope", "claudecode",
		"--include-all",
		"--output", outPath,
		"--isolate",
		"--attach-images",
	); err != nil {
		t.Fatalf("extract: %v", err)
	}

	// Source file must be byte-identical.
	srcAfter, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read src after: %v", err)
	}
	srcHashAfter := sha256.Sum256(srcAfter)
	if srcHashBefore != srcHashAfter {
		t.Fatalf("source file mutated during extract: before=%s after=%s",
			hex.EncodeToString(srcHashBefore[:]), hex.EncodeToString(srcHashAfter[:]))
	}

	// Staging file must exist and be redacted. The path is namespaced by
	// a hash prefix of the source's parent directory, so discover it.
	stagingPath := findSingleStagingFile(t, filepath.Join(filepath.Dir(outPath), "isolate", "claudecode"), "session.jsonl")
	stagingData, err := os.ReadFile(stagingPath)
	if err != nil {
		t.Fatalf("read staging: %v", err)
	}
	text := string(stagingData)
	if strings.Contains(text, "alice@contoso.dev") {
		t.Errorf("email survived redaction in staging file")
	}
	if strings.Contains(text, "sk-abcdefghijklmnopqrstuvwxyz0123") {
		t.Errorf("cloud key survived redaction in staging file")
	}
	if !strings.Contains(text, "[MNEMOSYNE_REDACTED]") {
		t.Errorf("staging file missing placeholder marker")
	}

	// Config must record the isolate session.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LastExtract == nil {
		t.Fatalf("LastExtract is nil")
	}
	if len(cfg.LastExtract.IsolateSessions) != 1 {
		t.Fatalf("isolate sessions = %d, want 1", len(cfg.LastExtract.IsolateSessions))
	}
	session := cfg.LastExtract.IsolateSessions[0]
	if !strings.HasPrefix(session.File, "claudecode/") || !strings.HasSuffix(session.File, "/session.jsonl") {
		t.Errorf("session.File = %q, want claudecode/<prefix>/session.jsonl", session.File)
	}
	if session.Format != "claudecode" {
		t.Errorf("session.Format = %q", session.Format)
	}
	if session.SourceHash == "" || session.RedactedHash == "" {
		t.Errorf("hashes not populated: %+v", session)
	}
	if session.RedactionKey == "" || !strings.Contains(session.RedactionKey, "keep-images") {
		t.Errorf("redaction key unexpected: %q", session.RedactionKey)
	}
	if !cfg.IsolateExport || !cfg.AttachImages {
		t.Errorf("IsolateExport/AttachImages not persisted: isolate=%v attach=%v", cfg.IsolateExport, cfg.AttachImages)
	}
}

// Subagent records carry Provenance.SourcePath that points at a directory
// (the per-session subagents/ folder), which the native redactor cannot
// open as a JSONL file. Isolate mode must ignore such records gracefully
// and still emit the canonical record.
func TestExtractIsolate_SkipsSubagentDirectoryProvenance(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Seed a claudecode project with a primary session AND a subagents/
	// directory that would otherwise produce a directory-valued Provenance.
	projDir := filepath.Join(home, ".claude", "projects", "demo")
	sessionID := "root"
	primaryPath := filepath.Join(projDir, sessionID+".jsonl")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir proj: %v", err)
	}
	if err := os.WriteFile(primaryPath, []byte(extractIsolateClaudecode), 0o644); err != nil {
		t.Fatalf("write primary: %v", err)
	}

	subagentDir := filepath.Join(projDir, sessionID, "subagents")
	if err := os.MkdirAll(subagentDir, 0o755); err != nil {
		t.Fatalf("mkdir subagents: %v", err)
	}
	subagentFile := filepath.Join(subagentDir, "child.jsonl")
	if err := os.WriteFile(subagentFile, []byte(extractIsolateClaudecode), 0o644); err != nil {
		t.Fatalf("write subagent: %v", err)
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	outPath := filepath.Join(t.TempDir(), "canonical.jsonl")
	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}

	out, err := runExtract(t, rt,
		"--scope", "claudecode",
		"--include-all",
		"--output", outPath,
		"--isolate",
	)
	if err != nil {
		t.Fatalf("extract: %v\noutput: %s", err, out)
	}

	// Only the primary session should produce a staging file; the
	// subagent directory must be skipped cleanly.
	stagingRoot := filepath.Join(filepath.Dir(outPath), "isolate", "claudecode")
	jsonlFiles := collectJSONLFiles(t, stagingRoot)
	if len(jsonlFiles) != 1 || filepath.Base(jsonlFiles[0]) != sessionID+".jsonl" {
		t.Errorf("staging files = %v, want one %s.jsonl", jsonlFiles, sessionID)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LastExtract == nil {
		t.Fatalf("LastExtract is nil")
	}
	if len(cfg.LastExtract.IsolateSessions) != 1 {
		t.Errorf("isolate sessions = %d, want 1 (subagent dir should be skipped)", len(cfg.LastExtract.IsolateSessions))
	}
	// Canonical record for the subagent tree should still exist: the
	// consumer pipeline is not affected by isolate skipping.
	canonical, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read canonical: %v", err)
	}
	if !strings.Contains(string(canonical), ":subagents") {
		t.Errorf("canonical record for subagent tree missing; got:\n%s", canonical)
	}
}

// Two Claude Code projects that happen to contain a session file with
// the same basename must not collide in the staging directory or in the
// manifest — otherwise the second source silently overwrites the first
// and the stored redacted_hash stops describing the bytes on disk.
func TestExtractIsolate_DisambiguatesSameBasenameAcrossProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	projA := filepath.Join(home, ".claude", "projects", "projA")
	projB := filepath.Join(home, ".claude", "projects", "projB")
	for _, dir := range []string{projA, projB} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), []byte(extractIsolateClaudecode), 0o644); err != nil {
			t.Fatalf("write %s: %v", dir, err)
		}
	}

	cfgPath := filepath.Join(t.TempDir(), "config.json")
	outPath := filepath.Join(t.TempDir(), "canonical.jsonl")
	rt := &runtime{
		configPath: cfgPath,
		logger:     newLogger(false),
		stdout:     &bytes.Buffer{},
		stderr:     &bytes.Buffer{},
	}
	if out, err := runExtract(t, rt,
		"--scope", "claudecode",
		"--include-all",
		"--output", outPath,
		"--isolate",
	); err != nil {
		t.Fatalf("extract: %v\n%s", err, out)
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.LastExtract == nil {
		t.Fatalf("LastExtract is nil")
	}
	if len(cfg.LastExtract.IsolateSessions) != 2 {
		t.Fatalf("isolate sessions = %d, want 2", len(cfg.LastExtract.IsolateSessions))
	}

	seenFile := make(map[string]struct{}, 2)
	seenStaging := make(map[string]struct{}, 2)
	for _, s := range cfg.LastExtract.IsolateSessions {
		if !strings.HasPrefix(s.File, "claudecode/") || !strings.HasSuffix(s.File, "/session.jsonl") {
			t.Errorf("session.File = %q, want claudecode/<prefix>/session.jsonl", s.File)
		}
		if _, dup := seenFile[s.File]; dup {
			t.Errorf("duplicate session.File across distinct sources: %q", s.File)
		}
		seenFile[s.File] = struct{}{}
		if _, dup := seenStaging[s.StagingPath]; dup {
			t.Errorf("two sources share a staging path: %q", s.StagingPath)
		}
		seenStaging[s.StagingPath] = struct{}{}
		if _, err := os.Stat(s.StagingPath); err != nil {
			t.Errorf("staging file %q missing: %v", s.StagingPath, err)
		}
	}
}

// findSingleStagingFile returns the staging file under root whose
// basename matches name. It fails the test if there is not exactly one.
func findSingleStagingFile(t *testing.T, root, name string) string {
	t.Helper()
	files := collectJSONLFiles(t, root)
	matches := make([]string, 0, 1)
	for _, f := range files {
		if filepath.Base(f) == name {
			matches = append(matches, f)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("expected exactly one %s under %s, got %v", name, root, matches)
	}
	return matches[0]
}

func collectJSONLFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	if err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(path) == ".jsonl" {
			out = append(out, path)
		}
		return nil
	}); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}
