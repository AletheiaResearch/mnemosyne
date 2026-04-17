package serialize

import (
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

type ChatML struct{}

func (ChatML) Name() string {
	return "chatml"
}

func (ChatML) Description() string {
	return "Render records into ChatML-style tagged transcripts."
}

func (ChatML) Serialize(record schema.Record) (any, error) {
	lines := make([]string, 0, len(record.Turns)*2)
	for _, turn := range record.Turns {
		lines = append(lines, "<|im_start|>"+turn.Role)
		lines = append(lines, turn.Text)
		lines = append(lines, "<|im_end|>")
	}
	return struct {
		ID      string `json:"id"`
		Model   string `json:"model"`
		Content string `json:"content"`
	}{
		ID:      record.RecordID,
		Model:   record.Model,
		Content: strings.Join(lines, "\n"),
	}, nil
}
