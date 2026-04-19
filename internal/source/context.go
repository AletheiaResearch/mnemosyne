package source

import (
	"fmt"
	"log/slog"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
)

// ExtractionContext carries the shared plumbing a Source.Extract needs. All
// fields are optional: Logger may be nil (no structured logging), Redactor may
// be nil (no secret scrubbing), Warn may be nil (per-file warnings are
// dropped), and SuppressReasoning strips assistant chain-of-thought from the
// emitted records when set.
type ExtractionContext struct {
	Logger            *slog.Logger
	Redactor          *redact.Pipeline
	SuppressReasoning bool
	Warn              func(string)
}

// ReportWarning is a best-effort helper for per-file non-fatal warnings during
// extraction. It fans out to the caller-provided Warn callback and to the
// Logger when either is set; both are optional and the call never panics.
func ReportWarning(ctx ExtractionContext, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if ctx.Warn != nil {
		ctx.Warn(message)
	}
	if ctx.Logger != nil {
		ctx.Logger.Warn("extract warning", "warning", message)
	}
}
