package source

import (
	"context"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

// Grouping is a discoverable unit of records within a Source — typically a
// workspace directory, session file, or digest bucket. The ID is the stable
// primary key used by Extract and the orchestrator fallback path; DisplayLabel
// is the human-readable form in `<source>:<short-id>` convention (e.g.
// "codex:proj-a", "gemini:aaaaaaaa"). EstimatedRecords/EstimatedBytes are
// best-effort pre-extract hints and may be zero when a cheap estimate is not
// available. Excluded marks groupings the user has blacklisted via config.
type Grouping struct {
	ID               string `json:"id"`
	DisplayLabel     string `json:"display_label"`
	Origin           string `json:"origin"`
	EstimatedRecords int    `json:"estimated_records"`
	EstimatedBytes   int64  `json:"estimated_bytes"`
	Excluded         bool   `json:"excluded,omitempty"`
}

// Source is the contract every conversation source (codex, claudecode,
// orchestrator, ...) implements. Name must return a stable, URL-safe
// identifier used for dispatch and display. Discover enumerates available
// groupings without loading records; missing roots produce an empty slice
// rather than an error. Extract streams records for a single Grouping to the
// callback; it may emit partial results before returning an error and should
// propagate any error returned by the callback unchanged.
type Source interface {
	Name() string
	Discover(context.Context) ([]Grouping, error)
	Extract(context.Context, Grouping, ExtractionContext, func(schema.Record) error) error
}

// SessionLookup is the optional sidecar interface a Source implements when it
// can resolve a single record by its external session ID. The orchestrator
// uses these lookups to fill in richer per-turn content from the native store
// when its own SQLite mirror is lossy. Implementations return (zero, false,
// nil) when the ID is unknown and only return a non-nil error for IO or
// decoding failures.
type SessionLookup interface {
	LookupSession(context.Context, string) (schema.Record, bool, error)
}
