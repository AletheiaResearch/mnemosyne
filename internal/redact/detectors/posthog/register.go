package posthog

import "github.com/AletheiaResearch/mnemosyne/internal/redact/detectors"

func init() {
	detectors.Register(
		NewPersonalAPIKeyScanner(),
		NewFeatureFlagSecureKeyScanner(),
		NewProjectAPIKeyScanner(),
	)
}
