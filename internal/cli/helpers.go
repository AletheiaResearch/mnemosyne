package cli

import (
	"encoding/json"
)

func marshalPretty(value any) ([]byte, error) {
	return json.MarshalIndent(value, "", "  ")
}
