package redact

import (
	"strings"
	"testing"
)

func TestDetectorRedactsSecrets(t *testing.T) {
	t.Parallel()

	detector := NewDetector()
	out, count := detector.Redact(`token=sk-AbCdEfGhIjKlMnOpQrStUvWx`)
	if count == 0 {
		t.Fatal("expected replacement count")
	}
	if out == `token=sk-AbCdEfGhIjKlMnOpQrStUvWx` {
		t.Fatal("expected input to be redacted")
	}
}

func TestAnonymizerRewritesHomePath(t *testing.T) {
	t.Parallel()

	anonymizer, err := NewAnonymizer([]string{"octocat"})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := anonymizer.ApplyText("/Users/alice/project and octocat", 0)
	if out == "/Users/alice/project and octocat" {
		t.Fatal("expected anonymized output")
	}
}

func TestStringsReplaceAllHandlesReplacementContainingNeedle(t *testing.T) {
	t.Parallel()

	out, count := stringsReplaceAll("token=REDACTED", "REDACTED", PlaceholderMarker)
	if count != 1 {
		t.Fatalf("expected 1 replacement, got %d", count)
	}
	if out != "token="+PlaceholderMarker {
		t.Fatalf("unexpected replacement output: %q", out)
	}
}

func TestAnonymizerRewritesShortHandleInsideHomeStylePath(t *testing.T) {
	t.Parallel()

	anonymizer, err := NewAnonymizer([]string{"xy"})
	if err != nil {
		t.Fatal(err)
	}

	out, _ := anonymizer.ApplyPath("/Users/xy/project", 0)
	if strings.Contains(out, "/Users/xy/") {
		t.Fatalf("expected short handle path segment to be anonymized, got %q", out)
	}
	if !strings.Contains(out, "/project") {
		t.Fatalf("expected path separator to be preserved, got %q", out)
	}
}

func TestAnonymizerDoesNotRewriteShortHandlePrefixCollisions(t *testing.T) {
	t.Parallel()

	anonymizer, err := NewAnonymizer([]string{"xy"})
	if err != nil {
		t.Fatal(err)
	}

	out, _ := anonymizer.ApplyPath("/Users/xyz/project", 0)
	if out != "/Users/xyz/project" {
		t.Fatalf("expected unrelated path to stay unchanged, got %q", out)
	}
}

func TestPipelineRedactsPostHogKeys(t *testing.T) {
	t.Parallel()

	pipeline, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		fakeKey(phxPrefix, personalBody),
		fakeKey(phsPrefix, flagBody),
		fakeKey(phcPrefix, projectBody),
	} {
		out, count := pipeline.applyText("Authorization: Bearer " + key)
		if count == 0 {
			t.Fatalf("expected redaction for %q", key)
		}
		if strings.Contains(out, key) {
			t.Fatalf("key %q still present in %q", key, out)
		}
		if !strings.Contains(out, PlaceholderMarker) {
			t.Fatalf("expected placeholder marker in %q", out)
		}
	}
}
