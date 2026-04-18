package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func runConfigure(t *testing.T, cfgPath string, args ...string) (string, error) {
	t.Helper()
	rt := &runtime{configPath: cfgPath}
	cmd := newConfigureCommand(rt)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	err := cmd.ExecuteContext(context.Background())
	return buf.String(), err
}

func TestConfigureChatTemplateNameClearsFile(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if _, err := runConfigure(t, cfgPath, "--chat-template-file", "/tmp/custom.tmpl"); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	if _, err := runConfigure(t, cfgPath, "--chat-template-name", "chatml"); err != nil {
		t.Fatalf("set name: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatTemplate == nil {
		t.Fatal("expected ChatTemplate to be set")
	}
	if cfg.ChatTemplate.Name != "chatml" {
		t.Errorf("Name = %q, want chatml", cfg.ChatTemplate.Name)
	}
	if cfg.ChatTemplate.File != "" {
		t.Errorf("File = %q, want cleared", cfg.ChatTemplate.File)
	}
}

func TestConfigureChatTemplateFileClearsName(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	if _, err := runConfigure(t, cfgPath, "--chat-template-name", "chatml"); err != nil {
		t.Fatalf("seed name: %v", err)
	}
	if _, err := runConfigure(t, cfgPath, "--chat-template-file", "/tmp/custom.tmpl"); err != nil {
		t.Fatalf("set file: %v", err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatTemplate.File != "/tmp/custom.tmpl" {
		t.Errorf("File = %q", cfg.ChatTemplate.File)
	}
	if cfg.ChatTemplate.Name != "" {
		t.Errorf("Name = %q, want cleared", cfg.ChatTemplate.Name)
	}
}

func TestConfigureChatTemplateBothNonEmptyInvalid(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	_, err := runConfigure(t, cfgPath,
		"--chat-template-name", "chatml",
		"--chat-template-file", "/tmp/custom.tmpl",
	)
	if err == nil {
		t.Fatal("expected error when both set non-empty")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigureChatTemplateTUIEmptySubmitLeavesConfigClean(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	// Simulate TUI always sending every chat-template flag, with all blanks.
	_, err := runConfigure(t, cfgPath,
		"--scope", "all",
		"--chat-template-name", "",
		"--chat-template-file", "",
		"--chat-template-bos-token", "",
		"--chat-template-eos-token", "",
		"--chat-template-add-generation-prompt=false",
	)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ChatTemplate != nil {
		t.Errorf("expected ChatTemplate to stay nil, got %+v", cfg.ChatTemplate)
	}
}
