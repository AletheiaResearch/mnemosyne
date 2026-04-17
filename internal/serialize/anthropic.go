package serialize

import "github.com/AletheiaResearch/mnemosyne/internal/schema"

type Anthropic struct{}

func (Anthropic) Name() string {
	return "anthropic"
}

func (Anthropic) Description() string {
	return "Flatten records into Anthropic message arrays."
}

func (Anthropic) Serialize(record schema.Record) (any, error) {
	type message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	out := struct {
		ID       string    `json:"id"`
		Model    string    `json:"model"`
		Messages []message `json:"messages"`
	}{
		ID:    record.RecordID,
		Model: record.Model,
	}
	for _, turn := range record.Turns {
		text := turn.Text
		if text == "" && len(turn.Attachments) > 0 {
			text = "[attachments omitted]"
		}
		out.Messages = append(out.Messages, message{Role: turn.Role, Content: text})
	}
	return out, nil
}
