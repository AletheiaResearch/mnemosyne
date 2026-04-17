package source

import (
	"fmt"
	"log/slog"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
)

type ExtractionContext struct {
	Logger            *slog.Logger
	Redactor          *redact.Pipeline
	SuppressReasoning bool
	Warn              func(string)
}

func ReportWarning(ctx ExtractionContext, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if ctx.Warn != nil {
		ctx.Warn(message)
	}
	if ctx.Logger != nil {
		ctx.Logger.Warn("extract warning", "warning", message)
	}
}
