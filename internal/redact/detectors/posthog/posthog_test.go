package posthog

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	unknownBody  = "ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ"
	flagLongBody = "abcdefghijklmnopqrstuvwxyz012345678901"
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

func TestPersonalAPIKeyVerification(t *testing.T) {
	t.Parallel()

	good := fakeKey(phxPrefix, personalBody)

	var usCalls, euCalls atomic.Int32
	us := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		usCalls.Add(1)
		if r.URL.Path != "/api/users/@me/" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+good {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"email":"test@example.com"}`)
	}))
	defer us.Close()
	eu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		euCalls.Add(1)
		// The EU fixture always 401s so the unknown-key case exhausts
		// both regions and the verified case stops at US.
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer eu.Close()

	scanner := NewPersonalAPIKeyScanner(
		WithBaseURLs([]string{us.URL, eu.URL}),
		WithHTTPClient(http.DefaultClient),
	)

	// Verified: US returns 200, EU should not be queried.
	results, err := scanner.FromData(context.Background(), true, []byte(good))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 || !results[0].Verified {
		t.Fatalf("expected verified=true, got %+v", results)
	}
	if err := results[0].VerificationError(); err != nil {
		t.Fatalf("unexpected verification error: %v", err)
	}
	if usCalls.Load() != 1 || euCalls.Load() != 0 {
		t.Fatalf("verified case should stop at US, got us=%d eu=%d", usCalls.Load(), euCalls.Load())
	}

	// Unknown: US 401 → EU 401 → verified=false, no error surfaced.
	bad := fakeKey(phxPrefix, unknownBody)
	results, err = scanner.FromData(context.Background(), true, []byte(bad))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 || results[0].Verified {
		t.Fatalf("expected verified=false for unknown key, got %+v", results)
	}
	if err := results[0].VerificationError(); err != nil {
		t.Fatalf("401 should not be a verification error, got %v", err)
	}
	if usCalls.Load() != 2 || euCalls.Load() != 1 {
		t.Fatalf("unknown case should query both regions, got us=%d eu=%d", usCalls.Load(), euCalls.Load())
	}
}

func TestProjectAPIKeyVerification(t *testing.T) {
	t.Parallel()

	good := fakeKey(phcPrefix, projectBody)

	var mu sync.Mutex
	var bodies []string
	us := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/flags/" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		if !strings.Contains(string(body), good) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"featureFlags":{}}`)
	}))
	defer us.Close()

	scanner := NewProjectAPIKeyScanner(
		WithBaseURLs([]string{us.URL}),
		WithHTTPClient(http.DefaultClient),
	)

	results, err := scanner.FromData(context.Background(), true, []byte(good))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 || !results[0].Verified {
		t.Fatalf("expected verified=true, got %+v", results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 || !strings.Contains(bodies[0], good) {
		t.Fatalf("expected /flags body to embed key, got %v", bodies)
	}
}

func TestFeatureFlagKeyHasNoVerification(t *testing.T) {
	t.Parallel()

	scanner := NewFeatureFlagSecureKeyScanner()
	key := fakeKey(phsPrefix, flagLongBody)
	results, err := scanner.FromData(context.Background(), true, []byte(key))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Verified {
		t.Fatalf("phs keys have no verifier, should not be marked verified")
	}
	if err := results[0].VerificationError(); err != nil {
		t.Fatalf("phs skip should not set a verification error, got %v", err)
	}
}

func TestWithBaseURLsIgnoresEmpty(t *testing.T) {
	t.Parallel()

	scanner := NewPersonalAPIKeyScanner(WithBaseURLs(nil))
	if len(scanner.baseURLs) == 0 {
		t.Fatalf("expected default base URLs to be preserved when override is empty")
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
