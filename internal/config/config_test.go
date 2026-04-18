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
