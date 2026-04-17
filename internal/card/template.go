package card

import "encoding/json"

func RenderManifest(summary Summary) ([]byte, error) {
	return json.MarshalIndent(summary, "", "  ")
}
