package attest

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	MinimumAttestationLength = 20
	MinimumManualSampleCount = 15
)

type ValidationError struct {
	Checks []string `json:"checks"`
}

func (e ValidationError) Error() string {
	return strings.Join(e.Checks, "; ")
}

func ValidateStatements(fullName string, skipName bool, identity, entity, manual string) error {
	failures := make([]string, 0)
	if len(strings.TrimSpace(identity)) < MinimumAttestationLength {
		failures = append(failures, "identity attestation is too short")
	}
	if len(strings.TrimSpace(entity)) < MinimumAttestationLength {
		failures = append(failures, "entity attestation is too short")
	}
	if len(strings.TrimSpace(manual)) < MinimumAttestationLength {
		failures = append(failures, "manual review attestation is too short")
	}

	lowerIdentity := strings.ToLower(identity)
	if skipName {
		if !containsAny(lowerIdentity, "declin", "skip") {
			failures = append(failures, "identity attestation must mention the skipped full-name check")
		}
	} else {
		if strings.TrimSpace(fullName) == "" {
			failures = append(failures, "full name is required unless the name scan is skipped")
		}
		for _, part := range strings.Fields(strings.ToLower(fullName)) {
			if !strings.Contains(lowerIdentity, part) {
				failures = append(failures, "identity attestation must include every word of the supplied full name")
				break
			}
		}
		if !containsAny(lowerIdentity, "scan", "searched", "grep") {
			failures = append(failures, "identity attestation must mention scanning the export")
		}
	}

	lowerEntity := strings.ToLower(entity)
	if !containsAny(lowerEntity, "company", "client", "project", "url", "domain", "tool", "third-party") {
		failures = append(failures, "entity attestation must mention a sensitive-entity category")
	}
	if !containsAny(lowerEntity, "none", "found", "added", "redaction", "redacted") {
		failures = append(failures, "entity attestation must mention the interview outcome")
	}

	sampleCount := ParseManualSampleCount(manual)
	if sampleCount < MinimumManualSampleCount {
		failures = append(failures, fmt.Sprintf("manual review attestation must include a sample count of at least %d", MinimumManualSampleCount))
	}
	if !containsAny(strings.ToLower(manual), "manual", "review", "scan") {
		failures = append(failures, "manual review attestation must mention a manual review")
	}

	if len(failures) > 0 {
		return ValidationError{Checks: failures}
	}
	return nil
}

func ValidatePublishAttestation(input string) error {
	failures := make([]string, 0)
	lower := strings.ToLower(strings.TrimSpace(input))
	if len(lower) < MinimumAttestationLength {
		failures = append(failures, "publish attestation is too short")
	}
	if !containsAny(lower, "approved", "approve", "consent") {
		failures = append(failures, "publish attestation must include approval language")
	}
	if !containsAny(lower, "publish", "upload", "transmit") {
		failures = append(failures, "publish attestation must include transmission language")
	}
	if len(failures) > 0 {
		return ValidationError{Checks: failures}
	}
	return nil
}

func ParseManualSampleCount(input string) int {
	re := regexp.MustCompile(`\b(\d+)\b`)
	match := re.FindStringSubmatch(input)
	if len(match) < 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

func containsAny(input string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(input, value) {
			return true
		}
	}
	return false
}
