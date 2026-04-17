package source

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	_ "modernc.org/sqlite"
)

const maxJSONLineBytes = 64 * 1024 * 1024

func HomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

func Expand(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(HomeDir(), path[2:])
	}
	return path
}

func DirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func FileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func ReadJSONLines(path string, fn func(line int, raw []byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), maxJSONLineBytes)
	line := 0
	for scanner.Scan() {
		line++
		raw := bytesTrim(scanner.Bytes())
		if len(raw) == 0 {
			continue
		}
		if err := fn(line, slices.Clone(raw)); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return fmt.Errorf("%s: jsonl line exceeds %d bytes: %w", path, maxJSONLineBytes, err)
		}
		return err
	}
	return nil
}

func DecodeJSONObject(raw []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func NormalizeTimestamp(value any) string {
	switch typed := value.(type) {
	case string:
		if typed == "" {
			return ""
		}
		if ts, err := time.Parse(time.RFC3339, typed); err == nil {
			return ts.UTC().Format(time.RFC3339)
		}
		for _, layout := range []string{
			time.RFC3339Nano,
			"2006-01-02T15:04:05.000Z07:00",
			"2006-01-02 15:04:05",
		} {
			if ts, err := time.Parse(layout, typed); err == nil {
				return ts.UTC().Format(time.RFC3339)
			}
		}
		if ms, err := strconv.ParseInt(typed, 10, 64); err == nil {
			return time.UnixMilli(ms).UTC().Format(time.RFC3339)
		}
	case float64:
		return time.UnixMilli(int64(typed)).UTC().Format(time.RFC3339)
	case int64:
		return time.UnixMilli(typed).UTC().Format(time.RFC3339)
	case json.Number:
		if ms, err := typed.Int64(); err == nil {
			return time.UnixMilli(ms).UTC().Format(time.RFC3339)
		}
	}
	return ""
}

func EstimateUnknownLabel(origin string) string {
	return origin + ":unknown"
}

func DisplayLabelFromPath(path string) string {
	path = filepath.Clean(path)
	home := HomeDir()
	if home != "" && strings.HasPrefix(path, home) {
		path = strings.TrimPrefix(path, home)
		path = strings.TrimPrefix(path, string(filepath.Separator))
	}
	parts := strings.FieldsFunc(path, func(r rune) bool { return r == filepath.Separator || r == '/' })
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		switch part {
		case "", "Documents", "Downloads", "Desktop":
		default:
			filtered = append(filtered, part)
		}
	}
	if len(filtered) == 0 {
		return filepath.Base(path)
	}
	return strings.Join(filtered, "/")
}

func HashSHA256(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func HashMD5(value string) string {
	sum := md5.Sum([]byte(value))
	return hex.EncodeToString(sum[:])
}

func JSONString(input string) any {
	var out any
	if err := json.Unmarshal([]byte(input), &out); err == nil {
		return out
	}
	return input
}

func CollectFiles(root string, match func(path string, entry fs.DirEntry) bool) ([]string, error) {
	if !DirExists(root) {
		return nil, nil
	}
	files := make([]string, 0)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if match(path, entry) {
			files = append(files, path)
		}
		return nil
	})
	slices.Sort(files)
	return files, err
}

func OpenSQLite(path string) (*sql.DB, error) {
	if !FileExists(path) {
		return nil, os.ErrNotExist
	}
	dsn := fmt.Sprintf("file:%s?mode=ro", path)
	return sql.Open("sqlite", dsn)
}

func LatestTimestamp(values ...string) string {
	var latest time.Time
	for _, value := range values {
		if value == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		if ts.After(latest) {
			latest = ts
		}
	}
	if latest.IsZero() {
		return ""
	}
	return latest.UTC().Format(time.RFC3339)
}

func EarliestTimestamp(values ...string) string {
	var earliest time.Time
	for _, value := range values {
		if value == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339, value)
		if err != nil {
			continue
		}
		if earliest.IsZero() || ts.Before(earliest) {
			earliest = ts
		}
	}
	if earliest.IsZero() {
		return ""
	}
	return earliest.UTC().Format(time.RFC3339)
}

func WithCause(ctx context.Context, cause error) context.Context {
	if cause == nil {
		return ctx
	}
	return context.WithValue(ctx, contextKey("cause"), cause)
}

func CauseFromContext(ctx context.Context) error {
	value := ctx.Value(contextKey("cause"))
	if err, ok := value.(error); ok {
		return err
	}
	return nil
}

func ExtractString(input map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := input[key]; ok {
			switch typed := value.(type) {
			case string:
				return typed
			case json.Number:
				return typed.String()
			}
		}
	}
	return ""
}

func ExtractMap(input map[string]any, key string) map[string]any {
	if input == nil {
		return nil
	}
	if value, ok := input[key]; ok {
		if out, ok := value.(map[string]any); ok {
			return out
		}
	}
	return nil
}

func ExtractSlice(input map[string]any, key string) []any {
	if input == nil {
		return nil
	}
	if value, ok := input[key]; ok {
		if out, ok := value.([]any); ok {
			return out
		}
	}
	return nil
}

func bytesTrim(input []byte) []byte {
	return []byte(strings.TrimSpace(string(input)))
}

func Optional(err error) error {
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

type contextKey string

func CountTurns(turns []schema.Turn, role string) int {
	count := 0
	for _, turn := range turns {
		if turn.Role == role {
			count++
		}
	}
	return count
}

func CountToolCalls(turns []schema.Turn) int {
	count := 0
	for _, turn := range turns {
		count += len(turn.ToolCalls)
	}
	return count
}
