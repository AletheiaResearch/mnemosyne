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

// ParseRedactionKey is the inverse of RedactionKey. It recovers the
// redaction posture a session was produced under from the opaque key
// string stored on IsolateSession, without consulting any live config.
// Use it at publish time so the manifest header describes the bytes
// actually being uploaded (which were redacted at extract time) rather
// than whatever config happens to be loaded when publish runs.
//
// The key format is "v1:<pipeline>:<config>:<keep|strip>-images" where
// <config> is itself "sha256:<hex>". Splitting from the right isolates
// the image suffix; the remaining prefix is split from the left to
// recover pipeline and config fingerprints. ok=false for empty or
// malformed keys (unknown manifest version, wrong suffix, missing
// fields); callers should treat !ok as a hard error rather than
// fabricating values.
func ParseRedactionKey(key string) (pipelineFP, configFP string, keepImages, ok bool) {
	if key == "" {
		return "", "", false, false
	}
	lastColon := strings.LastIndex(key, ":")
	if lastColon < 0 {
		return "", "", false, false
	}
	suffix := key[lastColon+1:]
	switch suffix {
	case "keep-images":
		keepImages = true
	case "strip-images":
		keepImages = false
	default:
		return "", "", false, false
	}
	prefix := key[:lastColon]
	// prefix = "<manifestVersion>:<pipeline>:sha256:<hex>"
	// SplitN on ":" into 4 parts so the "sha256:<hex>" stays intact.
	parts := strings.SplitN(prefix, ":", 4)
	if len(parts) != 4 {
		return "", "", false, false
	}
	manifestVersion := parts[0]
	if manifestVersion != "v1" {
		return "", "", false, false
	}
	pipelineFP = parts[1]
	configFP = parts[2] + ":" + parts[3]
	if pipelineFP == "" || !strings.HasPrefix(configFP, "sha256:") {
		return "", "", false, false
	}
	return pipelineFP, configFP, keepImages, true
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
