// Package detectors holds the in-process registry of trufflehog
// detectors that the redact pipeline uses at runtime. It combines the
// upstream-default detector set (defaults.DefaultDetectors) with any
// provider-specific scanners mnemosyne plugs in via Register.
package detectors

import (
	"sync"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"
	"github.com/trufflesecurity/trufflehog/v3/pkg/engine/defaults"
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
func All() []thdet.Detector {
	mu.RLock()
	extra := append([]thdet.Detector(nil), registered...)
	mu.RUnlock()
	base := defaults.DefaultDetectors()
	return append(base, extra...)
}
