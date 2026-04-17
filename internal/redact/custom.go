package redact

import (
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

type customDetectorFile struct {
	Patterns []struct {
		Name  string `yaml:"name"`
		Regex string `yaml:"regex"`
	} `yaml:"patterns"`
}

func LoadCustomPatterns(path string) ([]*regexp.Regexp, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var file customDetectorFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, err
	}
	out := make([]*regexp.Regexp, 0, len(file.Patterns))
	for _, item := range file.Patterns {
		re, err := regexp.Compile(item.Regex)
		if err != nil {
			return nil, err
		}
		out = append(out, re)
	}
	return out, nil
}
