package serialize

import (
	"fmt"
	"strings"

	"github.com/Quantumlyy/mnemosyne/internal/schema"
)

type Flat struct{}

func (Flat) Name() string {
	return "flat"
}

func (Flat) Description() string {
	return "Emit one compact transcript string per record."
}

func (Flat) Serialize(record schema.Record) (any, error) {
	lines := make([]string, 0, len(record.Turns))
	for _, turn := range record.Turns {
		lines = append(lines, fmt.Sprintf("[%s] %s", turn.Role, turn.Text))
	}
	return struct {
		ID         string `json:"id"`
		Model      string `json:"model"`
		Transcript string `json:"transcript"`
	}{
		ID:         record.RecordID,
		Model:      record.Model,
		Transcript: strings.Join(lines, "\n"),
	}, nil
}
