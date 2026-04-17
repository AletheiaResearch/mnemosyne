package redact

import (
	"bytes"
	"os/exec"
)

func TrufflehogAvailable() bool {
	_, err := exec.LookPath("trufflehog")
	return err == nil
}

func TrufflehogScan(input string) (string, error) {
	if !TrufflehogAvailable() || input == "" {
		return "", nil
	}
	cmd := exec.Command("trufflehog", "filesystem", "--json", "--only-verified=false", "--path", "-")
	cmd.Stdin = bytes.NewBufferString(input)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), err
	}
	return string(out), nil
}
