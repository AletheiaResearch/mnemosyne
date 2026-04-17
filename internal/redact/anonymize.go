package redact

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type Anonymizer struct {
	homeDir         string
	homeParent      string
	username        string
	usernameToken   string
	homePrefix      string
	textMatchers    []matcher
	shortPathTokens map[string]string
}

type matcher struct {
	re          *regexp.Regexp
	replacement string
}

func NewAnonymizer(handles []string) (*Anonymizer, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = ""
	}
	username := filepath.Base(homeDir)
	if username == "." || username == string(filepath.Separator) {
		username = ""
	}

	a := &Anonymizer{
		homeDir:         filepath.Clean(homeDir),
		homeParent:      filepath.Dir(filepath.Clean(homeDir)),
		username:        username,
		usernameToken:   tokenFor(username),
		shortPathTokens: make(map[string]string),
	}
	if a.homeDir != "" && a.homeParent != "" && a.homeParent != "." {
		a.homePrefix = filepath.Join(a.homeParent, a.usernameToken)
	}

	if username != "" {
		a.addIdentifier(username)
	}
	for _, handle := range handles {
		a.addIdentifier(strings.TrimSpace(handle))
	}
	return a, nil
}

func (a *Anonymizer) ApplyText(input string, count int) (string, int) {
	if input == "" {
		return input, count
	}

	out := input
	if a.homeDir != "" {
		replaced, matches := stringsReplaceAll(out, a.homeDir, a.homePrefix)
		out = replaced
		count += matches
		replaced, matches = stringsReplaceAll(out, filepath.ToSlash(a.homeDir), filepath.ToSlash(a.homePrefix))
		out = replaced
		count += matches
	}
	out, count = a.applyShortPathTokens(out, count)

	for _, matcher := range a.textMatchers {
		before := out
		out = matcher.re.ReplaceAllString(out, matcher.replacement)
		if out != before {
			count++
		}
	}
	return out, count
}

func (a *Anonymizer) ApplyPath(input string, count int) (string, int) {
	if input == "" {
		return input, count
	}
	clean := filepath.Clean(input)
	if a.homeDir != "" && strings.HasPrefix(clean, a.homeDir) {
		out := a.homePrefix + strings.TrimPrefix(clean, a.homeDir)
		return a.applyShortPathTokens(out, count+1)
	}
	if a.homeDir != "" && strings.HasPrefix(filepath.ToSlash(clean), filepath.ToSlash(a.homeDir)) {
		out := filepath.ToSlash(a.homePrefix) + strings.TrimPrefix(filepath.ToSlash(clean), filepath.ToSlash(a.homeDir))
		return a.applyShortPathTokens(out, count+1)
	}
	return a.ApplyText(input, count)
}

func (a *Anonymizer) ApplyURL(input string, count int) (string, int) {
	if input == "" {
		return input, count
	}
	parsed, err := url.Parse(input)
	if err != nil || parsed.Path == "" {
		return a.ApplyText(input, count)
	}
	updated, nextCount := a.ApplyPath(parsed.Path, count)
	parsed.Path = updated
	return parsed.String(), nextCount
}

func (a *Anonymizer) addIdentifier(identifier string) {
	if identifier == "" {
		return
	}
	token := tokenFor(identifier)
	if len(identifier) < 4 {
		a.shortPathTokens[identifier] = token
		return
	}
	pattern := regexp.MustCompile(`(?i)(^|[^A-Za-z0-9_])(` + regexp.QuoteMeta(identifier) + `)([^A-Za-z0-9_]|$)`)
	a.textMatchers = append(a.textMatchers, matcher{
		re:          pattern,
		replacement: "${1}" + token + "${3}",
	})
}

func (a *Anonymizer) applyShortPathTokens(input string, count int) (string, int) {
	if len(a.shortPathTokens) == 0 || input == "" {
		return input, count
	}

	out := input
	for identifier, token := range a.shortPathTokens {
		for _, pattern := range shortIdentifierPathPatterns(identifier) {
			replaced, matches := stringsReplaceAll(out, pattern, token)
			out = replaced
			count += matches
		}
	}
	return out, count
}

func shortIdentifierPathPatterns(identifier string) []string {
	return []string{
		"/Users/" + identifier + "/",
		"/home/" + identifier + "/",
		`C:\Users\` + identifier + `\`,
	}
}

func tokenFor(input string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(input))))
	return "anon_" + hex.EncodeToString(sum[:])[:10]
}
