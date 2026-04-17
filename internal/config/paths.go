package config

import (
	"os"
	"path/filepath"
)

const (
	AppName          = "mnemosyne"
	DefaultFileName  = "config.json"
	DefaultOutputDir = "exports"
)

func Dir() (string, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, AppName), nil
}

func File() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, DefaultFileName), nil
}
