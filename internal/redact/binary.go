package redact

import "strings"

func LooksLikeLargeBinary(input string) bool {
	if len(input) < 4096 {
		return false
	}
	if strings.HasPrefix(input, "data:") && strings.Contains(input, ";base64,") {
		return true
	}
	for _, r := range input {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '+' || r == '/' || r == '=' || r == '\n' || r == '\r':
		default:
			return false
		}
	}
	return true
}
