package posthog

import (
	"context"
	"strings"
	"testing"
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

func TestRegexMatches(t *testing.T) {
	t.Parallel()

	personalKey := fakeKey(phxPrefix, personalBody)
	flagKey := fakeKey(phsPrefix, flagBody)
	projectKey := fakeKey(phcPrefix, projectBody)

	cases := []struct {
		name    string
		scanner *Scanner
		input   string
		want    []string
		noMatch bool
	}{
		{
			name:    "personal key in bearer header",
			scanner: NewPersonalAPIKeyScanner(),
			input:   "Authorization: Bearer " + personalKey,
			want:    []string{personalKey},
		},
		{
			name:    "feature-flag key amid prose",
			scanner: NewFeatureFlagSecureKeyScanner(),
			input:   "secret=" + flagKey + " other=junk",
			want:    []string{flagKey},
		},
		{
			name:    "project key in JSON body",
			scanner: NewProjectAPIKeyScanner(),
			input:   `{"api_key":"` + projectKey + `","distinct_id":"u"}`,
			want:    []string{projectKey},
		},
		{
			name:    "too short to count",
			scanner: NewPersonalAPIKeyScanner(),
			input:   phxPrefix + "short",
			noMatch: true,
		},
		{
			name:    "wrong prefix is ignored",
			scanner: NewPersonalAPIKeyScanner(),
			input:   "phy" + "_" + personalBody,
			noMatch: true,
		},
		{
			name:    "dedup within a single chunk",
			scanner: NewPersonalAPIKeyScanner(),
			input:   "one: " + personalKey + "\ntwo: " + personalKey,
			want:    []string{personalKey},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results, err := tc.scanner.FromData(context.Background(), false, []byte(tc.input))
			if err != nil {
				t.Fatalf("FromData: %v", err)
			}
			if tc.noMatch {
				if len(results) != 0 {
					t.Fatalf("expected no matches, got %d: %v", len(results), results)
				}
				return
			}
			got := make([]string, 0, len(results))
			for _, r := range results {
				got = append(got, string(r.Raw))
			}
			if !equalStrings(got, tc.want) {
				t.Fatalf("matches mismatch: got %v want %v", got, tc.want)
			}
			for _, r := range results {
				if !strings.HasPrefix(r.Redacted, tc.scanner.Prefix()) {
					t.Fatalf("redacted %q missing prefix %q", r.Redacted, tc.scanner.Prefix())
				}
				if r.Verified {
					t.Fatalf("regex-only run should not mark verified")
				}
			}
		})
	}
}

// TestFromDataIgnoresVerifyArg confirms the detector never marks a
// match verified — even when trufflehog passes verify=true, the
// pipeline intentionally keeps PostHog detection regex-only.
func TestFromDataIgnoresVerifyArg(t *testing.T) {
	t.Parallel()

	scanner := NewPersonalAPIKeyScanner()
	key := fakeKey(phxPrefix, personalBody)
	results, err := scanner.FromData(context.Background(), true, []byte(key))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Verified {
		t.Fatalf("verify=true must not mark PostHog keys verified (regex-only)")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
