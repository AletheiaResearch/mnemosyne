package source

import (
	"log/slog"

	"github.com/AletheiaResearch/mnemosyne/internal/redact"
)

type ExtractionContext struct {
	Logger            *slog.Logger
	Redactor          *redact.Pipeline
	SuppressReasoning bool
}
