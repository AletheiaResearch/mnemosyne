package redact

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

	"github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

// posthogBaseURLs holds the PostHog host roots queried for verification.
// Exported as package variables so tests can redirect traffic to a
// httptest.Server. Order matters: the first entry is tried first and the
// remaining entries are fallbacks when the primary responds with 401
// (the key could belong to a different region).
var posthogBaseURLs = []string{
	"https://us.posthog.com",
	"https://eu.posthog.com",
}

// posthogHTTPClient is built lazily the first time a detector needs to
// verify a key so that the regex-only path pays no startup cost. The
// client forbids local addresses (mirroring upstream trufflehog) to
// prevent SSRF against a caller's loopback.
var (
	posthogHTTPClientOnce sync.Once
	posthogHTTPClient     *http.Client
)

func getPosthogHTTPClient() *http.Client {
	posthogHTTPClientOnce.Do(func() {
		posthogHTTPClient = detectors.NewDetectorHttpClient(
			detectors.WithTimeout(5*time.Second),
			detectors.WithNoLocalIP(),
		)
	})
	return posthogHTTPClient
}

// posthogKeyRegex matches keys of the form <prefix><base64url chars>. The
// 32+ lower bound matches PostHog-issued keys while letting the verifier
// reject anything shorter.
func posthogKeyRegex(prefix string) *regexp.Regexp {
	return regexp.MustCompile(`\b(` + regexp.QuoteMeta(prefix) + `[A-Za-z0-9_-]{32,})\b`)
}

// postHogDetector is the shared implementation backing all three PostHog
// key detectors. The verify callback is responsible for contacting the
// PostHog API and, on success, marking the result as verified.
type postHogDetector struct {
	name        string
	prefix      string
	description string
	regex       *regexp.Regexp
	verify      func(ctx context.Context, key string, result *detectors.Result)
}

var _ detectors.Detector = (*postHogDetector)(nil)

func (d *postHogDetector) Keywords() []string {
	return []string{d.prefix}
}

func (d *postHogDetector) Type() detector_typepb.DetectorType {
	// Upstream trufflehog has no detector type for PostHog feature-flag
	// or project keys, so we reuse CustomRegex and differentiate via
	// DetectorName on the Result.
	return detector_typepb.DetectorType_CustomRegex
}

func (d *postHogDetector) Description() string {
	return d.description
}

func (d *postHogDetector) FromData(ctx context.Context, verify bool, data []byte) ([]detectors.Result, error) {
	if len(data) == 0 {
		return nil, nil
	}
	matches := d.regex.FindAllSubmatch(data, -1)
	if len(matches) == 0 {
		return nil, nil
	}

	seen := make(map[string]struct{}, len(matches))
	results := make([]detectors.Result, 0, len(matches))
	for _, match := range matches {
		key := string(match[1])
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		result := detectors.Result{
			DetectorType: d.Type(),
			DetectorName: d.name,
			Raw:          []byte(key),
			Redacted:     d.prefix + "****",
		}
		if verify && d.verify != nil {
			d.verify(ctx, key, &result)
		}
		results = append(results, result)
	}
	return results, nil
}

// verifyPostHogPersonalAPIKey validates phx_ personal API keys by hitting
// /api/users/@me/ with a bearer token. 200 marks the key verified, 401
// means the key is unknown, anything else is surfaced as a verification
// error. The EU host is tried only when US returns 401.
func verifyPostHogPersonalAPIKey(ctx context.Context, key string, result *detectors.Result) {
	client := getPosthogHTTPClient()
	var lastErr error
	for i, base := range posthogBaseURLs {
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
			// try next region
			lastErr = nil
		default:
			lastErr = fmt.Errorf("posthog %s: unexpected status %d", base, res.StatusCode)
		}
		// Don't try the EU fallback on non-401 responses from the first host.
		if res.StatusCode != http.StatusUnauthorized && i == 0 {
			break
		}
	}
	if lastErr != nil {
		result.SetVerificationError(lastErr, key)
	}
}

// verifyPostHogFeatureFlagSecureKey cannot be verified against a stable
// public endpoint (phs_ keys are server-side inputs to /decide and
// PostHog does not expose a "check this key" endpoint), so this is a
// best-effort no-op that leaves Verified=false without signalling an
// error.
func verifyPostHogFeatureFlagSecureKey(_ context.Context, _ string, _ *detectors.Result) {
	// phs_ keys have no public verification surface — limitation noted.
}

// verifyPostHogProjectAPIKey validates phc_ project capture keys by
// posting to /decide/?v=3. These keys are public so "verified" simply
// means PostHog recognises them.
func verifyPostHogProjectAPIKey(ctx context.Context, key string, result *detectors.Result) {
	client := getPosthogHTTPClient()
	var lastErr error
	for i, base := range posthogBaseURLs {
		body := strings.NewReader(fmt.Sprintf(`{"api_key":%q,"distinct_id":"trufflehog-verify"}`, key))
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/decide/?v=3", body)
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
			lastErr = errors.New("posthog decide returned 200 with non-JSON body")
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

// PostHogDetectors returns the list of detectors for the three PostHog
// key variants. Each call returns fresh detector instances; callers
// should cache the slice if they plan to invoke it frequently.
func PostHogDetectors() []detectors.Detector {
	return []detectors.Detector{
		&postHogDetector{
			name:        "PostHogPersonalAPIKey",
			prefix:      "phx_",
			description: "PostHog personal API key (grants access to the authenticated user's account).",
			regex:       posthogKeyRegex("phx_"),
			verify:      verifyPostHogPersonalAPIKey,
		},
		&postHogDetector{
			name:        "PostHogFeatureFlagSecureKey",
			prefix:      "phs_",
			description: "PostHog feature-flag secure key (server-side secret used with /decide).",
			regex:       posthogKeyRegex("phs_"),
			verify:      verifyPostHogFeatureFlagSecureKey,
		},
		&postHogDetector{
			name:        "PostHogProjectAPIKey",
			prefix:      "phc_",
			description: "PostHog project capture key (client-side token posted to /capture).",
			regex:       posthogKeyRegex("phc_"),
			verify:      verifyPostHogProjectAPIKey,
		},
	}
}
