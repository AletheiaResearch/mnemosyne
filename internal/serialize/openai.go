package serialize

import "github.com/AletheiaResearch/mnemosyne/internal/schema"

type OpenAI struct{}

func (OpenAI) Name() string {
	return "openai"
}

func (OpenAI) Description() string {
	return "Flatten records into OpenAI chat-completions messages."
}

func (OpenAI) Serialize(record schema.Record) (any, error) {
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
		if turn.Reasoning != "" && turn.Role == "assistant" {
			if text != "" {
				text += "\n\n"
			}
			text += turn.Reasoning
		}
		out.Messages = append(out.Messages, message{Role: turn.Role, Content: text})
	}
	return out, nil
}
