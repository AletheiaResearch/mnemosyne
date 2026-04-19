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

func TestParseRedactionKey_RoundTrip(t *testing.T) {
	t.Parallel()
	cfg := config.Config{
		CustomRedactions: []string{"alpha", "beta"},
		CustomHandles:    []string{"nejc"},
	}
	for _, keep := range []bool{true, false} {
		key := RedactionKey(cfg, keep)
		pipelineFP, configFP, keepImages, ok := ParseRedactionKey(key)
		if !ok {
			t.Fatalf("ParseRedactionKey(%q) failed", key)
		}
		if pipelineFP != PipelineFingerprint() {
			t.Errorf("pipelineFP = %q, want %q", pipelineFP, PipelineFingerprint())
		}
		if configFP != ConfigFingerprint(cfg) {
			t.Errorf("configFP = %q, want %q", configFP, ConfigFingerprint(cfg))
		}
		if keepImages != keep {
			t.Errorf("keepImages = %v, want %v", keepImages, keep)
		}
	}
}

func TestParseRedactionKey_Malformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		key  string
	}{
		{"empty", ""},
		{"no colons", "nothing"},
		{"wrong suffix", "v1:v1:sha256:abc:other"},
		{"wrong version", "v2:v1:sha256:abc:keep-images"},
		{"missing pipeline", "v1::sha256:abc:keep-images"},
		{"missing sha256 prefix", "v1:v1:notsha:abc:keep-images"},
		{"too few parts", "v1:keep-images"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, ok := ParseRedactionKey(tc.key)
			if ok {
				t.Fatalf("ParseRedactionKey(%q) = ok, want malformed", tc.key)
			}
		})
	}
}
