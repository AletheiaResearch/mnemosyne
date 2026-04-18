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

// ApplyStats is the accumulator ApplyRecord returns. Redactions counts
// every substitution performed; VerifiedSecrets holds labels for secrets
// the pipeline confirmed live against a provider verifier (capped).
type ApplyStats struct {
	Redactions          int
	VerifiedSecretCount int
	VerifiedSecrets     []string

	// seenVerified dedups raw verified values while walking the record.
	// Unexported because callers shouldn't need the raw secret — only the
	// count and labels are surfaced via VerifiedSecret* fields.
	seenVerified map[string]struct{}
}

// recordVerified appends a label for a verified secret once per raw
// value. Returns true when the label was newly added.
func (s *ApplyStats) recordVerified(raw, label string) bool {
	if s == nil || raw == "" {
		return false
	}
	if s.seenVerified == nil {
		s.seenVerified = make(map[string]struct{})
	}
	if _, dup := s.seenVerified[raw]; dup {
		return false
	}
	s.seenVerified[raw] = struct{}{}
	s.VerifiedSecretCount++
	if len(s.VerifiedSecrets) < 20 {
		s.VerifiedSecrets = append(s.VerifiedSecrets, label)
	}
	return true
}

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

// ApplyRecord redacts sensitive content in the record. The returned
// ApplyStats carries the total replacement count plus, when
// Options.VerifySecrets is on, labels for secrets the pipeline confirmed
// live against a provider verifier.
func (p *Pipeline) ApplyRecord(record schema.Record) (schema.Record, ApplyStats) {
	stats := ApplyStats{}
	record.Grouping = p.applyText(record.Grouping, &stats)
	record.Branch = p.applyText(record.Branch, &stats)
	record.WorkingDir = p.applyPath(record.WorkingDir, &stats)
	record.Title = p.applyText(record.Title, &stats)

	if record.Extensions != nil {
		record.Extensions = p.applyAny(record.Extensions, "", &stats).(map[string]any)
	}

	if record.Provenance != nil {
		record.Provenance.SourcePath = p.applyPath(record.Provenance.SourcePath, &stats)
		record.Provenance.SourceID = p.applyText(record.Provenance.SourceID, &stats)
		if record.Provenance.Extensions != nil {
			record.Provenance.Extensions = p.applyAny(record.Provenance.Extensions, "", &stats).(map[string]any)
		}
	}

	for idx, turn := range record.Turns {
		record.Turns[idx] = p.applyTurn(turn, &stats)
	}

	return record, stats
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
	findings := Findings{}
	p.detector.ScanInto(builder.String(), &findings)
	p.trufflehog.Scan(builder.String(), &findings)
	return findings
}

func (p *Pipeline) applyTurn(turn schema.Turn, stats *ApplyStats) schema.Turn {
	turn.Text = p.applyText(turn.Text, stats)
	turn.Reasoning = p.applyText(turn.Reasoning, stats)
	if turn.Extensions != nil {
		turn.Extensions = p.applyAny(turn.Extensions, "", stats).(map[string]any)
	}
	for idx, block := range turn.Attachments {
		turn.Attachments[idx] = p.applyBlock(block, stats)
	}
	for idx, call := range turn.ToolCalls {
		call.Tool = p.applyText(call.Tool, stats)
		if call.Input != nil {
			call.Input = p.applyAny(call.Input, "", stats)
		}
		if call.Output != nil {
			call.Output.Text = p.applyText(call.Output.Text, stats)
			for jdx, block := range call.Output.Content {
				call.Output.Content[jdx] = p.applyBlock(block, stats)
			}
			if call.Output.Raw != nil {
				call.Output.Raw = p.applyAny(call.Output.Raw, "", stats)
			}
		}
		call.Status = p.applyText(call.Status, stats)
		turn.ToolCalls[idx] = call
	}
	return turn
}

func (p *Pipeline) applyBlock(block schema.ContentBlock, stats *ApplyStats) schema.ContentBlock {
	block.Text = p.applyText(block.Text, stats)
	block.URL = p.applyURL(block.URL, stats)
	block.Name = p.applyText(block.Name, stats)
	return block
}

func (p *Pipeline) applyAny(value any, key string, stats *ApplyStats) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			out[childKey] = p.applyAny(childValue, childKey, stats)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, p.applyAny(item, key, stats))
		}
		return out
	case string:
		if looksLikePathKey(key) {
			return p.applyPath(typed, stats)
		}
		if looksLikeURLKey(key) {
			return p.applyURL(typed, stats)
		}
		if looksLikeCommandKey(key) {
			return p.applyCommand(typed, stats)
		}
		return p.applyText(typed, stats)
	default:
		return value
	}
}

func (p *Pipeline) applyText(input string, stats *ApplyStats) string {
	return p.applyString(input, modeText, stats)
}

func (p *Pipeline) applyPath(input string, stats *ApplyStats) string {
	return p.applyString(input, modePath, stats)
}

func (p *Pipeline) applyURL(input string, stats *ApplyStats) string {
	return p.applyString(input, modeURL, stats)
}

func (p *Pipeline) applyCommand(input string, stats *ApplyStats) string {
	return p.applyString(input, modeCommand, stats)
}

type applyMode int

const (
	modeText applyMode = iota
	modePath
	modeURL
	modeCommand
)

func (p *Pipeline) applyString(input string, mode applyMode, stats *ApplyStats) string {
	if input == "" || LooksLikeLargeBinary(input) {
		return input
	}

	out, count := p.detector.Redact(input)
	out, trufflehogCount := p.trufflehog.Redact(out, stats)
	count += trufflehogCount
	out, literalCount := applyLiterals(out, p.literals)
	count += literalCount

	var final string
	switch mode {
	case modePath:
		final, count = p.anonymizer.ApplyPath(out, count)
	case modeURL:
		final, count = p.anonymizer.ApplyURL(out, count)
	default:
		final, count = p.anonymizer.ApplyText(out, count)
	}
	if stats != nil {
		stats.Redactions += count
	}
	return final
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
