package publish

import "testing"

func TestIsHFNotFound(t *testing.T) {
	t.Parallel()
	notFoundOutputs := []string{
		"EntryNotFoundError: File manifest.mnemosyne does not exist",
		"404 Client Error: Not Found",
		"Repository Not Found for url: https://...",
		"hf: does not exist",
	}
	for _, output := range notFoundOutputs {
		if !isHFNotFound(output) {
			t.Errorf("expected not-found for %q", output)
		}
	}

	realFailures := []string{
		"authentication token missing",
		"connection refused",
		"500 Internal Server Error",
	}
	for _, output := range realFailures {
		if isHFNotFound(output) {
			t.Errorf("%q should not classify as not-found", output)
		}
	}
}
