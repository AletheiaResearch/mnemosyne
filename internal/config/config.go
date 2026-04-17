package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Phase string

const (
	PhaseInitial       Phase = "initial"
	PhasePreparing     Phase = "preparing"
	PhasePendingReview Phase = "pending-review"
	PhaseCleared       Phase = "cleared"
	PhaseFinalized     Phase = "finalized"
)

type Config struct {
	DestinationRepo        string                     `json:"destination_repo,omitempty"`
	OriginScope            string                     `json:"origin_scope,omitempty"`
	ExcludedGroupings      []string                   `json:"excluded_groupings,omitempty"`
	CustomRedactions       []string                   `json:"custom_redactions,omitempty"`
	CustomHandles          []string                   `json:"custom_handles,omitempty"`
	ScopeConfirmed         bool                       `json:"scope_confirmed,omitempty"`
	PhaseMarker            Phase                      `json:"phase_marker,omitempty"`
	LastExtract            *LastExtract               `json:"last_extract,omitempty"`
	ReviewerStatements     *ReviewerStatements        `json:"reviewer_statements,omitempty"`
	VerificationRecord     *VerificationRecord        `json:"verification_record,omitempty"`
	LastAttest             *LastAttest                `json:"last_attest,omitempty"`
	PublicationAttestation string                     `json:"publication_attestation,omitempty"`
	Unknown                map[string]json.RawMessage `json:"-"`
}

type LastExtract struct {
	Timestamp      string   `json:"timestamp"`
	RecordCount    int      `json:"record_count"`
	SkippedRecords int      `json:"skipped_records,omitempty"`
	RedactionCount int      `json:"redaction_count,omitempty"`
	InputTokens    int      `json:"input_tokens,omitempty"`
	OutputTokens   int      `json:"output_tokens,omitempty"`
	Scope          string   `json:"scope"`
	OutputPath     string   `json:"output_path,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
}

type ReviewerStatements struct {
	IdentityScan    string `json:"identity_scan"`
	EntityInterview string `json:"entity_interview"`
	ManualReview    string `json:"manual_review"`
}

type VerificationRecord struct {
	FullName           string `json:"full_name,omitempty"`
	NameScanSkipped    bool   `json:"name_scan_skipped,omitempty"`
	FullNameMatchCount int    `json:"full_name_match_count,omitempty"`
	ManualSampleCount  int    `json:"manual_sample_count,omitempty"`
}

type LastAttest struct {
	Timestamp         string `json:"timestamp"`
	FilePath          string `json:"file_path"`
	BuiltInFindings   bool   `json:"built_in_findings,omitempty"`
	FullName          string `json:"full_name,omitempty"`
	NameScanSkipped   bool   `json:"name_scan_skipped,omitempty"`
	FullNameMatches   int    `json:"full_name_matches,omitempty"`
	ManualSampleCount int    `json:"manual_sample_count,omitempty"`
	FileSize          int64  `json:"file_size,omitempty"`
}

func Default() Config {
	return Config{
		ExcludedGroupings: make([]string, 0),
		CustomRedactions:  make([]string, 0),
		CustomHandles:     make([]string, 0),
		PhaseMarker:       PhaseInitial,
		Unknown:           make(map[string]json.RawMessage),
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path == "" {
		var err error
		path, err = File()
		if err != nil {
			return cfg, err
		}
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return cfg, nil
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return cfg, fmt.Errorf("load settings: %w", err)
	}

	type alias Config
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return cfg, fmt.Errorf("decode settings: %w", err)
	}

	cfg = Config(decoded)
	cfg.Unknown = make(map[string]json.RawMessage)
	for key, value := range raw {
		switch key {
		case "destination_repo", "origin_scope", "excluded_groupings", "custom_redactions",
			"custom_handles", "scope_confirmed", "phase_marker", "last_extract",
			"reviewer_statements", "verification_record", "last_attest",
			"publication_attestation":
		default:
			cfg.Unknown[key] = value
		}
	}

	cfg.normalize()
	return Migrate(cfg), nil
}

func Save(path string, cfg Config) error {
	if path == "" {
		var err error
		path, err = File()
		if err != nil {
			return err
		}
	}

	cfg = Migrate(cfg)
	cfg.normalize()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	payload, err := cfg.marshal()
	if err != nil {
		return err
	}

	tmp := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, payload, 0o600); err != nil {
		return err
	}
	defer os.Remove(tmp)

	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (c Config) marshal() ([]byte, error) {
	type alias Config
	payload, err := json.Marshal(alias(c))
	if err != nil {
		return nil, err
	}

	var out map[string]json.RawMessage
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	for key, value := range c.Unknown {
		out[key] = value
	}
	return json.MarshalIndent(out, "", "  ")
}

func (c *Config) RefreshPhase(hasCredentials bool) {
	switch {
	case c.LastPublishAvailable():
		c.PhaseMarker = PhaseFinalized
	case c.LastAttest != nil:
		c.PhaseMarker = PhaseCleared
	case c.LastExtract != nil:
		c.PhaseMarker = PhasePendingReview
	case hasCredentials:
		c.PhaseMarker = PhasePreparing
	default:
		c.PhaseMarker = PhaseInitial
	}
}

func (c Config) LastPublishAvailable() bool {
	return c.PublicationAttestation != ""
}

func (c *Config) MergeStringSlice(dst *[]string, values []string) {
	seen := make(map[string]struct{}, len(*dst)+len(values))
	for _, item := range *dst {
		if item == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	for _, item := range values {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		seen[item] = struct{}{}
	}
	merged := make([]string, 0, len(seen))
	for item := range seen {
		merged = append(merged, item)
	}
	slices.Sort(merged)
	*dst = merged
}

func (c Config) Masked() Config {
	copyCfg := c
	copyCfg.CustomRedactions = make([]string, 0, len(c.CustomRedactions))
	for _, item := range c.CustomRedactions {
		copyCfg.CustomRedactions = append(copyCfg.CustomRedactions, MaskSecret(item))
	}
	return copyCfg
}

func MaskSecret(input string) string {
	switch {
	case len(input) < 4:
		return "[redacted]"
	case len(input) <= 8:
		return input[:1] + strings.Repeat("*", len(input)-2) + input[len(input)-1:]
	default:
		return input[:4] + strings.Repeat("*", len(input)-8) + input[len(input)-4:]
	}
}

func (c *Config) normalize() {
	if c.Unknown == nil {
		c.Unknown = make(map[string]json.RawMessage)
	}
	if c.ExcludedGroupings == nil {
		c.ExcludedGroupings = make([]string, 0)
	}
	if c.CustomRedactions == nil {
		c.CustomRedactions = make([]string, 0)
	}
	if c.CustomHandles == nil {
		c.CustomHandles = make([]string, 0)
	}
	if c.PhaseMarker == "" {
		c.PhaseMarker = PhaseInitial
	}
	slices.Sort(c.ExcludedGroupings)
	slices.Sort(c.CustomRedactions)
	slices.Sort(c.CustomHandles)
}
