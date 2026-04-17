package redact

import "testing"

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
