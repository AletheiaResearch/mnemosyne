// Package detectors holds the in-process registry of trufflehog
// detectors that the redact pipeline uses at runtime. It combines the
// upstream-default detector set (defaults.DefaultDetectors) with any
// provider-specific scanners mnemosyne plugs in via Register.
package detectors

import (
	"sync"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/engine/defaults"
	"github.com/trufflesecurity/trufflehog/v3/pkg/pb/detector_typepb"
)

var (
	mu         sync.RWMutex
	registered []thdet.Detector
)

// Register appends detectors to the in-process registry. Intended for
// package init() in provider-specific subpackages so that plugging a new
// provider in is a one-line blank import on the consumer side.
func Register(ds ...thdet.Detector) {
	mu.Lock()
	defer mu.Unlock()
	registered = append(registered, ds...)
}

// All returns the union of upstream trufflehog defaults and every
// detector supplied via Register, in that order. The returned slice is
// freshly allocated each call so callers may mutate or extend it.
//
// Upstream's PostHog detector issues HTTP requests to PostHog when
// verify=true. The local regex-only scanners under detectors/posthog
// cover the same prefixes (phx_/phs_/phc_) with a broader body
// pattern, so filter the upstream one out to keep PostHog detection
// strictly non-networked regardless of --verify-secrets.
func All() []thdet.Detector {
	mu.RLock()
	extra := append([]thdet.Detector(nil), registered...)
	mu.RUnlock()
	base := defaults.DefaultDetectors()
	filtered := make([]thdet.Detector, 0, len(base))
	for _, d := range base {
		if d.Type() == detector_typepb.DetectorType_PosthogApp {
			continue
		}
		filtered = append(filtered, d)
	}
	return append(filtered, extra...)
}
