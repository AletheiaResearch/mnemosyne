package redact

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"

	"github.com/AletheiaResearch/mnemosyne/internal/redact/detectors/posthog"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

// Fixtures are assembled at runtime so the full provider-prefixed
// literal never appears in source — GitHub push protection otherwise
// flags them as leaked PostHog keys.
func fakeKey(prefix, body string) string { return prefix + body }

const (
	phxPrefix = "phx" + "_"
	phsPrefix = "phs" + "_"
	phcPrefix = "phc" + "_"

	personalBody = "abcdefghijklmnopqrstuvwxyz0123456789"
	flagBody     = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdef012345"
	projectBody  = "abcdefghijklmnopqrstuvwxyz01234567"
)

// TestPipelineRedactsWithoutVerification asserts that VerifySecrets=false
// keeps the pipeline offline — no HTTP calls — while every PostHog key
// is still replaced with the placeholder marker.
func TestPipelineRedactsWithoutVerification(t *testing.T) {
	t.Parallel()

	var httpHits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		httpHits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	scanners := posthogScanners(ts.URL)

	pipeline, err := New(Options{
		Detectors:     scanners,
		VerifySecrets: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	personal := fakeKey(phxPrefix, personalBody)
	flag := fakeKey(phsPrefix, flagBody)
	project := fakeKey(phcPrefix, projectBody)

	input := strings.Join([]string{
		"Authorization: Bearer " + personal,
		"secret=" + flag,
		`{"api_key":"` + project + `"}`,
	}, "\n")

	stats := &ApplyStats{}
	out := pipeline.applyText(input, stats)
	if stats.Redactions < 3 {
		t.Fatalf("expected at least 3 redactions, got %d: %q", stats.Redactions, out)
	}
	for _, key := range []string{personal, flag, project} {
		if strings.Contains(out, key) {
			t.Fatalf("key %q still present after redaction: %q", key, out)
		}
	}
	if !strings.Contains(out, PlaceholderMarker) {
		t.Fatalf("expected placeholder marker, got %q", out)
	}
	if httpHits.Load() != 0 {
		t.Fatalf("no-verify mode must not touch the network, got %d hits", httpHits.Load())
	}
}

// TestPipelineRedactsWithVerification drives the verify path end-to-end
// against an httptest mock. Both the personal and project key return 200
// so the pipeline should redact them AND report them as verified
// secrets; the feature-flag key has no verifier and must come back
// unverified.
func TestPipelineRedactsWithVerification(t *testing.T) {
	t.Parallel()

	personal := fakeKey(phxPrefix, personalBody)
	flag := fakeKey(phsPrefix, flagBody)
	project := fakeKey(phcPrefix, projectBody)

	var personalHits, projectHits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/users/@me/":
			personalHits.Add(1)
			if r.Header.Get("Authorization") != "Bearer "+personal {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"email":"ok@example.com"}`)
		case "/decide/":
			projectHits.Add(1)
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), project) {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"featureFlags":{}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()

	scanners := posthogScanners(ts.URL)

	pipeline, err := New(Options{
		Detectors:     scanners,
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	newRecord := func() schema.Record {
		return schema.Record{
			Grouping: "test/verification",
			Turns: []schema.Turn{
				{Text: "personal=" + personal + " flag=" + flag + " project=" + project},
			},
		}
	}

	// ApplyRecord mutates the Turns backing array, so scan a fresh copy
	// before redacting the test fixture.
	findings := pipeline.ScanRecord(newRecord())

	redacted, applyStats := pipeline.ApplyRecord(newRecord())
	if applyStats.Redactions < 3 {
		t.Fatalf("expected at least 3 redactions, got %d", applyStats.Redactions)
	}
	if applyStats.VerifiedSecretCount != 2 {
		t.Fatalf("expected ApplyRecord to surface 2 verified secrets (personal+project), got %d (%v)",
			applyStats.VerifiedSecretCount, applyStats.VerifiedSecrets)
	}
	if !containsWith(applyStats.VerifiedSecrets, "PostHogPersonalAPIKey") ||
		!containsWith(applyStats.VerifiedSecrets, "PostHogProjectAPIKey") {
		t.Fatalf("ApplyRecord verified secrets should list personal+project detectors, got %v",
			applyStats.VerifiedSecrets)
	}
	turnText := redacted.Turns[0].Text
	for _, key := range []string{personal, flag, project} {
		if strings.Contains(turnText, key) {
			t.Fatalf("key %q still present after redaction: %q", key, turnText)
		}
	}
	if findings.TokenCount < 3 {
		t.Fatalf("expected TokenCount>=3, got %d", findings.TokenCount)
	}
	if findings.VerifiedSecretCount != 2 {
		t.Fatalf("expected 2 verified secrets (personal+project), got %d (%v)",
			findings.VerifiedSecretCount, findings.VerifiedSecrets)
	}
	if !containsWith(findings.VerifiedSecrets, "PostHogPersonalAPIKey") ||
		!containsWith(findings.VerifiedSecrets, "PostHogProjectAPIKey") {
		t.Fatalf("verified secrets should list both personal and project detectors, got %v",
			findings.VerifiedSecrets)
	}
	if containsWith(findings.VerifiedSecrets, "PostHogFeatureFlagSecureKey") {
		t.Fatalf("phs key has no verifier, must not appear in verified secrets: %v",
			findings.VerifiedSecrets)
	}

	// ScanRecord invoked the verifier a second time, so two hits per
	// endpoint are expected; the important thing is that the verify
	// path was exercised at all.
	if personalHits.Load() == 0 {
		t.Fatalf("expected /api/users/@me/ to be queried")
	}
	if projectHits.Load() == 0 {
		t.Fatalf("expected /decide/ to be queried")
	}
}

// TestPipelineVerifyReportsUnverifiedKeysAsTokens checks the
// pipeline's Scan path for keys the mock rejects with 401 — they should
// land in Findings.Tokens but not in VerifiedSecrets.
func TestPipelineVerifyReportsUnverifiedKeysAsTokens(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	pipeline, err := New(Options{
		Detectors:     posthogScanners(ts.URL),
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	personal := fakeKey(phxPrefix, personalBody)
	findings := pipeline.ScanRecord(schema.Record{
		Turns: []schema.Turn{{Text: "bearer " + personal}},
	})
	if findings.TokenCount == 0 {
		t.Fatalf("expected the key to be counted as a token, findings=%+v", findings)
	}
	if findings.VerifiedSecretCount != 0 {
		t.Fatalf("401 from verifier must not mark the key verified, got %d", findings.VerifiedSecretCount)
	}
}

// TestScanRecordDedupsTokensAcrossRegexAndTrufflehog guards against
// double-counting when the regex detector and a trufflehog scanner both
// flag the same raw secret (e.g. a phx_ key inside a bearer header
// matches both the regex "bearer" pattern and the PostHog scanner).
func TestScanRecordDedupsTokensAcrossRegexAndTrufflehog(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	pipeline, err := New(Options{
		Detectors:     posthogScanners(ts.URL),
		VerifySecrets: false,
	})
	if err != nil {
		t.Fatal(err)
	}

	personal := fakeKey(phxPrefix, personalBody)
	findings := pipeline.ScanRecord(schema.Record{
		Turns: []schema.Turn{{Text: "Authorization: Bearer " + personal}},
	})

	if findings.TokenCount != 1 {
		t.Fatalf("expected TokenCount=1 for a single secret hit by both detectors, got %d (tokens=%v)",
			findings.TokenCount, findings.Tokens)
	}
}

// TestApplyRecordSurfacesVerifiedSecrets verifies that the metadata
// previously only visible through ScanRecord is now returned from
// ApplyRecord too, so the extract CLI path can surface verified-secret
// counts without a second scan.
func TestApplyRecordSurfacesVerifiedSecrets(t *testing.T) {
	t.Parallel()

	personal := fakeKey(phxPrefix, personalBody)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/users/@me/" && r.Header.Get("Authorization") == "Bearer "+personal {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"email":"ok@example.com"}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer ts.Close()

	pipeline, err := New(Options{
		Detectors:     posthogScanners(ts.URL),
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	record := schema.Record{
		Turns: []schema.Turn{{Text: "personal=" + personal}},
	}
	redacted, stats := pipeline.ApplyRecord(record)
	if strings.Contains(redacted.Turns[0].Text, personal) {
		t.Fatalf("expected key to be redacted, got %q", redacted.Turns[0].Text)
	}
	if stats.Redactions == 0 {
		t.Fatalf("expected at least one redaction")
	}
	if stats.VerifiedSecretCount != 1 {
		t.Fatalf("expected 1 verified secret, got %d (%v)",
			stats.VerifiedSecretCount, stats.VerifiedSecrets)
	}
	if !containsWith(stats.VerifiedSecrets, "PostHogPersonalAPIKey") {
		t.Fatalf("verified secret should carry the detector label, got %v",
			stats.VerifiedSecrets)
	}
}

// posthogScanners builds the PostHog scanner set pointed at a single
// httptest URL. Tests use this to exercise the pipeline without
// depending on the init-time registry (so the upstream default
// detectors — 800+ of them — never come into play and the test stays
// deterministic).
func posthogScanners(baseURL string) []thdet.Detector {
	opts := []posthog.Option{
		posthog.WithBaseURLs([]string{baseURL}),
		posthog.WithHTTPClient(http.DefaultClient),
	}
	return []thdet.Detector{
		posthog.NewPersonalAPIKeyScanner(opts...),
		posthog.NewFeatureFlagSecureKeyScanner(opts...),
		posthog.NewProjectAPIKeyScanner(opts...),
	}
}

func containsWith(items []string, needle string) bool {
	for _, item := range items {
		if strings.Contains(item, needle) {
			return true
		}
	}
	return false
}
