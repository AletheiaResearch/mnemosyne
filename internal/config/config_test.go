package config

import (
	"os"
	"path/filepath"
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
}
