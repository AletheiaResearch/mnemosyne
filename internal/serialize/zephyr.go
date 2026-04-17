package serialize

import (
	"strings"

	"github.com/Quantumlyy/mnemosyne/internal/schema"
)

type Zephyr struct{}

func (Zephyr) Name() string {
	return "zephyr"
}

func (Zephyr) Description() string {
	return "Render records into Zephyr-style turn-delimited text."
}

func (Zephyr) Serialize(record schema.Record) (any, error) {
	parts := make([]string, 0, len(record.Turns))
	for _, turn := range record.Turns {
		parts = append(parts, "<|"+turn.Role+"|>\n"+turn.Text)
	}
	return struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}{
		ID:      record.RecordID,
		Model:   record.Model,
		Content: strings.Join(parts, "\n"),
	}, nil
}
