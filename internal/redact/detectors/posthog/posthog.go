// Package posthog contributes trufflehog-compatible detectors for
// PostHog-issued keys. It covers three prefixes:
//
//   - phx_ personal API keys
//   - phs_ feature-flag secure keys
//   - phc_ project capture keys
//
// Detection is regex-only; the pipeline never calls the PostHog API to
// verify a match. Register the scanners with the shared redact registry
// by blank-importing this package; see register.go.
package posthog

import (
	"context"
	"regexp"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

// Scanner implements detectors.Detector for one flavour of PostHog key.
// Construct via the New*Scanner factories; the zero value is not valid.
type Scanner struct {
	name        string
	prefix      string
	description string
	version     int
	regex       *regexp.Regexp
}

// Version values disambiguate PostHog detectors inside trufflehog's
// aho-corasick registry — without them, the three flavours collide on
// DetectorKey (same type, empty version, empty name) and only one
// survives. See ahocorasick.CreateDetectorKey in the upstream package.
const (
	versionPersonalAPIKey       = 1
	versionFeatureFlagSecureKey = 2
	versionProjectAPIKey        = 3
)

// keyRegex returns the bounded match pattern for a given PostHog key
// prefix. 32+ characters comfortably covers every issued length while
// rejecting shorter false positives.
//
// ASCII `\b` treats `-` as a non-word char, so a key whose final byte
// is `-` fails the trailing word boundary; RE2 then backtracks one
// char and returns a truncated capture that leaks the last character
// of the secret into the output. Explicit non-word-char boundaries
// hold up regardless of whether the key's ends are word chars.
func keyRegex(prefix string) *regexp.Regexp {
	return regexp.MustCompile(
		`(?:^|[^A-Za-z0-9_])(` + regexp.QuoteMeta(prefix) + `[A-Za-z0-9_-]{32,})(?:$|[^A-Za-z0-9_])`,
	)
}

// NewPersonalAPIKeyScanner returns a regex-only Scanner for phx_
// personal API keys.
func NewPersonalAPIKeyScanner() *Scanner {
	return buildScanner(
		"PostHogPersonalAPIKey",
		"phx_",
		"PostHog personal API key (grants access to the authenticated user's account).",
		versionPersonalAPIKey,
	)
}

// NewFeatureFlagSecureKeyScanner returns a regex-only Scanner for phs_
// feature-flag secure keys.
func NewFeatureFlagSecureKeyScanner() *Scanner {
	return buildScanner(
		"PostHogFeatureFlagSecureKey",
		"phs_",
		"PostHog feature-flag secure key (server-side secret used with /flags).",
		versionFeatureFlagSecureKey,
	)
}

// NewProjectAPIKeyScanner returns a regex-only Scanner for phc_ project
// capture keys.
func NewProjectAPIKeyScanner() *Scanner {
	return buildScanner(
		"PostHogProjectAPIKey",
		"phc_",
		"PostHog project capture key (client-side token posted to /capture).",
		versionProjectAPIKey,
	)
}

func buildScanner(name, prefix, description string, version int) *Scanner {
	return &Scanner{
		name:        name,
		prefix:      prefix,
		description: description,
		version:     version,
		regex:       keyRegex(prefix),
	}
}

var _ thdet.Detector = (*Scanner)(nil)

// Name returns the scanner identifier used in redact findings.
func (s *Scanner) Name() string { return s.name }

// Prefix returns the key prefix the scanner matches.
func (s *Scanner) Prefix() string { return s.prefix }

// Keywords satisfies detectors.Detector — used by the aho-corasick
// prefilter to avoid running FromData against chunks with no plausible
// hit.
func (s *Scanner) Keywords() []string { return []string{s.prefix} }

// Type satisfies detectors.Detector. Upstream trufflehog has no
// dedicated type for PostHog feature-flag or project keys, so every
// flavour maps to CustomRegex and the name field carries the flavour.
func (s *Scanner) Type() detector_typepb.DetectorType {
	return detector_typepb.DetectorType_CustomRegex
}

// Description satisfies detectors.Detector.
func (s *Scanner) Description() string { return s.description }

// Version disambiguates PostHog detectors inside trufflehog's
// aho-corasick registry; see the const block above.
func (s *Scanner) Version() int { return s.version }

// FromData scans data for the scanner's prefix and deduplicates within a
// single chunk. The verify argument is ignored — PostHog matches are
// never sent to an external endpoint by this package.
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
			DetectorName: s.name,
			Raw:          []byte(key),
			Redacted:     s.prefix + "****",
		})
	}
	return results, nil
}
