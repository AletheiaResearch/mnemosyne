package redact

import (
	"context"
	"fmt"
	"strings"
	"testing"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"

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

// posthogScanners builds the PostHog scanner set without pulling in the
// upstream default detectors — with 800+ scanners registered by default,
// test determinism benefits from an explicit slice.
func posthogScanners() []thdet.Detector {
	return []thdet.Detector{
		posthog.NewPersonalAPIKeyScanner(),
		posthog.NewFeatureFlagSecureKeyScanner(),
		posthog.NewProjectAPIKeyScanner(),
	}
}

// TestPipelineCustomRedactionsLongestLiteralFirst guards the overlap
// regression reported on PR thread PRRT_kwDOSFNLc857-Z7u: when two
// custom literals overlap (e.g. abc and abcdef), a lexicographic sort
// replaces the shorter one first and leaves the suffix `def` of the
// longer secret exposed in the output.
//
// Inputs are plain alphabetic tokens without any token/secret/bearer
// shape so the regex detector can't independently redact them and mask
// a regression in the literal loop.
func TestPipelineCustomRedactionsLongestLiteralFirst(t *testing.T) {
	t.Parallel()

	pipeline, err := New(Options{
		CustomRedactions: []string{"abc", "abcdef"},
	})
	if err != nil {
		t.Fatal(err)
	}

	input := "plain marker abcdef end\nalso plain abc here"
	out := pipeline.applyText(input, &ApplyStats{})

	if strings.Contains(out, "abcdef") {
		t.Fatalf("longer literal leaked: %q", out)
	}
	if strings.Contains(out, "def") {
		t.Fatalf("suffix of longer literal leaked after shorter literal ran first: %q", out)
	}
	if strings.Contains(out, "abc") {
		t.Fatalf("shorter literal still visible: %q", out)
	}
	if strings.Count(out, PlaceholderMarker) != 2 {
		t.Fatalf("expected exactly two placeholder markers, got %q", out)
	}
}

// TestPipelineRedactsPostHogKeys asserts every PostHog key variant gets
// replaced by the placeholder marker. Detection is regex-only; the
// pipeline never issues network calls for PostHog matches.
func TestPipelineRedactsPostHogKeys(t *testing.T) {
	t.Parallel()

	pipeline, err := New(Options{Detectors: posthogScanners()})
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
}

// TestScanRecordDedupsTokensAcrossRegexAndTrufflehog guards against
// double-counting when the regex detector and a trufflehog scanner both
// flag the same raw secret (e.g. a phx_ key inside a bearer header
// matches both the regex "bearer" pattern and the PostHog scanner).
func TestScanRecordDedupsTokensAcrossRegexAndTrufflehog(t *testing.T) {
	t.Parallel()

	pipeline, err := New(Options{Detectors: posthogScanners()})
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

// TestApplyRecordSurfacesVerifiedSecrets verifies that ApplyRecord
// propagates verified-secret metadata from trufflehog detectors, so the
// extract CLI path can surface verified counts without a second scan.
func TestApplyRecordSurfacesVerifiedSecrets(t *testing.T) {
	t.Parallel()

	const (
		id     = "ABIAS9L8MS5IPHTZPPUQ"
		secret = "v2QPKHl7LcdVYsjaR4LgQiZ1zw3MAnMyiondXC63"
	)
	det := &multipartDetector{
		keyword:  "ABIA",
		verified: true,
		emit: []multipartResult{
			{raw: id, rawV2: id + ":" + secret},
		},
	}
	pipeline, err := New(Options{
		Detectors:     []thdet.Detector{det},
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	redacted, stats := pipeline.ApplyRecord(schema.Record{
		Turns: []schema.Turn{{Text: "cred=" + id + ":" + secret}},
	})
	if strings.Contains(redacted.Turns[0].Text, secret) {
		t.Fatalf("expected secret to be redacted, got %q", redacted.Turns[0].Text)
	}
	if stats.Redactions == 0 {
		t.Fatalf("expected at least one redaction")
	}
	if stats.VerifiedSecretCount != 1 {
		t.Fatalf("expected 1 verified secret, got %d (%v)",
			stats.VerifiedSecretCount, stats.VerifiedSecrets)
	}
	if !containsWith(stats.VerifiedLabels(), "MultipartTest") {
		t.Fatalf("verified secret should carry the detector label, got %v",
			stats.VerifiedLabels())
	}
}

// TestPipelineVerifyReportsUnverifiedKeysAsTokens confirms detectors
// that return verified=false still surface their hits as tokens but
// never populate VerifiedSecrets.
func TestPipelineVerifyReportsUnverifiedKeysAsTokens(t *testing.T) {
	t.Parallel()

	const (
		id     = "ABIAS9L8MS5IPHTZPPUQ"
		secret = "v2QPKHl7LcdVYsjaR4LgQiZ1zw3MAnMyiondXC63"
	)
	det := &multipartDetector{
		keyword: "ABIA",
		emit: []multipartResult{
			{raw: id, rawV2: id + ":" + secret},
		},
	}
	pipeline, err := New(Options{
		Detectors:     []thdet.Detector{det},
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	findings := pipeline.ScanRecord(schema.Record{
		Turns: []schema.Turn{{Text: "cred=" + id + ":" + secret}},
	})
	if findings.TokenCount == 0 {
		t.Fatalf("expected unverified key to be counted as a token, findings=%+v", findings)
	}
	if findings.VerifiedSecretCount != 0 {
		t.Fatalf("unverified detector must not populate VerifiedSecrets, got %d", findings.VerifiedSecretCount)
	}
}

// TestApplyRecordVerifiedSecretsUncapped ensures every verified
// fingerprint reaches ApplyStats.VerifiedSecrets even when a single
// record contains more than the 20-entry display cap previously applied
// here. Capping inside the accumulator would make cross-record
// aggregators (extract) both undercount the total and double-count any
// fingerprint that got dropped and later reappeared.
func TestApplyRecordVerifiedSecretsUncapped(t *testing.T) {
	t.Parallel()

	const keys = 25
	emits := make([]multipartResult, keys)
	var builder strings.Builder
	for i := range emits {
		id := fmt.Sprintf("ABIAS9L8MS5IPHTZP%03d", i)
		secret := fmt.Sprintf("secret%034d", i)
		emits[i] = multipartResult{raw: id, rawV2: id + ":" + secret}
		if i > 0 {
			builder.WriteString(" ")
		}
		builder.WriteString(fmt.Sprintf("k%d=", i))
		builder.WriteString(id + ":" + secret)
	}
	det := &multipartDetector{
		keyword:  "ABIA",
		verified: true,
		emit:     emits,
	}
	pipeline, err := New(Options{
		Detectors:     []thdet.Detector{det},
		VerifySecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, stats := pipeline.ApplyRecord(schema.Record{
		Turns: []schema.Turn{{Text: builder.String()}},
	})
	if stats.VerifiedSecretCount != keys {
		t.Fatalf("expected VerifiedSecretCount=%d, got %d", keys, stats.VerifiedSecretCount)
	}
	if len(stats.VerifiedSecrets) != keys {
		t.Fatalf("expected %d VerifiedSecrets entries (uncapped), got %d",
			keys, len(stats.VerifiedSecrets))
	}
	seen := make(map[string]struct{}, keys)
	for _, vs := range stats.VerifiedSecrets {
		seen[vs.Fingerprint] = struct{}{}
	}
	if len(seen) != keys {
		t.Fatalf("expected %d distinct fingerprints, got %d", keys, len(seen))
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

// multipartDetector is a minimal stand-in for a trufflehog detector
// that emits multipart credentials (e.g. AWS "id:secret"): Raw is the
// identifier, RawV2 is the full token. It returns one Result per entry
// in `emit`, using the configured verified flag.
type multipartDetector struct {
	keyword  string
	emit     []multipartResult
	verified bool
}

type multipartResult struct {
	raw   string
	rawV2 string
}

func (d *multipartDetector) Keywords() []string { return []string{d.keyword} }
func (d *multipartDetector) Type() detector_typepb.DetectorType {
	return detector_typepb.DetectorType_CustomRegex
}
func (d *multipartDetector) Description() string { return "multipart test detector" }
func (d *multipartDetector) FromData(_ context.Context, _ bool, _ []byte) ([]thdet.Result, error) {
	out := make([]thdet.Result, 0, len(d.emit))
	for _, e := range d.emit {
		out = append(out, thdet.Result{
			DetectorType: detector_typepb.DetectorType_CustomRegex,
			DetectorName: "MultipartTest",
			Raw:          []byte(e.raw),
			RawV2:        []byte(e.rawV2),
			Redacted:     "multipart****",
			Verified:     d.verified,
		})
	}
	return out, nil
}

// TestTrufflehogRunnerRedactsMultipartSecretFully guards the reviewer's
// AWS scenario: when a detector emits RawV2 with the full "id:secret"
// token, the combined string must be stripped — not just the Raw
// identifier half that would otherwise leave the secret material in the
// output.
func TestTrufflehogRunnerRedactsMultipartSecretFully(t *testing.T) {
	t.Parallel()

	const (
		id     = "ABIAS9L8MS5IPHTZPPUQ"
		secret = "v2QPKHl7LcdVYsjaR4LgQiZ1zw3MAnMyiondXC63"
	)
	det := &multipartDetector{
		keyword: "ABIA",
		emit: []multipartResult{
			{raw: id, rawV2: id + ":" + secret},
		},
	}
	pipeline, err := New(Options{Detectors: []thdet.Detector{det}})
	if err != nil {
		t.Fatal(err)
	}

	input := "cred=" + id + ":" + secret
	out := pipeline.applyText(input, &ApplyStats{})
	if strings.Contains(out, secret) {
		t.Fatalf("secret half leaked after redaction: %q", out)
	}
	if strings.Contains(out, id) {
		t.Fatalf("identifier half leaked after redaction: %q", out)
	}
	if !strings.Contains(out, PlaceholderMarker) {
		t.Fatalf("expected placeholder marker in output, got %q", out)
	}
}

// TestTrufflehogRunnerRedactsMultipartSecretSplitAcrossLines covers the
// case where the identifier and secret appear on separate lines, so the
// combined RawV2 substring isn't present. The runner must scrub both
// halves — previously only Raw was replaced and the secret leaked.
// Uses neutral "cred"/"material" labels so the regex pass (env_assign,
// generic_assign) doesn't independently redact the secret and mask a
// regression in the trufflehog fallback.
func TestTrufflehogRunnerRedactsMultipartSecretSplitAcrossLines(t *testing.T) {
	t.Parallel()

	const (
		id     = "ABIAS9L8MS5IPHTZPPUQ"
		secret = "v2QPKHl7LcdVYsjaR4LgQiZ1zw3MAnMyiondXC63"
	)
	det := &multipartDetector{
		keyword: "ABIA",
		emit: []multipartResult{
			{raw: id, rawV2: id + ":" + secret},
		},
	}
	pipeline, err := New(Options{Detectors: []thdet.Detector{det}})
	if err != nil {
		t.Fatal(err)
	}

	input := "cred: " + id + "\nmaterial: " + secret
	out := pipeline.applyText(input, &ApplyStats{})
	if strings.Contains(out, id) {
		t.Fatalf("identifier still present when RawV2 isn't contiguous: %q", out)
	}
	if strings.Contains(out, secret) {
		t.Fatalf("secret still present when RawV2 isn't contiguous: %q", out)
	}
	if !strings.Contains(out, PlaceholderMarker) {
		t.Fatalf("expected placeholder marker, got %q", out)
	}
}

// TestScanRecordDedupsMultipartAcrossRegexAndTrufflehog guards the
// cross-detector TokenCount dedup for multipart credentials: an Algolia
// app-id + API-key pair caught by both the regex pass (on the api_key=
// assignment) and the multipart trufflehog detector (emitting
// Raw=api_key, RawV2=id:api_key) must still produce TokenCount=1.
// Keying dedup on RawV2 would miss the overlap and double-count.
func TestScanRecordDedupsMultipartAcrossRegexAndTrufflehog(t *testing.T) {
	t.Parallel()

	const (
		appID  = "AA11BB22CC"
		apiKey = "0123456789abcdef0123456789abcdef"
	)
	det := &multipartDetector{
		keyword: "AA11",
		emit: []multipartResult{
			{raw: apiKey, rawV2: appID + ":" + apiKey},
		},
	}
	pipeline, err := New(Options{Detectors: []thdet.Detector{det}})
	if err != nil {
		t.Fatal(err)
	}

	findings := pipeline.ScanRecord(schema.Record{
		Turns: []schema.Turn{{Text: "api_key=" + apiKey + " app_id=" + appID}},
	})
	if findings.TokenCount != 1 {
		t.Fatalf("expected TokenCount=1 for a single multipart credential flagged by both passes, got %d (tokens=%v)",
			findings.TokenCount, findings.Tokens)
	}
}

// TestTrufflehogRunnerMultipartDedupUsesRawV2 asserts that two verified
// multipart credentials sharing a Raw identifier but differing in RawV2
// are counted and fingerprinted as distinct — the old Raw-only dedup
// would have collapsed them into one.
func TestTrufflehogRunnerMultipartDedupUsesRawV2(t *testing.T) {
	t.Parallel()

	const (
		id      = "ABIAS9L8MS5IPHTZPPUQ"
		secretA = "v2QPKHl7LcdVYsjaR4LgQiZ1zw3MAnMyiondXC63"
		secretB = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	)
	det := &multipartDetector{
		keyword:  "ABIA",
		verified: true,
		emit: []multipartResult{
			{raw: id, rawV2: id + ":" + secretA},
			{raw: id, rawV2: id + ":" + secretB},
		},
	}
	pipeline, err := New(Options{Detectors: []thdet.Detector{det}, VerifySecrets: true})
	if err != nil {
		t.Fatal(err)
	}

	record := schema.Record{
		Turns: []schema.Turn{{Text: "a=" + id + ":" + secretA + " b=" + id + ":" + secretB}},
	}
	_, stats := pipeline.ApplyRecord(record)
	if stats.VerifiedSecretCount != 2 {
		t.Fatalf("expected 2 distinct verified multipart secrets, got %d (%v)",
			stats.VerifiedSecretCount, stats.VerifiedLabels())
	}
	if stats.VerifiedSecrets[0].Fingerprint == stats.VerifiedSecrets[1].Fingerprint {
		t.Fatalf("distinct multipart secrets must yield distinct fingerprints, got %q twice",
			stats.VerifiedSecrets[0].Fingerprint)
	}
}
