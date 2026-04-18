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

	// Staging file must exist and be redacted.
	stagingPath := filepath.Join(filepath.Dir(outPath), "isolate", "claudecode", "session.jsonl")
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
	if session.File != "claudecode/session.jsonl" {
		t.Errorf("session.File = %q", session.File)
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
