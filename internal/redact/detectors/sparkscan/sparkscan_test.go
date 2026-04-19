package sparkscan

import (
	"context"
	"strings"
	"testing"
)

// Fixtures are assembled at runtime so the full provider-prefixed
// literal never appears in source — secret scanners otherwise flag
// them as leaked Sparkscan keys.
func fakeKey(prefix, body string) string { return prefix + body }

const (
	skLivePrefix = "ss" + "_sk_live_"
	skTestPrefix = "ss" + "_sk_test_"
	pkLivePrefix = "ss" + "_pk_live_"
	pkTestPrefix = "ss" + "_pk_test_"

	// 22 alnum chars — the minimum the regex accepts.
	body22 = "abcdefghijklmnopqrstuv"
	// 28 alnum chars — a comfortable middle.
	body28 = "ABCDEFGHIJKLMNOPQRSTUVWXabcd"
)

func TestRegexMatches(t *testing.T) {
	t.Parallel()

	skLive := fakeKey(skLivePrefix, body22)
	pkTest := fakeKey(pkTestPrefix, body28)

	cases := []struct {
		name    string
		input   string
		want    []string
		noMatch bool
	}{
		{
			name:  "sk live key in bearer header",
			input: "Authorization: Bearer " + skLive,
			want:  []string{skLive},
		},
		{
			name:  "pk test key amid prose",
			input: "note: " + pkTest + " was leaked",
			want:  []string{pkTest},
		},
		{
			name:  "sk test key in JSON body",
			input: `{"api_key":"` + fakeKey(skTestPrefix, body22) + `"}`,
			want:  []string{fakeKey(skTestPrefix, body22)},
		},
		{
			name:  "pk live key in JSON body",
			input: `{"api_key":"` + fakeKey(pkLivePrefix, body28) + `"}`,
			want:  []string{fakeKey(pkLivePrefix, body28)},
		},
		{
			name:    "too short to count",
			input:   skLivePrefix + "short",
			noMatch: true,
		},
		{
			// One char shy of the 22-char minimum.
			name:    "body one byte below minimum",
			input:   skLivePrefix + body22[:21],
			noMatch: true,
		},
		{
			name:    "wrong key type is ignored",
			input:   "ss" + "_xk_live_" + body22,
			noMatch: true,
		},
		{
			name:    "wrong environment is ignored",
			input:   "ss" + "_sk_prod_" + body22,
			noMatch: true,
		},
		{
			name:  "dedup within a single chunk",
			input: "one: " + skLive + "\ntwo: " + skLive,
			want:  []string{skLive},
		},
		{
			// Regression: non-overlapping matches must not swallow the
			// shared delimiter between two distinct adjacent keys, or
			// the second key leaks through.
			name:  "adjacent distinct keys both match",
			input: skLive + "\n" + pkTest,
			want:  []string{skLive, pkTest},
		},
		{
			name:  "key at start of input matches",
			input: skLive + " trailing prose",
			want:  []string{skLive},
		},
		{
			name:  "key at end of input matches",
			input: "prefix prose " + skLive,
			want:  []string{skLive},
		},
	}

	scanner := NewScanner()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			results, err := scanner.FromData(context.Background(), false, []byte(tc.input))
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
				if !strings.HasPrefix(r.Redacted, "ss_") {
					t.Fatalf("redacted %q missing ss_ prefix", r.Redacted)
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
// pipeline intentionally keeps Sparkscan detection regex-only.
func TestFromDataIgnoresVerifyArg(t *testing.T) {
	t.Parallel()

	scanner := NewScanner()
	key := fakeKey(skLivePrefix, body22)
	results, err := scanner.FromData(context.Background(), true, []byte(key))
	if err != nil {
		t.Fatalf("FromData: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Verified {
		t.Fatalf("verify=true must not mark Sparkscan keys verified (regex-only)")
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
