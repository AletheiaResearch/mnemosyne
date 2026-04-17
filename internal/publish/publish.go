package publish

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
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
