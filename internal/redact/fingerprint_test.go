package redact

import (
	"strings"
	"testing"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

func TestConfigFingerprint_StableAcrossOrder(t *testing.T) {
	t.Parallel()
	a := config.Config{
		CustomRedactions: []string{"alpha", "beta"},
		CustomHandles:    []string{"nejc", "alice"},
	}
	b := config.Config{
		CustomRedactions: []string{"beta", "alpha"},
		CustomHandles:    []string{"alice", "nejc"},
	}
	if ConfigFingerprint(a) != ConfigFingerprint(b) {
		t.Fatalf("fingerprint should be order-independent")
	}
}

func TestConfigFingerprint_DiffersWhenChanged(t *testing.T) {
	t.Parallel()
	a := config.Config{CustomRedactions: []string{"alpha"}}
	b := config.Config{CustomRedactions: []string{"alpha", "beta"}}
	if ConfigFingerprint(a) == ConfigFingerprint(b) {
		t.Fatalf("fingerprint should change when redactions change")
	}
}

func TestConfigFingerprint_IgnoresWhitespace(t *testing.T) {
	t.Parallel()
	a := config.Config{CustomRedactions: []string{"alpha", "", "  "}}
	b := config.Config{CustomRedactions: []string{"alpha"}}
	if ConfigFingerprint(a) != ConfigFingerprint(b) {
		t.Fatalf("fingerprint should ignore empty/whitespace entries")
	}
}

func TestRedactionKey_FormatAndImageMode(t *testing.T) {
	t.Parallel()
	cfg := config.Config{}
	keepKey := RedactionKey(cfg, true)
	stripKey := RedactionKey(cfg, false)

	if keepKey == stripKey {
		t.Fatalf("keep vs strip should produce different keys")
	}
	if !strings.HasSuffix(keepKey, ":keep-images") {
		t.Fatalf("keep key suffix = %q", keepKey)
	}
	if !strings.HasSuffix(stripKey, ":strip-images") {
		t.Fatalf("strip key suffix = %q", stripKey)
	}
	if !strings.HasPrefix(keepKey, "v1:"+PipelineVersion+":") {
		t.Fatalf("unexpected prefix: %q", keepKey)
	}
}
