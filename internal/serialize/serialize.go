package serialize

import "github.com/Quantumlyy/mnemosyne/internal/schema"

type Serializer interface {
	Name() string
	Description() string
	Serialize(schema.Record) (any, error)
}
