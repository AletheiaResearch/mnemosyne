package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
)

// PipelineVersion identifies the redactor's detector ruleset. Bump this
// whenever detector patterns or anonymizer semantics change materially so
// consumers can distinguish outputs produced under different rules.
const PipelineVersion = "v1"

// PipelineFingerprint returns a stable identifier for the current redactor
// implementation. It does not incorporate user-specific configuration.
func PipelineFingerprint() string {
	return PipelineVersion
}

// ConfigFingerprint returns a deterministic "sha256:<hex>" digest derived from
// the user-tunable inputs to the redactor (custom literal redactions and custom
// handles). Two configs with the same trimmed+sorted lists produce the same
// fingerprint regardless of original ordering or whitespace.
func ConfigFingerprint(cfg config.Config) string {
	redactions := normalizeList(cfg.CustomRedactions)
	handles := normalizeList(cfg.CustomHandles)

	var builder strings.Builder
	builder.WriteString("redactions:")
	for _, item := range redactions {
		builder.WriteString(item)
		builder.WriteByte('\n')
	}
	builder.WriteString("handles:")
	for _, item := range handles {
		builder.WriteString(item)
		builder.WriteByte('\n')
	}

	sum := sha256.Sum256([]byte(builder.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// RedactionKey combines the pipeline version, config fingerprint, and image
// attachment mode into a single opaque identifier. Two outputs carrying the
// same RedactionKey were produced by the same rules with the same image
// handling policy. The format matches the "v1:<...>" style of
// pi-share-hf/manifest.jsonl so downstream tooling can treat both uniformly.
func RedactionKey(cfg config.Config, attachImages bool) string {
	images := "strip-images"
	if attachImages {
		images = "keep-images"
	}
	return strings.Join([]string{
		"v1",
		PipelineFingerprint(),
		ConfigFingerprint(cfg),
		images,
	}, ":")
}

func normalizeList(input []string) []string {
	out := make([]string, 0, len(input))
	for _, item := range input {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	slices.Sort(out)
	return out
}
