package redact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setFakeHome(t *testing.T, username string) string {
	t.Helper()
	root := t.TempDir()
	var home string
	if isWindows() {
		home = filepath.Join(root, "Users", username)
	} else {
		home = filepath.Join(root, "Users", username)
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	// USERPROFILE is consulted on Windows by os.UserHomeDir.
	t.Setenv("USERPROFILE", home)
	return home
}

func isWindows() bool {
	return os.PathSeparator == '\\'
}

func TestNewAnonymizerStillUsableWhenHomeResolutionFails(t *testing.T) {
	t.Parallel()
	// A blank anonymizer with no identifiers should not explode on Apply.
	a := &Anonymizer{shortPathTokens: map[string]string{}}
	out, count := a.ApplyText("hello world", 0)
	if out != "hello world" || count != 0 {
		t.Fatalf("unexpected = %q / %d", out, count)
	}
	out, count = a.ApplyPath("hello world", 0)
	if out != "hello world" || count != 0 {
		t.Fatalf("unexpected path passthrough = %q / %d", out, count)
	}
	out, count = a.ApplyURL("", 0)
	if out != "" || count != 0 {
		t.Fatalf("unexpected empty url = %q / %d", out, count)
	}
}

func TestApplyTextReplacesHomeAndHandlesDeterministically(t *testing.T) {
	home := setFakeHome(t, "alice")

	a, err := NewAnonymizer([]string{"alice", "ghHandle"})
	if err != nil {
		t.Fatal(err)
	}

	input := "see " + home + "/docs and reach out to ghHandle please"
	out, count := a.ApplyText(input, 0)
	if strings.Contains(out, home) {
		t.Fatalf("home path leaked in %q", out)
	}
	if strings.Contains(out, "ghHandle") {
		t.Fatalf("handle leaked in %q", out)
	}
	if count == 0 {
		t.Fatalf("expected at least one replacement, got %q", out)
	}

	out2, _ := a.ApplyText(input, 0)
	if out != out2 {
		t.Fatalf("non-deterministic output: %q vs %q", out, out2)
	}

	// Different identifiers must produce different tokens.
	if tokenFor("alice") == tokenFor("bob") {
		t.Fatalf("tokens collided for distinct inputs")
	}
	// Token format: "anon_" + 10 hex chars.
	token := tokenFor("alice")
	if !strings.HasPrefix(token, "anon_") || len(token) != len("anon_")+10 {
		t.Fatalf("unexpected token shape %q", token)
	}
}

func TestApplyTextIgnoresHandleSubstrings(t *testing.T) {
	setFakeHome(t, "alice")
	a, err := NewAnonymizer([]string{"ghHandle"})
	if err != nil {
		t.Fatal(err)
	}
	// The matcher uses word boundaries so "ghHandleX" should not be rewritten.
	out, count := a.ApplyText("prefix ghHandleX suffix", 0)
	if count != 0 || !strings.Contains(out, "ghHandleX") {
		t.Fatalf("unexpected replacement in %q (count=%d)", out, count)
	}
}

func TestApplyPathRewritesHomeAndPassesThroughOthers(t *testing.T) {
	home := setFakeHome(t, "alice")
	a, err := NewAnonymizer([]string{"alice"})
	if err != nil {
		t.Fatal(err)
	}

	out, count := a.ApplyPath(filepath.Join(home, "work", "repo"), 0)
	if strings.Contains(out, "alice") {
		t.Fatalf("username leaked in %q", out)
	}
	if !strings.HasSuffix(out, filepath.Join("work", "repo")) {
		t.Fatalf("path suffix lost in %q", out)
	}
	if count == 0 {
		t.Fatalf("expected path substitution counted")
	}

	out2, count2 := a.ApplyPath(filepath.Join(home, "work", "repo"), 0)
	if out != out2 || count != count2 {
		t.Fatalf("determinism broken: %q/%d vs %q/%d", out, count, out2, count2)
	}

	// A path outside $HOME falls through to ApplyText.
	other := filepath.Join(t.TempDir(), "other")
	outOther, _ := a.ApplyPath(other, 0)
	if outOther != other {
		t.Fatalf("unrelated path rewritten: %q → %q", other, outOther)
	}

	empty, count := a.ApplyPath("", 5)
	if empty != "" || count != 5 {
		t.Fatalf("empty input must pass through, got %q / %d", empty, count)
	}
}

func TestApplyURLRewritesHomePathsInsideURL(t *testing.T) {
	home := setFakeHome(t, "alice")
	a, err := NewAnonymizer([]string{"alice"})
	if err != nil {
		t.Fatal(err)
	}

	urlIn := "file://" + filepath.ToSlash(home) + "/project/readme.md"
	out, count := a.ApplyURL(urlIn, 0)
	if strings.Contains(out, "alice") {
		t.Fatalf("username leaked in URL %q", out)
	}
	if !strings.HasPrefix(out, "file://") {
		t.Fatalf("scheme lost in %q", out)
	}
	if !strings.HasSuffix(out, "/project/readme.md") {
		t.Fatalf("path suffix lost in %q", out)
	}
	if count == 0 {
		t.Fatalf("expected URL substitution counted")
	}

	// A URL pointing outside $HOME is reconstructed unchanged.
	out, count = a.ApplyURL("https://example.com/api/v1/items", 0)
	if out != "https://example.com/api/v1/items" || count != 0 {
		t.Fatalf("non-home url mangled: %q / %d", out, count)
	}
	// Empty URL short-circuits.
	out, count = a.ApplyURL("", 7)
	if out != "" || count != 7 {
		t.Fatalf("empty url should pass through, got %q / %d", out, count)
	}
}

func TestAddIdentifierHandlesShortAndLongFormsDifferently(t *testing.T) {
	setFakeHome(t, "alice")
	a, err := NewAnonymizer([]string{"bo", "longerName"})
	if err != nil {
		t.Fatal(err)
	}
	// Short identifiers go to shortPathTokens; long identifiers get regex matchers.
	if _, ok := a.shortPathTokens["bo"]; !ok {
		t.Fatalf("short identifier not registered: %+v", a.shortPathTokens)
	}
	foundLong := false
	for _, m := range a.textMatchers {
		if strings.Contains(m.re.String(), "longerName") {
			foundLong = true
			break
		}
	}
	if !foundLong {
		t.Fatalf("long identifier did not produce a matcher")
	}
}
