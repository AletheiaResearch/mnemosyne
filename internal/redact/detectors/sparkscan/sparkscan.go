// Package sparkscan contributes a trufflehog-compatible detector for
// Sparkscan API keys. Sparkscan issues keys of the form
// `ss_{pk|sk}_{live|test}_<22+ alnum>`; both public (pk) and secret
// (sk) variants are matched so a single scanner covers the provider.
//
// Detection is regex-only; the pipeline never calls Sparkscan to
// verify a match. Register the scanner with the shared redact
// registry by blank-importing this package; see register.go.
package sparkscan

import (
	"context"
	"regexp"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

// Scanner implements detectors.Detector for Sparkscan API keys.
// Construct via NewScanner; the zero value is not valid.
type Scanner struct {
	regex *regexp.Regexp
}

// keyRegex uses zero-width `\b` boundaries. The body class is strictly
// `[A-Za-z0-9]`, so the first and last chars of any valid key are word
// chars and `\b` reliably fires on both sides.
//
// Consuming the boundary bytes (as detectors/posthog does to survive
// `-`-terminated bodies) would break adjacent-key detection here:
// Go `regexp.FindAllSubmatch` returns non-overlapping matches, so two
// keys separated by a single delimiter (e.g. `keyA\nkeyB`) would share
// that delimiter and only the first key would be emitted.
var keyRegex = regexp.MustCompile(`\b(ss_(?:pk|sk)_(?:live|test)_[A-Za-z0-9]{22,})\b`)

// NewScanner returns a regex-only Scanner for Sparkscan API keys.
func NewScanner() *Scanner {
	return &Scanner{regex: keyRegex}
}

var _ thdet.Detector = (*Scanner)(nil)

// Name returns the scanner identifier used in redact findings.
func (s *Scanner) Name() string { return "SparkscanAPIKey" }

// Keywords satisfies detectors.Detector — used by the aho-corasick
// prefilter to skip chunks with no plausible hit.
func (s *Scanner) Keywords() []string { return []string{"ss_sk_", "ss_pk_"} }

// Type satisfies detectors.Detector. Upstream trufflehog has no
// dedicated type for Sparkscan keys, so the name field carries the
// provider identity.
func (s *Scanner) Type() detector_typepb.DetectorType {
	return detector_typepb.DetectorType_CustomRegex
}

// Description satisfies detectors.Detector.
func (s *Scanner) Description() string {
	return "Sparkscan API key (ss_pk_/ss_sk_, live or test)."
}

// FromData scans data for Sparkscan keys and deduplicates within a
// single chunk. The verify argument is ignored — Sparkscan matches
// are never sent to an external endpoint.
func (s *Scanner) FromData(_ context.Context, _ bool, data []byte) ([]thdet.Result, error) {
	if len(data) == 0 {
		return nil, nil
	}
	matches := s.regex.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(matches))
	results := make([]thdet.Result, 0, len(matches))
	for _, match := range matches {
		key := string(match[1])
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		results = append(results, thdet.Result{
			DetectorType: s.Type(),
			DetectorName: s.Name(),
			Raw:          []byte(key),
			Redacted:     "ss_****",
		})
	}
	return results, nil
}
