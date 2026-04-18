package redact

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

const PlaceholderMarker = "[MNEMOSYNE_REDACTED]"

type Options struct {
	CustomRedactions []string
	CustomHandles    []string
}

type Pipeline struct {
	anonymizer *Anonymizer
	detector   *Detector
	literals   []string
}

func New(opts Options) (*Pipeline, error) {
	anonymizer, err := NewAnonymizer(opts.CustomHandles)
	if err != nil {
		return nil, err
	}

	literals := make([]string, 0, len(opts.CustomRedactions))
	for _, item := range opts.CustomRedactions {
		item = strings.TrimSpace(item)
		if len(item) >= 3 {
			literals = append(literals, item)
		}
	}
	slices.Sort(literals)

	return &Pipeline{
		anonymizer: anonymizer,
		detector:   NewDetector(),
		literals:   literals,
	}, nil
}

func FromConfig(cfg config.Config) (*Pipeline, error) {
	return New(Options{
		CustomRedactions: cfg.CustomRedactions,
		CustomHandles:    cfg.CustomHandles,
	})
}

func (p *Pipeline) ApplyRecord(record schema.Record) (schema.Record, int) {
	count := 0
	record.Grouping, count = p.ApplyText(record.Grouping)
	record.Branch, count = p.add(record.Branch, count, p.ApplyText)
	record.WorkingDir, count = p.add(record.WorkingDir, count, p.ApplyPath)
	record.Title, count = p.add(record.Title, count, p.ApplyText)

	if record.Extensions != nil {
		updated, c := p.ApplyAny(record.Extensions, "")
		record.Extensions = updated.(map[string]any)
		count += c
	}

	if record.Provenance != nil {
		record.Provenance.SourcePath, count = p.add(record.Provenance.SourcePath, count, p.ApplyPath)
		record.Provenance.SourceID, count = p.add(record.Provenance.SourceID, count, p.ApplyText)
		if record.Provenance.Extensions != nil {
			updated, c := p.ApplyAny(record.Provenance.Extensions, "")
			record.Provenance.Extensions = updated.(map[string]any)
			count += c
		}
	}

	for idx, turn := range record.Turns {
		record.Turns[idx], count = p.applyTurn(turn, count)
	}

	return record, count
}

func (p *Pipeline) ScanRecord(record schema.Record) Findings {
	var builder strings.Builder
	builder.WriteString(record.Grouping)
	builder.WriteRune('\n')
	builder.WriteString(record.WorkingDir)
	builder.WriteRune('\n')
	for _, turn := range record.Turns {
		builder.WriteString(turn.Text)
		builder.WriteRune('\n')
		builder.WriteString(turn.Reasoning)
		builder.WriteRune('\n')
	}
	return p.detector.Scan(builder.String())
}

func (p *Pipeline) applyTurn(turn schema.Turn, count int) (schema.Turn, int) {
	turn.Text, count = p.add(turn.Text, count, p.ApplyText)
	turn.Reasoning, count = p.add(turn.Reasoning, count, p.ApplyText)
	if turn.Extensions != nil {
		updated, c := p.ApplyAny(turn.Extensions, "")
		turn.Extensions = updated.(map[string]any)
		count += c
	}
	for idx, block := range turn.Attachments {
		turn.Attachments[idx], count = p.applyBlock(block, count)
	}
	for idx, call := range turn.ToolCalls {
		call.Tool, count = p.add(call.Tool, count, p.ApplyText)
		if call.Input != nil {
			updated, c := p.ApplyAny(call.Input, "")
			call.Input = updated
			count += c
		}
		if call.Output != nil {
			call.Output.Text, count = p.add(call.Output.Text, count, p.ApplyText)
			for jdx, block := range call.Output.Content {
				call.Output.Content[jdx], count = p.applyBlock(block, count)
			}
			if call.Output.Raw != nil {
				updated, c := p.ApplyAny(call.Output.Raw, "")
				call.Output.Raw = updated
				count += c
			}
		}
		call.Status, count = p.add(call.Status, count, p.ApplyText)
		turn.ToolCalls[idx] = call
	}
	return turn, count
}

func (p *Pipeline) applyBlock(block schema.ContentBlock, count int) (schema.ContentBlock, int) {
	block.Text, count = p.add(block.Text, count, p.ApplyText)
	block.URL, count = p.add(block.URL, count, p.ApplyURL)
	block.Name, count = p.add(block.Name, count, p.ApplyText)
	return block, count
}

// ApplyAny walks any JSON-decoded value (map/slice/string/other) and applies
// string-level redaction based on key heuristics. Maps and slices are rebuilt
// bottom-up; string values are routed through ApplyPath/ApplyURL/ApplyCommand/ApplyText
// according to the enclosing key's name.
func (p *Pipeline) ApplyAny(value any, key string) (any, int) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		count := 0
		for childKey, childValue := range typed {
			next, c := p.ApplyAny(childValue, childKey)
			out[childKey] = next
			count += c
		}
		return out, count
	case []any:
		out := make([]any, 0, len(typed))
		count := 0
		for _, item := range typed {
			next, c := p.ApplyAny(item, key)
			out = append(out, next)
			count += c
		}
		return out, count
	case string:
		switch {
		case LooksLikePathKey(key):
			return p.ApplyPath(typed)
		case LooksLikeURLKey(key):
			return p.ApplyURL(typed)
		case LooksLikeCommandKey(key):
			return p.ApplyCommand(typed)
		default:
			return p.ApplyText(typed)
		}
	default:
		return value, 0
	}
}

func (p *Pipeline) ApplyText(input string) (string, int) {
	return p.applyString(input, modeText)
}

func (p *Pipeline) ApplyPath(input string) (string, int) {
	return p.applyString(input, modePath)
}

func (p *Pipeline) ApplyURL(input string) (string, int) {
	return p.applyString(input, modeURL)
}

func (p *Pipeline) ApplyCommand(input string) (string, int) {
	return p.applyString(input, modeCommand)
}

type applyMode int

const (
	modeText applyMode = iota
	modePath
	modeURL
	modeCommand
)

func (p *Pipeline) applyString(input string, mode applyMode) (string, int) {
	if input == "" || LooksLikeLargeBinary(input) {
		return input, 0
	}

	out, count := p.detector.Redact(input)
	out, literalCount := applyLiterals(out, p.literals)
	count += literalCount

	switch mode {
	case modePath:
		return p.anonymizer.ApplyPath(out, count)
	case modeURL:
		return p.anonymizer.ApplyURL(out, count)
	default:
		return p.anonymizer.ApplyText(out, count)
	}
}

func applyLiterals(input string, literals []string) (string, int) {
	if len(literals) == 0 || input == "" {
		return input, 0
	}

	count := 0
	out := input
	for _, literal := range literals {
		replaced, matches := stringsReplaceAll(out, literal, PlaceholderMarker)
		out = replaced
		count += matches
	}
	return out, count
}

func stringsReplaceAll(input, needle, repl string) (string, int) {
	if needle == "" {
		return input, 0
	}
	count := strings.Count(input, needle)
	if count == 0 {
		return input, 0
	}
	return strings.ReplaceAll(input, needle, repl), count
}

func (p *Pipeline) add(input string, current int, fn func(string) (string, int)) (string, int) {
	output, count := fn(input)
	return output, current + count
}

func CloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	maps.Copy(out, input)
	return out
}

func LooksLikePathKey(key string) bool {
	key = strings.ToLower(key)
	for _, candidate := range []string{
		"path", "cwd", "directory", "dir", "file", "filepath", "file_path",
		"target", "destination", "dest", "root", "workspace",
	} {
		if key == candidate || strings.Contains(key, candidate) {
			return true
		}
	}
	return false
}

func LooksLikeURLKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "url") || strings.Contains(key, "uri")
}

func LooksLikeCommandKey(key string) bool {
	key = strings.ToLower(key)
	return key == "command" || key == "cmd" || strings.Contains(key, "command")
}

func MarshalFindingSummary(findings Findings) string {
	if findings.Empty() {
		return "no findings"
	}
	return fmt.Sprintf("%d emails, %d public IPs, %d token-like matches, %d entropy matches",
		findings.EmailCount, findings.PublicIPCount, findings.TokenCount, len(findings.HighEntropy))
}
