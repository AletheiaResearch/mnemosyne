package redact

import (
	"math"
	"regexp"
	"strings"
)

var quotedSecretPattern = regexp.MustCompile(`["']([A-Za-z0-9+/=_\-]{20,})["']`)

func redactHighEntropy(input string) (string, int) {
	count := 0
	output := quotedSecretPattern.ReplaceAllStringFunc(input, func(value string) string {
		trimmed := strings.Trim(value, `"'`)
		if entropy(trimmed) < 4.0 || !hasMixedClasses(trimmed) {
			return value
		}
		count++
		return `"` + PlaceholderMarker + `"`
	})
	return output, count
}

func ScanEntropy(input string, limit int) []Finding {
	matches := quotedSecretPattern.FindAllStringSubmatchIndex(input, -1)
	findings := make([]Finding, 0, min(limit, len(matches)))
	for _, idx := range matches {
		value := input[idx[2]:idx[3]]
		if entropy(value) < 4.0 || !hasMixedClasses(value) {
			continue
		}
		start := max(0, idx[0]-24)
		end := min(len(input), idx[1]+24)
		findings = append(findings, Finding{
			Category: "high_entropy",
			Value:    value,
			Context:  strings.ReplaceAll(input[start:end], "\n", " "),
		})
		if len(findings) >= limit {
			break
		}
	}
	return findings
}

func entropy(input string) float64 {
	if input == "" {
		return 0
	}
	freq := make(map[rune]float64)
	for _, r := range input {
		freq[r]++
	}
	var score float64
	size := float64(len(input))
	for _, count := range freq {
		p := count / size
		score -= p * math.Log2(p)
	}
	return score
}

func hasMixedClasses(input string) bool {
	var hasUpper, hasLower, hasDigit bool
	for _, r := range input {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r >= 'a' && r <= 'z':
			hasLower = true
		case r >= '0' && r <= '9':
			hasDigit = true
		}
	}
	return hasUpper && hasLower && hasDigit
}
