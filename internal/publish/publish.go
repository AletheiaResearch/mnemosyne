package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Identity struct {
	Username string `json:"username"`
}

func DetectIdentity() (Identity, error) {
	if _, err := exec.LookPath("hf"); err != nil {
		return Identity{}, err
	}
	cmd := exec.Command("hf", "auth", "whoami", "--format", "json")
	out, err := cmd.Output()
	if err != nil {
		return Identity{}, err
	}
	var payload map[string]any
	if err := json.Unmarshal(out, &payload); err != nil {
		return Identity{}, err
	}
	for _, key := range []string{"name", "username", "user"} {
		if value, ok := payload[key].(string); ok && value != "" {
			return Identity{Username: value}, nil
		}
	}
	return Identity{}, errors.New("unable to detect hugging face username")
}

func EnsureDatasetRepo(repoID string) error {
	cmd := exec.Command("hf", "repos", "create", repoID, "--repo-type", "dataset", "--exist-ok")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func UploadFile(repoID, localPath, pathInRepo, message string) error {
	args := []string{"upload", repoID, localPath, pathInRepo, "--repo-type", "dataset", "--quiet"}
	if message != "" {
		args = append(args, "--commit-message", message)
	}
	cmd := exec.Command("hf", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func DatasetURL(repoID string) string {
	return "https://huggingface.co/datasets/" + strings.TrimSpace(repoID)
}

// DownloadFile fetches a single file from a HuggingFace dataset repo to
// localPath. Returns (true, nil) when the file was downloaded, (false, nil)
// when the remote reports the file does not exist (treated as an empty
// starting state), and (false, err) for transport or auth failures.
func DownloadFile(repoID, pathInRepo, localPath string) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
		return false, err
	}
	dir, err := os.MkdirTemp("", "mnemosyne-hf-download-*")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(dir)

	args := []string{
		"download", repoID, pathInRepo,
		"--repo-type", "dataset",
		"--local-dir", dir,
		"--quiet",
	}
	cmd := exec.Command("hf", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		combined := string(out)
		if isHFNotFound(combined) {
			return false, nil
		}
		return false, fmt.Errorf("%w: %s", err, combined)
	}

	downloaded := filepath.Join(dir, pathInRepo)
	data, err := os.ReadFile(downloaded)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := os.WriteFile(localPath, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func isHFNotFound(output string) bool {
	lower := strings.ToLower(output)
	markers := []string{
		"entrynotfounderror",
		"404 client error",
		"404 not found",
		"repository not found",
		"does not exist",
	}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
