package redact

import (
	"strings"
	"testing"
)

func TestRedactHighEntropyReplacesMixedClassSecrets(t *testing.T) {
	t.Parallel()

	input := `token = "TESTONLY_A1b2C3d4E5f6G7h8I9j0K1" and key = "TESTONLY_Z9y8X7w6V5u4T3s2R1q0P9"`
	out, count := redactHighEntropy(input)
	if count < 2 {
		t.Fatalf("expected both secrets redacted, got count=%d out=%q", count, out)
	}
	if strings.Contains(out, "TESTONLY_A1b2C3d4E5f6G7h8I9j0K1") {
		t.Fatalf("high-entropy secret leaked: %q", out)
	}
	if !strings.Contains(out, PlaceholderMarker) {
		t.Fatalf("placeholder marker missing: %q", out)
	}
}

func TestRedactHighEntropyLeavesLowEntropyAlone(t *testing.T) {
	t.Parallel()

	// Repeats: low entropy, should not be flagged even though length qualifies.
	input := `note = "aaaaaaaaaaaaaaaaaaaaaaaa"`
	out, count := redactHighEntropy(input)
	if count != 0 || out != input {
		t.Fatalf("unexpected redaction: %q → %q (count=%d)", input, out, count)
	}
}

func TestRedactHighEntropyRequiresMixedCharacterClasses(t *testing.T) {
	t.Parallel()

	// All-lowercase alphanumeric of sufficient length but no upper / mixed classes.
	input := `note = "abcdefghijklmnopqrstuvwxyz"`
	out, count := redactHighEntropy(input)
	if count != 0 || out != input {
		t.Fatalf("single-class secret should not be redacted, got %q / %d", out, count)
	}
}

func TestScanEntropyRespectsLimitAndContextWindow(t *testing.T) {
	t.Parallel()

	input := `a="TESTONLY_A1b2C3d4E5f6G7h8I9j0K1" b="TESTONLY_Z9y8X7w6V5u4T3s2R1q0P9" c="TESTONLY_M7n6B5v4C3x2Z1a0S9d8"`
	findings := ScanEntropy(input, 2)
	if len(findings) != 2 {
		t.Fatalf("expected limit of 2 findings, got %d: %+v", len(findings), findings)
	}
	for _, finding := range findings {
		if finding.Category != "high_entropy" {
			t.Fatalf("unexpected category %q", finding.Category)
		}
		if finding.Value == "" {
			t.Fatalf("empty value in finding %+v", finding)
		}
		if finding.Context == "" || !strings.Contains(finding.Context, finding.Value) {
			t.Fatalf("context does not embed the finding value: %+v", finding)
		}
	}
}

func TestScanEntropyIgnoresLowEntropyStrings(t *testing.T) {
	t.Parallel()
	input := `readme = "the quick brown fox jumped over"`
	if got := ScanEntropy(input, 5); len(got) != 0 {
		t.Fatalf("expected no findings, got %+v", got)
	}
}

func TestEntropyComputesShannon(t *testing.T) {
	t.Parallel()
	if got := entropy(""); got != 0 {
		t.Fatalf("entropy(empty) = %f", got)
	}
	if got := entropy("aaaa"); got != 0 {
		t.Fatalf("entropy of uniform input should be 0, got %f", got)
	}
	// Perfect 2-symbol alternation gives exactly 1 bit.
	if got := entropy("abab"); got < 0.99 || got > 1.01 {
		t.Fatalf("entropy(abab) expected ~1.0, got %f", got)
	}
}

func TestHasMixedClassesDetectsAllThreeClasses(t *testing.T) {
	t.Parallel()
	if !hasMixedClasses("Abc123") {
		t.Fatalf("Abc123 should be mixed")
	}
	if hasMixedClasses("abcdef") {
		t.Fatalf("all-lower should not be mixed")
	}
	if hasMixedClasses("ABC123") {
		t.Fatalf("missing lowercase should not be mixed")
	}
	if hasMixedClasses("abcABC") {
		t.Fatalf("missing digit should not be mixed")
	}
}
