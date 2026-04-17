package source

import (
	"context"

	"github.com/Quantumlyy/mnemosyne/internal/schema"
)

type Grouping struct {
	ID               string `json:"id"`
	DisplayLabel     string `json:"display_label"`
	Origin           string `json:"origin"`
	EstimatedRecords int    `json:"estimated_records"`
	EstimatedBytes   int64  `json:"estimated_bytes"`
	Excluded         bool   `json:"excluded,omitempty"`
}

type Source interface {
	Name() string
	Discover(context.Context) ([]Grouping, error)
	Extract(context.Context, Grouping, ExtractionContext, func(schema.Record) error) error
}
