package redact

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
)

// Test fixtures are assembled at runtime via fakeKey so the full
// provider-prefixed literal never appears in source — GitHub's push
// protection otherwise flags them as leaked PostHog keys.
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

func TestPostHogDetectorsRegexMatches(t *testing.T) {
	t.Parallel()

	type matchCase struct {
		name    string
		prefix  string
		input   string
		want    []string
		noMatch bool
	}

	personalKey := fakeKey(phxPrefix, personalBody)
	flagKey := fakeKey(phsPrefix, flagBody)
	projectKey := fakeKey(phcPrefix, projectBody)

	cases := []matchCase{
		{
			name:   "personal key in bearer header",
			prefix: phxPrefix,
			input:  "Authorization: Bearer " + personalKey,
			want:   []string{personalKey},
		},
		{
			name:   "feature-flag key amid prose",
			prefix: phsPrefix,
			input:  "secret=" + flagKey + " other=junk",
			want:   []string{flagKey},
		},
		{
			name:   "project key in JSON body",
			prefix: phcPrefix,
			input:  `{"api_key":"` + projectKey + `","distinct_id":"u"}`,
			want:   []string{projectKey},
		},
		{
			name:    "too short to count",
			prefix:  phxPrefix,
			input:   phxPrefix + "short",
			noMatch: true,
		},
		{
			name:    "wrong prefix is ignored",
			prefix:  phxPrefix,
			input:   "phy" + "_" + personalBody,
			noMatch: true,
		},
		{
			name:   "dedup within a single chunk",
			prefix: phxPrefix,
			input:  "one: " + personalKey + "\ntwo: " + personalKey,
			want:   []string{personalKey},
		},
	}

	byPrefix := map[string]detectors.Detector{}
	for _, d := range PostHogDetectors() {
		byPrefix[d.Keywords()[0]] = d
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := byPrefix[tc.prefix]
			if d == nil {
				t.Fatalf("no detector for prefix %q", tc.prefix)
			}
			results, err := d.FromData(context.Background(), false, []byte(tc.input))
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
				if !strings.HasPrefix(r.Redacted, tc.prefix) {
					t.Fatalf("redacted %q missing prefix %q", r.Redacted, tc.prefix)
				}
				if r.Verified {
					t.Fatalf("regex-only run should not mark verified")
				}
			}
		})
	}
}

func TestPostHogPersonalAPIKeyVerification(t *testing.T) {
	good := fakeKey(phxPrefix, personalBody)

	mux := http.NewServeMux()
	var usCalls, euCalls int
	var mu sync.Mutex
	mux.HandleFunc("/api/users/@me/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		region := r.Header.Get("X-Region")
		if region == "eu" {
			euCalls++
		} else {
			usCalls++
		}
		if r.Header.Get("Authorization") != "Bearer "+good {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"email":"test@example.com"}`)
	})
	us := httptest.NewServer(mux)
	defer us.Close()
	eu := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set("X-Region", "eu")
		mux.ServeHTTP(w, r)
	}))
	defer eu.Close()

	withPostHogBaseURLs(t, []string{us.URL, eu.URL}, func() {
		// Verified case (US returns 200).
		d := findDetector(t, phxPrefix)
		results, err := d.FromData(context.Background(), true, []byte(good))
		if err != nil {
			t.Fatalf("FromData: %v", err)
		}
		if len(results) != 1 || !results[0].Verified {
			t.Fatalf("expected verified=true, got %+v", results)
		}
		if err := results[0].VerificationError(); err != nil {
			t.Fatalf("unexpected verification error: %v", err)
		}

		// Unknown key: US 401, EU 401 → Verified=false, no error.
		bad := fakeKey(phxPrefix, unknownBody)
		results, err = d.FromData(context.Background(), true, []byte(bad))
		if err != nil {
			t.Fatalf("FromData: %v", err)
		}
		if len(results) != 1 || results[0].Verified {
			t.Fatalf("expected verified=false for unknown key, got %+v", results)
		}
		if err := results[0].VerificationError(); err != nil {
			t.Fatalf("401 should not be a verification error, got %v", err)
		}

		if usCalls == 0 || euCalls == 0 {
			t.Fatalf("expected both regions to be queried at least once, got us=%d eu=%d", usCalls, euCalls)
		}
	})
}

func TestPostHogProjectAPIKeyVerification(t *testing.T) {
	good := fakeKey(phcPrefix, projectBody)

	us := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/decide/" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), good) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"featureFlags":{}}`)
	}))
	defer us.Close()

	withPostHogBaseURLs(t, []string{us.URL}, func() {
		d := findDetector(t, phcPrefix)
		results, err := d.FromData(context.Background(), true, []byte(good))
		if err != nil {
			t.Fatalf("FromData: %v", err)
		}
		if len(results) != 1 || !results[0].Verified {
			t.Fatalf("expected verified=true, got %+v", results)
		}
	})
}

func TestPostHogFeatureFlagKeyHasNoVerification(t *testing.T) {
	t.Parallel()

	d := findDetector(t, phsPrefix)
	key := fakeKey(phsPrefix, flagLongBody)
	results, err := d.FromData(context.Background(), true, []byte(key))
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

func withPostHogBaseURLs(t *testing.T, urls []string, fn func()) {
	t.Helper()
	prevURLs := append([]string(nil), posthogBaseURLs...)
	prevClient := posthogHTTPClient
	prevOnce := posthogHTTPClientOnce

	posthogBaseURLs = urls
	posthogHTTPClient = http.DefaultClient
	posthogHTTPClientOnce = sync.Once{}
	posthogHTTPClientOnce.Do(func() {}) // mark done so getPosthogHTTPClient returns the test client
	defer func() {
		posthogBaseURLs = prevURLs
		posthogHTTPClient = prevClient
		posthogHTTPClientOnce = prevOnce
	}()
	fn()
}

func findDetector(t *testing.T, prefix string) detectors.Detector {
	t.Helper()
	for _, d := range PostHogDetectors() {
		if d.Keywords()[0] == prefix {
			return d
		}
	}
	t.Fatalf("no detector for prefix %q", prefix)
	return nil
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
