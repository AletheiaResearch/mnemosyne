package redact

import "github.com/Quantumlyy/mnemosyne/internal/config"

const PlaceholderMarker = "[MNEMOSYNE_REDACTED]"

type Pipeline struct {
	Literals []string
}

func New(custom []string) *Pipeline {
	filtered := make([]string, 0, len(custom))
	for _, item := range custom {
		if len(item) >= 3 {
			filtered = append(filtered, item)
		}
	}
	return &Pipeline{Literals: filtered}
}

func FromConfig(cfg config.Config) *Pipeline {
	return New(cfg.CustomRedactions)
}

func (p *Pipeline) ApplyText(input string) (string, int) {
	if p == nil || input == "" {
		return input, 0
	}
	out := input
	count := 0
	for _, literal := range p.Literals {
		replaced, matches := replaceAll(out, literal, PlaceholderMarker)
		out = replaced
		count += matches
	}
	return out, count
}

func replaceAll(input, needle, repl string) (string, int) {
	if needle == "" {
		return input, 0
	}
	count := 0
	for {
		idx := indexOf(input, needle)
		if idx < 0 {
			return input, count
		}
		input = input[:idx] + repl + input[idx+len(needle):]
		count++
	}
}

func indexOf(input, needle string) int {
	for idx := 0; idx+len(needle) <= len(input); idx++ {
		if input[idx:idx+len(needle)] == needle {
			return idx
		}
	}
	return -1
}
