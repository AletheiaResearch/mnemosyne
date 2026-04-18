package redact

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	thdet "github.com/trufflesecurity/trufflehog/v3/pkg/detectors"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/redact/detectors"
	_ "github.com/AletheiaResearch/mnemosyne/internal/redact/detectors/posthog"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
)

const PlaceholderMarker = "[MNEMOSYNE_REDACTED]"

type Options struct {
	CustomRedactions []string
	CustomHandles    []string
	// VerifySecrets toggles live verification against provider APIs for
	// detectors that support it. When false the pipeline stays fully
	// offline — matches are still redacted via their regex hits.
	VerifySecrets bool
	// Detectors, when non-nil, overrides the detector set the
	// trufflehog runner uses. Production callers leave this unset so the
	// pipeline gets defaults.DefaultDetectors() plus every provider
	// scanner registered via the detectors registry; tests pass a
	// curated slice (usually pointing at an httptest.Server) to avoid
	// touching real provider APIs.
	Detectors []thdet.Detector
}

type Pipeline struct {
	anonymizer *Anonymizer
	detector   *Detector
	trufflehog *trufflehogRunner
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

	ds := opts.Detectors
	if ds == nil {
		ds = detectors.All()
	}

	return &Pipeline{
		anonymizer: anonymizer,
		detector:   NewDetector(),
		trufflehog: newTrufflehogRunner(ds, opts.VerifySecrets),
		literals:   literals,
	}, nil
}

func FromConfig(cfg config.Config) (*Pipeline, error) {
	return FromConfigWithOptions(cfg, Options{})
}

// FromConfigWithOptions builds a Pipeline from persisted config while
// honouring extra runtime-only options (e.g. --verify-secrets). Fields
// already derived from cfg take precedence.
func FromConfigWithOptions(cfg config.Config, extra Options) (*Pipeline, error) {
	return New(Options{
		CustomRedactions: cfg.CustomRedactions,
		CustomHandles:    cfg.CustomHandles,
		VerifySecrets:    extra.VerifySecrets,
		Detectors:        extra.Detectors,
	})
}

func (p *Pipeline) ApplyRecord(record schema.Record) (schema.Record, int) {
	count := 0
	record.Grouping, count = p.applyText(record.Grouping)
	record.Branch, count = p.add(record.Branch, count, p.applyText)
	record.WorkingDir, count = p.add(record.WorkingDir, count, p.applyPath)
	record.Title, count = p.add(record.Title, count, p.applyText)

	if record.Extensions != nil {
		updated, c := p.applyAny(record.Extensions, "")
		record.Extensions = updated.(map[string]any)
		count += c
	}

	if record.Provenance != nil {
		record.Provenance.SourcePath, count = p.add(record.Provenance.SourcePath, count, p.applyPath)
		record.Provenance.SourceID, count = p.add(record.Provenance.SourceID, count, p.applyText)
		if record.Provenance.Extensions != nil {
			updated, c := p.applyAny(record.Provenance.Extensions, "")
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
	findings := p.detector.Scan(builder.String())
	p.trufflehog.Scan(builder.String(), &findings)
	return findings
}

func (p *Pipeline) applyTurn(turn schema.Turn, count int) (schema.Turn, int) {
	turn.Text, count = p.add(turn.Text, count, p.applyText)
	turn.Reasoning, count = p.add(turn.Reasoning, count, p.applyText)
	if turn.Extensions != nil {
		updated, c := p.applyAny(turn.Extensions, "")
		turn.Extensions = updated.(map[string]any)
		count += c
	}
	for idx, block := range turn.Attachments {
		turn.Attachments[idx], count = p.applyBlock(block, count)
	}
	for idx, call := range turn.ToolCalls {
		call.Tool, count = p.add(call.Tool, count, p.applyText)
		if call.Input != nil {
			updated, c := p.applyAny(call.Input, "")
			call.Input = updated
			count += c
		}
		if call.Output != nil {
			call.Output.Text, count = p.add(call.Output.Text, count, p.applyText)
			for jdx, block := range call.Output.Content {
				call.Output.Content[jdx], count = p.applyBlock(block, count)
			}
			if call.Output.Raw != nil {
				updated, c := p.applyAny(call.Output.Raw, "")
				call.Output.Raw = updated
				count += c
			}
		}
		call.Status, count = p.add(call.Status, count, p.applyText)
		turn.ToolCalls[idx] = call
	}
	return turn, count
}

func (p *Pipeline) applyBlock(block schema.ContentBlock, count int) (schema.ContentBlock, int) {
	block.Text, count = p.add(block.Text, count, p.applyText)
	block.URL, count = p.add(block.URL, count, p.applyURL)
	block.Name, count = p.add(block.Name, count, p.applyText)
	return block, count
}

func (p *Pipeline) applyAny(value any, key string) (any, int) {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		count := 0
		for childKey, childValue := range typed {
			next, c := p.applyAny(childValue, childKey)
			out[childKey] = next
			count += c
		}
		return out, count
	case []any:
		out := make([]any, 0, len(typed))
		count := 0
		for _, item := range typed {
			next, c := p.applyAny(item, key)
			out = append(out, next)
			count += c
		}
		return out, count
	case string:
		if looksLikePathKey(key) {
			return p.applyPath(typed)
		}
		if looksLikeURLKey(key) {
			return p.applyURL(typed)
		}
		if looksLikeCommandKey(key) {
			return p.applyCommand(typed)
		}
		return p.applyText(typed)
	default:
		return value, 0
	}
}

func (p *Pipeline) applyText(input string) (string, int) {
	return p.applyString(input, modeText)
}

func (p *Pipeline) applyPath(input string) (string, int) {
	return p.applyString(input, modePath)
}

func (p *Pipeline) applyURL(input string) (string, int) {
	return p.applyString(input, modeURL)
}

func (p *Pipeline) applyCommand(input string) (string, int) {
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
	out, trufflehogCount := p.trufflehog.Redact(out)
	count += trufflehogCount
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

func looksLikePathKey(key string) bool {
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

func looksLikeURLKey(key string) bool {
	key = strings.ToLower(key)
	return strings.Contains(key, "url") || strings.Contains(key, "uri")
}

func looksLikeCommandKey(key string) bool {
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
