package serialize

import "github.com/AletheiaResearch/mnemosyne/internal/schema"

type Canonical struct{}

func (Canonical) Name() string {
	return "canonical"
}

func (Canonical) Description() string {
	return "Emit records in Mnemosyne's canonical JSONL format."
}

func (Canonical) Serialize(record schema.Record) (any, error) {
	return record, nil
}
