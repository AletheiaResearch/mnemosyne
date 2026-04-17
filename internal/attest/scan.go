package attest

import (
	"bufio"
	"os"
	"strings"
	"time"

	"github.com/AletheiaResearch/mnemosyne/internal/card"
	"github.com/AletheiaResearch/mnemosyne/internal/redact"
)

type NameScan struct {
	Skipped  bool     `json:"skipped"`
	FullName string   `json:"full_name,omitempty"`
	Count    int      `json:"count"`
	Examples []string `json:"examples,omitempty"`
}

type Report struct {
	FilePath          string          `json:"file_path"`
	Summary           card.Summary    `json:"summary"`
	NameScan          NameScan        `json:"name_scan"`
	Findings          redact.Findings `json:"findings"`
	ManualSampleCount int             `json:"manual_sample_count"`
}

func ScanFile(path string, fullName string, skipName bool) (Report, error) {
	summary, err := card.SummarizeFile(path)
	if err != nil {
		return Report{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()

	detector := redact.NewDetector()
	findings := redact.Findings{}
	nameScan := NameScan{Skipped: skipName, FullName: fullName}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !skipName && fullName != "" && strings.Contains(strings.ToLower(line), strings.ToLower(fullName)) {
			nameScan.Count++
			if len(nameScan.Examples) < 5 {
				nameScan.Examples = append(nameScan.Examples, excerpt(line))
			}
		}

		lineFindings := detector.Scan(line)
		findings.EmailCount += lineFindings.EmailCount
		findings.PublicIPCount += lineFindings.PublicIPCount
		findings.TokenCount += lineFindings.TokenCount
		findings.Emails = appendLimit(findings.Emails, lineFindings.Emails, 20)
		findings.PublicIPs = appendLimit(findings.PublicIPs, lineFindings.PublicIPs, 20)
		findings.Tokens = appendLimit(findings.Tokens, lineFindings.Tokens, 20)
		findings.HighEntropy = appendFindingLimit(findings.HighEntropy, lineFindings.HighEntropy, 15)
	}

	return Report{
		FilePath: path,
		Summary:  summary,
		NameScan: nameScan,
		Findings: findings,
	}, scanner.Err()
}

func excerpt(input string) string {
	if len(input) <= 160 {
		return input
	}
	return input[:157] + "..."
}

func appendLimit(dst, src []string, limit int) []string {
	for _, item := range src {
		if len(dst) >= limit {
			break
		}
		dst = append(dst, item)
	}
	return dst
}

func appendFindingLimit(dst, src []redact.Finding, limit int) []redact.Finding {
	for _, item := range src {
		if len(dst) >= limit {
			break
		}
		dst = append(dst, item)
	}
	return dst
}

func DetectFileChange(path string, size int64, attestedAt string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	if size > 0 && info.Size() != size {
		return true
	}
	if attestedAt != "" {
		if ts, err := time.Parse(time.RFC3339Nano, attestedAt); err == nil && info.ModTime().After(ts) {
			return true
		}
	}
	return false
}
