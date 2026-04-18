package redact

import (
	"context"
	"time"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/engine/ahocorasick"
)

// verifyBudget bounds the total time spent verifying a single input
// across every detector the runner drives. Individual HTTP requests
// carry their own per-request timeouts via NewDetectorHttpClient.
const verifyBudget = 15 * time.Second

// trufflehogRunner plugs a set of trufflehog Detector implementations
// into the redact Pipeline. It mirrors the built-in regex Detector
// interface: Redact replaces every matched secret with PlaceholderMarker
// and Scan folds hits into Findings.
//
// Detectors are dispatched through an aho-corasick trie built at
// construction time. For each call only detectors whose keywords appear
// in the input are invoked, so the ~800 upstream defaults impose minimal
// per-call cost.
type trufflehogRunner struct {
	core   *ahocorasick.Core
	verify bool
}

func newTrufflehogRunner(ds []thdet.Detector, verify bool) *trufflehogRunner {
	if len(ds) == 0 {
		return &trufflehogRunner{verify: verify}
	}
	return &trufflehogRunner{
		core:   ahocorasick.NewAhoCorasickCore(ds),
		verify: verify,
	}
}

// Redact finds secrets via the configured detectors and replaces each
// raw match with PlaceholderMarker. When stats is non-nil, verified
// results (Result.Verified) are recorded there so callers of
// Pipeline.ApplyRecord can surface them in summaries.
func (r *trufflehogRunner) Redact(input string, stats *ApplyStats) (string, int) {
	if r == nil || r.core == nil || input == "" {
		return input, 0
	}
	data := []byte(input)
	matches := r.core.FindDetectorMatches(data)
	if len(matches) == 0 {
		return input, 0
	}

	ctx, cancel := r.context()
	defer cancel()

	out := input
	count := 0
	seen := make(map[string]struct{})
	for _, m := range matches {
		for _, chunk := range m.Matches() {
			results, err := m.FromData(ctx, r.verify, chunk)
			if err != nil || len(results) == 0 {
				continue
			}
			for _, result := range results {
				raw, rawV2 := string(result.Raw), string(result.RawV2)
				// RawV2 carries the full multipart credential ("id:secret")
				// when the detector emits one; Raw only holds the identifier
				// half. Prefer RawV2 for dedup/fingerprints so two AWS keys
				// that share an access-key ID aren't collapsed into one
				// verified entry.
				key := raw
				if rawV2 != "" {
					key = rawV2
				}
				if key == "" {
					continue
				}
				if result.Verified && stats != nil {
					stats.recordVerified(key, findingLabel(result))
				}
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				// Strip the combined form first so the full "id:secret"
				// substring is redacted when it appears contiguously, then
				// fall through to the bare identifier so the ID is still
				// scrubbed when the two halves are split across lines
				// (e.g. a .env file with ID and secret on separate keys).
				if rawV2 != "" {
					replaced, n := stringsReplaceAll(out, rawV2, PlaceholderMarker)
					out = replaced
					count += n
				}
				if raw != "" && raw != rawV2 {
					replaced, n := stringsReplaceAll(out, raw, PlaceholderMarker)
					out = replaced
					count += n
				}
			}
		}
	}
	return out, count
}

// Scan reports detector hits into findings. Raw values are deduped via
// Findings.markToken so tokens already counted by a regex pass on the
// same findings don't get counted again. Keys confirmed by a verifier
// contribute once to VerifiedSecretCount.
func (r *trufflehogRunner) Scan(input string, findings *Findings) {
	if r == nil || r.core == nil || input == "" || findings == nil {
		return
	}
	data := []byte(input)
	matches := r.core.FindDetectorMatches(data)
	if len(matches) == 0 {
		return
	}

	ctx, cancel := r.context()
	defer cancel()

	seenVerified := make(map[string]struct{})
	for _, m := range matches {
		for _, chunk := range m.Matches() {
			results, err := m.FromData(ctx, r.verify, chunk)
			if err != nil || len(results) == 0 {
				continue
			}
			for _, result := range results {
				raw, rawV2 := string(result.Raw), string(result.RawV2)
				// Token dedup uses Raw so a credential caught by both the
				// regex pass (which stores the secret half it matched) and
				// this trufflehog run gets counted once. Keying on RawV2
				// here would miss the cross-detector overlap and
				// double-count any multipart secret (e.g. an Algolia
				// app-id/API-key pair flagged by both passes).
				tokenKey := raw
				if tokenKey == "" {
					tokenKey = rawV2
				}
				if tokenKey == "" {
					continue
				}
				if findings.markToken(tokenKey) {
					findings.TokenCount++
					if len(findings.Tokens) < 20 {
						findings.Tokens = append(findings.Tokens, findingLabel(result))
					}
				}
				if !result.Verified {
					continue
				}
				// Verified-secret dedup prefers RawV2 so two distinct
				// multipart credentials that share an identifier half
				// aren't collapsed into one verified entry (the symmetric
				// treatment Redact applies).
				verifiedKey := rawV2
				if verifiedKey == "" {
					verifiedKey = raw
				}
				if _, dup := seenVerified[verifiedKey]; dup {
					continue
				}
				seenVerified[verifiedKey] = struct{}{}
				findings.VerifiedSecretCount++
				if len(findings.VerifiedSecrets) < 20 {
					findings.VerifiedSecrets = append(findings.VerifiedSecrets, findingLabel(result))
				}
			}
		}
	}
}

func (r *trufflehogRunner) context() (context.Context, context.CancelFunc) {
	if !r.verify {
		// Regex-only path never performs network I/O, so the context
		// is nominal — a cancel-on-return is enough.
		ctx, cancel := context.WithCancel(context.Background())
		return ctx, cancel
	}
	return context.WithTimeout(context.Background(), verifyBudget)
}

// findingLabel builds the short "<detector>:<redacted>" tag used in
// Findings. Falls back to the detector type when a detector does not
// populate Name, and to just the name when no Redacted hint is present.
func findingLabel(r thdet.Result) string {
	name := r.DetectorName
	if name == "" {
		name = r.DetectorType.String()
	}
	if r.Redacted != "" {
		return name + ":" + r.Redacted
	}
	return name
}
