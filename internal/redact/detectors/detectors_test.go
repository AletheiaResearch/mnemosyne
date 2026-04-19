package detectors

import (
	"testing"

	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

// TestAllExcludesUpstreamPostHog guards against the upstream
// trufflehog PostHog detector slipping back into the default set.
// That detector calls `https://app.posthog.com/api/event/` when
// verify=true, which would ship real PostHog keys to the PostHog API
// on `mnemosyne extract --verify-secrets` despite the local
// regex-only scanners in detectors/posthog documenting PostHog
// detection as non-networked.
func TestAllExcludesUpstreamPostHog(t *testing.T) {
	t.Parallel()

	all := All()
	if len(all) == 0 {
		t.Fatal("All() returned no detectors — upstream defaults are missing")
	}
	for _, d := range all {
		if d.Type() == detector_typepb.DetectorType_PosthogApp {
			t.Fatalf("upstream PostHog detector must be filtered out; found %T with DetectorType_PosthogApp", d)
		}
	}
}
