// Package posthog contributes trufflehog-compatible detectors for
// PostHog-issued keys. It covers three prefixes:
//
//   - phx_ personal API keys (verifiable via /api/users/@me/ on the app host)
//   - phs_ feature-flag secure keys (no public verification surface)
//   - phc_ project capture keys (verifiable via /flags/?v=2 on the ingestion host)
//
// Register the scanners with the shared redact registry by blank
// importing this package; see register.go. Callers that need explicit
// control (for example tests) can build a Scanner directly with the
// exported Option functions.
package posthog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

// DefaultBaseURLs lists the PostHog app-host region roots used by the
// personal-API-key verifier. Order matters: the first host is tried
// first and remaining hosts are fallbacks used when the primary returns
// 401 (the key may belong to a different region).
var DefaultBaseURLs = []string{
	"https://us.posthog.com",
	"https://eu.posthog.com",
}

// DefaultProjectAPIBaseURLs lists the PostHog ingestion-host region
// roots used by the project-API-key verifier. PostHog documents project
// (phc_) flag evaluation on the `.i.posthog.com` hosts rather than the
// app hosts used for personal keys.
var DefaultProjectAPIBaseURLs = []string{
	"https://us.i.posthog.com",
	"https://eu.i.posthog.com",
}

// defaultHTTPClient is built lazily so the regex-only path pays no
// startup cost. It forbids local addresses (mirroring upstream
// trufflehog) to prevent SSRF against a caller's loopback.
var (
	defaultHTTPClientOnce sync.Once
	defaultHTTPClient     *http.Client
)

func getDefaultHTTPClient() *http.Client {
	defaultHTTPClientOnce.Do(func() {
		defaultHTTPClient = thdet.NewDetectorHttpClient(
			thdet.WithTimeout(5*time.Second),
			thdet.WithNoLocalIP(),
		)
	})
	return defaultHTTPClient
}

// Scanner implements detectors.Detector for one flavour of PostHog key.
// Construct via the New*Scanner factories; the zero value is not valid.
type Scanner struct {
	name        string
	prefix      string
	description string
	version     int
	regex       *regexp.Regexp
	verify      verifyFunc

	baseURLs []string
	client   *http.Client
}

// Versioner is the upstream trufflehog interface used by
// ahocorasick.CreateDetectorKey to disambiguate detectors that share a
// DetectorType. Each PostHog flavour returns a distinct version so all
// three can coexist in a single detector set.
const (
	versionPersonalAPIKey        = 1
	versionFeatureFlagSecureKey  = 2
	versionProjectAPIKey         = 3
)

type verifyFunc func(ctx context.Context, s *Scanner, key string, result *thdet.Result)

// Option configures a Scanner returned by one of the New*Scanner factories.
type Option func(*Scanner)

// WithBaseURLs overrides the PostHog hosts used for verification. The
// list is tried in order; callers typically use this for tests pointing
// at an httptest.Server.
func WithBaseURLs(urls []string) Option {
	return func(s *Scanner) {
		if len(urls) == 0 {
			return
		}
		s.baseURLs = append([]string(nil), urls...)
	}
}

// WithHTTPClient overrides the http.Client used for verification. The
// default client forbids local addresses; tests pointing at
// httptest.Server must supply a permissive client (e.g. http.DefaultClient).
func WithHTTPClient(client *http.Client) Option {
	return func(s *Scanner) {
		if client != nil {
			s.client = client
		}
	}
}

// keyRegex returns the bounded match pattern for a given PostHog key
// prefix. 32+ characters comfortably covers every issued length while
// letting the verifier reject shorter false positives.
func keyRegex(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`\b(` + regexp.QuoteMeta(prefix) + `[A-Za-z0-9_-]{32,})\b`)
}

// NewPersonalAPIKeyScanner returns a Scanner for phx_ personal API keys.
// Verification hits /api/users/@me/ on the app host; 200 marks the key live.
func NewPersonalAPIKeyScanner(opts ...Option) *Scanner {
	return buildScanner(
		"PostHogPersonalAPIKey",
		"phx_",
		"PostHog personal API key (grants access to the authenticated user's account).",
		versionPersonalAPIKey,
		verifyPersonalAPIKey,
		DefaultBaseURLs,
		opts,
	)
}

// NewFeatureFlagSecureKeyScanner returns a Scanner for phs_ feature-flag
// secure keys. Verification is a no-op — phs_ keys are server-side inputs
// to /flags with no public "is-this-valid" endpoint.
func NewFeatureFlagSecureKeyScanner(opts ...Option) *Scanner {
	return buildScanner(
		"PostHogFeatureFlagSecureKey",
		"phs_",
		"PostHog feature-flag secure key (server-side secret used with /flags).",
		versionFeatureFlagSecureKey,
		nil,
		DefaultBaseURLs,
		opts,
	)
}

// NewProjectAPIKeyScanner returns a Scanner for phc_ project capture keys.
// Verification posts to /flags/?v=2 on the ingestion host; these keys are
// public-facing so "verified" simply means PostHog recognises them.
func NewProjectAPIKeyScanner(opts ...Option) *Scanner {
	return buildScanner(
		"PostHogProjectAPIKey",
		"phc_",
		"PostHog project capture key (client-side token posted to /capture).",
		versionProjectAPIKey,
		verifyProjectAPIKey,
		DefaultProjectAPIBaseURLs,
		opts,
	)
}

func buildScanner(name, prefix, description string, version int, verify verifyFunc, defaultBaseURLs []string, opts []Option) *Scanner {
	s := &Scanner{
		name:        name,
		prefix:      prefix,
		description: description,
		version:     version,
		regex:       keyRegex(prefix),
		verify:      verify,
		baseURLs:    append([]string(nil), defaultBaseURLs...),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
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
// aho-corasick registry — without it, the three flavours collide on
// DetectorKey (same type, empty version, empty name) and only one
// survives. See ahocorasick.CreateDetectorKey in the upstream package.
func (s *Scanner) Version() int { return s.version }

// FromData scans data for the scanner's prefix, deduplicates within a
// single chunk, and optionally runs the flavour-specific verifier.
func (s *Scanner) FromData(ctx context.Context, verify bool, data []byte) ([]thdet.Result, error) {
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

		result := thdet.Result{
			DetectorType: s.Type(),
			DetectorName: s.name,
			Raw:          []byte(key),
			Redacted:     s.prefix + "****",
		}
		if verify && s.verify != nil {
			s.verify(ctx, s, key, &result)
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *Scanner) httpClient() *http.Client {
	if s.client != nil {
		return s.client
	}
	return getDefaultHTTPClient()
}

func verifyPersonalAPIKey(ctx context.Context, s *Scanner, key string, result *thdet.Result) {
	client := s.httpClient()
	var lastErr error
	for i, base := range s.baseURLs {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/users/@me/", nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Authorization", "Bearer "+key)
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		drainAndClose(res.Body)
		switch res.StatusCode {
		case http.StatusOK:
			result.Verified = true
			return
		case http.StatusUnauthorized:
			lastErr = nil
		default:
			lastErr = fmt.Errorf("posthog %s: unexpected status %d", base, res.StatusCode)
		}
		// Only try the next host if the current one returned 401 (key
		// may live in a different region). Any other status is final.
		if res.StatusCode != http.StatusUnauthorized && i == 0 {
			break
		}
	}
	if lastErr != nil {
		result.SetVerificationError(lastErr, key)
	}
}

func verifyProjectAPIKey(ctx context.Context, s *Scanner, key string, result *thdet.Result) {
	client := s.httpClient()
	var lastErr error
	for i, base := range s.baseURLs {
		body := strings.NewReader(fmt.Sprintf(`{"api_key":%q,"distinct_id":"trufflehog-verify"}`, key))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/flags/?v=2", body)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		payload, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		switch res.StatusCode {
		case http.StatusOK:
			if json.Valid(payload) {
				result.Verified = true
				return
			}
			lastErr = errors.New("posthog flags returned 200 with non-JSON body")
		case http.StatusUnauthorized:
			lastErr = nil
		default:
			lastErr = fmt.Errorf("posthog %s: unexpected status %d", base, res.StatusCode)
		}
		if res.StatusCode != http.StatusUnauthorized && i == 0 {
			break
		}
	}
	if lastErr != nil {
		result.SetVerificationError(lastErr, key)
	}
}

func drainAndClose(body io.ReadCloser) {
	_, _ = io.Copy(io.Discard, body)
	_ = body.Close()
}
