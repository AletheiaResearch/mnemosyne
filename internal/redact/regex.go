package redact

import (
	"net"
	"regexp"
	"strings"
)

type Detector struct {
	patterns []replacementPattern
}

type replacementPattern struct {
	category string
	regex    *regexp.Regexp
	group    int
	allow    func(string) bool
}

type Findings struct {
	EmailCount    int       `json:"email_count"`
	PublicIPCount int       `json:"public_ip_count"`
	TokenCount    int       `json:"token_count"`
	Emails        []string  `json:"emails,omitempty"`
	PublicIPs     []string  `json:"public_ips,omitempty"`
	Tokens        []string  `json:"tokens,omitempty"`
	HighEntropy   []Finding `json:"high_entropy,omitempty"`
}

type Finding struct {
	Category string `json:"category"`
	Value    string `json:"value"`
	Context  string `json:"context"`
}

func NewDetector() *Detector {
	return &Detector{
		patterns: []replacementPattern{
			{category: "jwt", regex: regexp.MustCompile(`\beyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)?\b`), group: 0},
			{category: "db_url", regex: regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:[^/\s@]+@[^/\s]+`), group: 0},
			{category: "cloud_key", regex: regexp.MustCompile(`\b(?:sk-[A-Za-z0-9]{16,}|gh[pousr]_[A-Za-z0-9_]{20,}|AKIA[0-9A-Z]{16}|xox[baprs]-[A-Za-z0-9-]{10,}|hf_[A-Za-z0-9]{20,}|npm_[A-Za-z0-9]{20,})\b`), group: 0},
			{category: "pem", regex: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`), group: 0},
			{category: "cli_flag", regex: regexp.MustCompile(`(?i)(--(?:token|api[-_]?key|password|secret)(?:=|\s+))([^\s]+)`), group: 2},
			{category: "env_assign", regex: regexp.MustCompile(`\b([A-Z0-9_]*(?:TOKEN|SECRET|PASSWORD|API[_-]?KEY|ACCESS[_-]?KEY)[A-Z0-9_]*=)([^\s'"]+)`), group: 2},
			{category: "generic_assign", regex: regexp.MustCompile(`(?i)("?(?:token|secret|password|api[_-]?key|access[_-]?key)"?\s*[:=]\s*"?)([^",\s}]+)`), group: 2},
			{category: "password_value", regex: regexp.MustCompile(`(?i)(password[^:\n]{0,20}[:=]\s*)([^\s]+)`), group: 2},
			{category: "query_secret", regex: regexp.MustCompile(`(?i)([?&](?:token|key|secret|api[_-]?key)=)([^&#\s]+)`), group: 2},
			{category: "bearer", regex: regexp.MustCompile(`(?i)(authorization\s*:\s*bearer\s+)([^\s]+)`), group: 2},
			{category: "webhook", regex: regexp.MustCompile(`https://hooks\.slack\.com/services/[A-Za-z0-9/_-]+`), group: 0},
			{category: "wallet_key", regex: regexp.MustCompile(`\b0x[a-fA-F0-9]{64}\b`), group: 0},
			{category: "email", regex: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`), group: 0, allow: allowEmail},
			{category: "public_ip", regex: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`), group: 0, allow: allowIP},
		},
	}
}

func (d *Detector) Redact(input string) (string, int) {
	out := input
	count := 0
	for _, pattern := range d.patterns {
		replaced, matches := replacePattern(out, pattern)
		out = replaced
		count += matches
	}
	replaced, matches := redactHighEntropy(out)
	out = replaced
	count += matches
	return out, count
}

func (d *Detector) Scan(input string) Findings {
	findings := Findings{}
	for _, pattern := range d.patterns {
		matches := pattern.regex.FindAllStringSubmatch(input, -1)
		for _, match := range matches {
			value := match[pattern.group]
			if pattern.allow != nil && pattern.allow(value) {
				continue
			}
			switch pattern.category {
			case "email":
				findings.EmailCount++
				if len(findings.Emails) < 20 {
					findings.Emails = append(findings.Emails, value)
				}
			case "public_ip":
				findings.PublicIPCount++
				if len(findings.PublicIPs) < 20 {
					findings.PublicIPs = append(findings.PublicIPs, value)
				}
			default:
				findings.TokenCount++
				if len(findings.Tokens) < 20 {
					findings.Tokens = append(findings.Tokens, value)
				}
			}
		}
	}
	findings.HighEntropy = ScanEntropy(input, 15)
	return findings
}

func (f Findings) Empty() bool {
	return f.EmailCount == 0 && f.PublicIPCount == 0 && f.TokenCount == 0 && len(f.HighEntropy) == 0
}

func replacePattern(input string, pattern replacementPattern) (string, int) {
	count := 0
	if pattern.group == 0 {
		output := pattern.regex.ReplaceAllStringFunc(input, func(value string) string {
			if pattern.allow != nil && pattern.allow(value) {
				return value
			}
			count++
			return PlaceholderMarker
		})
		return output, count
	}

	indexes := pattern.regex.FindAllStringSubmatchIndex(input, -1)
	if len(indexes) == 0 {
		return input, 0
	}

	var builder strings.Builder
	last := 0
	for _, idx := range indexes {
		start, end := idx[pattern.group*2], idx[pattern.group*2+1]
		if start < 0 || end < 0 {
			continue
		}
		value := input[start:end]
		if pattern.allow != nil && pattern.allow(value) {
			continue
		}
		builder.WriteString(input[last:start])
		builder.WriteString(PlaceholderMarker)
		last = end
		count++
	}
	builder.WriteString(input[last:])
	return builder.String(), count
}

func allowEmail(value string) bool {
	lower := strings.ToLower(value)
	at := strings.LastIndex(lower, "@")
	if at < 0 {
		return false
	}
	return isReservedEmailDomain(lower[at+1:])
}

func isReservedEmailDomain(domain string) bool {
	if domain == "" {
		return false
	}
	switch domain {
	case "localhost", "example.com", "example.org", "example.net":
		return true
	}
	return strings.HasSuffix(domain, ".localhost") ||
		strings.HasSuffix(domain, ".example") ||
		strings.HasSuffix(domain, ".test") ||
		strings.HasSuffix(domain, ".invalid")
}

var privateIPBlocks = func() []*net.IPNet {
	cidrs := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
	}
	blocks := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		if _, block, err := net.ParseCIDR(cidr); err == nil {
			blocks = append(blocks, block)
		}
	}
	return blocks
}()

func allowIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	switch value {
	case "1.1.1.1", "1.0.0.1", "8.8.8.8", "8.8.4.4", "9.9.9.9":
		return true
	default:
		return false
	}
}
