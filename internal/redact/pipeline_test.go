package redact

import (
	"strings"
	"testing"
)

func TestApplyAny_RoutesByKeyHeuristic(t *testing.T) {
	t.Parallel()
	p, err := New(Options{})
	if err != nil {
		t.Fatalf("new pipeline: %v", err)
	}

	input := map[string]any{
		"cwd":     "/Users/nejc/repo",
		"url":     "https://api.example.com?token=sk-abcdef1234567890",
		"command": "curl -H 'Authorization: Bearer ghp_abcdef1234567890ABCDEFGHIJ'",
		"text":    "contact me at alice@contoso.dev",
		"nested": []any{
			map[string]any{
				"path": "/Users/nejc/other",
				"body": "sk-abcdefghijklmnopqrstuvwxyz0123",
			},
		},
	}

	out, count := p.ApplyAny(input, "")
	if count == 0 {
		t.Fatalf("expected at least one redaction; got 0")
	}
	outMap, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("ApplyAny returned %T, want map", out)
	}

	for k := range input {
		if _, present := outMap[k]; !present {
			t.Fatalf("output missing key %q", k)
		}
	}

	if got := outMap["url"].(string); strings.Contains(got, "sk-abcdef1234567890") {
		t.Fatalf("url not redacted: %q", got)
	}
	if got := outMap["command"].(string); strings.Contains(got, "ghp_abcdef1234567890ABCDEFGHIJ") {
		t.Fatalf("command not redacted: %q", got)
	}
	if got := outMap["text"].(string); strings.Contains(got, "alice@contoso.dev") {
		t.Fatalf("email not redacted: %q", got)
	}
	nested := outMap["nested"].([]any)
	if got := nested[0].(map[string]any)["body"].(string); strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("nested body not redacted: %q", got)
	}
}

func TestApplyAny_PreservesNonStringLeaves(t *testing.T) {
	t.Parallel()
	p, err := New(Options{})
	if err != nil {
		t.Fatalf("new pipeline: %v", err)
	}
	input := map[string]any{
		"count": float64(42),
		"ok":    true,
		"none":  nil,
	}
	out, _ := p.ApplyAny(input, "")
	outMap := out.(map[string]any)
	if outMap["count"].(float64) != 42 || !outMap["ok"].(bool) || outMap["none"] != nil {
		t.Fatalf("non-string leaves mutated: %+v", outMap)
	}
}

func TestLooksLikeKeyHelpers(t *testing.T) {
	t.Parallel()
	if !LooksLikePathKey("cwd") || !LooksLikePathKey("file_path") {
		t.Fatalf("expected path key recognition")
	}
	if !LooksLikeURLKey("image_url") || !LooksLikeURLKey("URI") {
		t.Fatalf("expected url key recognition")
	}
	if !LooksLikeCommandKey("command") || !LooksLikeCommandKey("cmd") {
		t.Fatalf("expected command key recognition")
	}
	if LooksLikePathKey("thinking") {
		t.Fatalf("unexpected path key")
	}
}
