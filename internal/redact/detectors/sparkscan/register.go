package sparkscan

import "github.com/AletheiaResearch/mnemosyne/internal/redact/detectors"

func init() {
	detectors.Register(NewScanner())
}
