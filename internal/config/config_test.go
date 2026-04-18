package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSavePreservesUnknownKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	input := `{"destination_repo":"alice/archive","future_key":{"flag":true}}`
	if err := os.WriteFile(path, []byte(input), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ScopeConfirmed = true

	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) == "" || cfg.Unknown["future_key"] == nil {
		t.Fatal("expected unknown keys to be preserved")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config file mode 0600, got %04o", got)
	}
}

func TestChatTemplatePersistsAndRoundTrips(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	cfg.ChatTemplate = &ChatTemplate{
		Name:                "chatml",
		BOSToken:            "<s>",
		EOSToken:            "</s>",
		AddGenerationPrompt: true,
	}
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"chat_template"`) {
		t.Fatalf("expected chat_template in persisted config: %s", raw)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ChatTemplate == nil {
		t.Fatal("expected ChatTemplate to round-trip")
	}
	if loaded.ChatTemplate.Name != "chatml" || !loaded.ChatTemplate.AddGenerationPrompt {
		t.Fatalf("unexpected chat template: %+v", loaded.ChatTemplate)
	}
}

func TestChatTemplateEmptyIsOmitted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := Default()
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatal(err)
	}
	if _, ok := parsed["chat_template"]; ok {
		t.Fatalf("did not expect chat_template in empty config: %s", raw)
	}
}

func TestLoadReturnsDefaultsWhenFileMissing(t *testing.T) {
	t.Parallel()

	cfg, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PhaseMarker != PhaseInitial {
		t.Fatalf("expected initial phase, got %q", cfg.PhaseMarker)
	}
	if cfg.ExcludedGroupings == nil || cfg.Unknown == nil {
		t.Fatalf("expected zero-valued slices/maps to be initialized")
	}
}

func TestLoadHandlesEmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte("   \n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.PhaseMarker != PhaseInitial {
		t.Fatalf("expected initial phase, got %q", cfg.PhaseMarker)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatalf("expected parse error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "load settings") {
		t.Fatalf("expected wrapped parse error, got %v", err)
	}
}

func TestDefaultProducesUsableConfig(t *testing.T) {
	t.Parallel()

	cfg := Default()
	if cfg.PhaseMarker != PhaseInitial {
		t.Fatalf("phase marker = %q", cfg.PhaseMarker)
	}
	if cfg.ExcludedGroupings == nil || cfg.CustomRedactions == nil || cfg.CustomHandles == nil || cfg.Unknown == nil {
		t.Fatalf("default should have initialized slices/maps: %+v", cfg)
	}
}

func TestMergeStringSliceDedupesAndSorts(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.ExcludedGroupings = []string{"b", "", "a"}
	cfg.MergeStringSlice(&cfg.ExcludedGroupings, []string{" a ", "c", "", "b"})
	want := []string{"a", "b", "c"}
	if len(cfg.ExcludedGroupings) != 3 {
		t.Fatalf("expected 3 unique entries, got %+v", cfg.ExcludedGroupings)
	}
	for i, v := range want {
		if cfg.ExcludedGroupings[i] != v {
			t.Fatalf("position %d: got %q, want %q (full=%v)", i, cfg.ExcludedGroupings[i], v, cfg.ExcludedGroupings)
		}
	}
}

func TestMaskSecretLengthBuckets(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":        "[redacted]",
		"abc":     "[redacted]",
		"abcd":    "a**d",
		"abcdef":  "a****f",
		"abcdefgh": "a******h",
		"abcdefghi":         "abcd*efghi"[:len("abcdefghi")], // placeholder, replaced below
	}
	// recompute the 9-char and longer cases explicitly to avoid copy/paste math mistakes.
	cases["abcdefghi"] = "abcd" + strings.Repeat("*", 1) + "fghi"
	cases["abcdefghij0123"] = "abcd" + strings.Repeat("*", len("abcdefghij0123")-8) + "0123"

	for input, want := range cases {
		if got := MaskSecret(input); got != want {
			t.Fatalf("MaskSecret(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestMaskedReplacesCustomRedactionsInPlace(t *testing.T) {
	t.Parallel()
	cfg := Default()
	cfg.CustomRedactions = []string{"secret-value-123", "xy"}
	masked := cfg.Masked()
	if masked.CustomRedactions[0] == "secret-value-123" {
		t.Fatalf("expected redaction to be masked")
	}
	if masked.CustomRedactions[1] != "[redacted]" {
		t.Fatalf("short values should fall back to [redacted], got %q", masked.CustomRedactions[1])
	}
	// Original slice must not be mutated.
	if cfg.CustomRedactions[0] != "secret-value-123" {
		t.Fatalf("Masked mutated the source slice")
	}
}

func TestRefreshPhaseFollowsAvailabilityLadder(t *testing.T) {
	t.Parallel()

	cfg := Default()
	cfg.RefreshPhase(false)
	if cfg.PhaseMarker != PhaseInitial {
		t.Fatalf("no-credentials = %q", cfg.PhaseMarker)
	}
	cfg.RefreshPhase(true)
	if cfg.PhaseMarker != PhasePreparing {
		t.Fatalf("credentials only = %q", cfg.PhaseMarker)
	}
	cfg.LastExtract = &LastExtract{Timestamp: "2026-04-17T10:00:00Z"}
	cfg.RefreshPhase(true)
	if cfg.PhaseMarker != PhasePendingReview {
		t.Fatalf("after extract = %q", cfg.PhaseMarker)
	}
	cfg.LastAttest = &LastAttest{Timestamp: "2026-04-17T10:10:00Z"}
	cfg.RefreshPhase(true)
	if cfg.PhaseMarker != PhaseCleared {
		t.Fatalf("after attest = %q", cfg.PhaseMarker)
	}
	cfg.PublicationAttestation = "hash"
	cfg.RefreshPhase(true)
	if cfg.PhaseMarker != PhaseFinalized {
		t.Fatalf("after publish = %q", cfg.PhaseMarker)
	}
	if !cfg.LastPublishAvailable() {
		t.Fatalf("expected LastPublishAvailable once attestation set")
	}
}

func TestChatTemplateHelpers(t *testing.T) {
	t.Parallel()
	if !(ChatTemplate{}).IsEmpty() {
		t.Fatal("zero value should be empty")
	}
	if (ChatTemplate{Name: "x"}).IsEmpty() {
		t.Fatal("populated template should not be empty")
	}
	cfg := Default()
	if !cfg.ChatTemplateValue().IsEmpty() {
		t.Fatal("nil ChatTemplate should yield empty value")
	}
	cfg.ChatTemplate = &ChatTemplate{Name: "chatml"}
	if cfg.ChatTemplateValue().Name != "chatml" {
		t.Fatalf("unexpected template value: %+v", cfg.ChatTemplateValue())
	}
}

func TestFileAndDirReturnPathsUnderAppName(t *testing.T) {
	// os.UserConfigDir resolves against XDG_CONFIG_HOME on Linux and HOME on darwin.
	// t.Setenv precludes t.Parallel here.
	fakeRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", fakeRoot)
	t.Setenv("HOME", fakeRoot)

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, AppName) {
		t.Fatalf("Dir() = %q, expected suffix %q", dir, AppName)
	}
	file, err := File()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(file) != DefaultFileName || filepath.Dir(file) != dir {
		t.Fatalf("File() = %q, Dir() = %q", file, dir)
	}
}
