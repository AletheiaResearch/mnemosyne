package kimi

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	root string
}

func New(root string) *Source {
	if root == "" {
		root = source.Expand("~/.kimi")
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "kimi"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	sessionRoot := filepath.Join(s.root, "sessions")
	if !source.DirExists(sessionRoot) {
		return nil, nil
	}

	resolved := s.workspaceMap()
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return nil, err
	}

	groupings := make([]source.Grouping, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		digest := entry.Name()
		projectDir := filepath.Join(sessionRoot, digest)
		sessionDirs, err := os.ReadDir(projectDir)
		if err != nil {
			continue
		}
		records := 0
		var bytes int64
		for _, sessionDir := range sessionDirs {
			if !sessionDir.IsDir() {
				continue
			}
			path := filepath.Join(projectDir, sessionDir.Name(), "context.jsonl")
			if !source.FileExists(path) {
				continue
			}
			records++
			info, err := os.Stat(path)
			if err == nil {
				bytes += info.Size()
			}
		}
		if records == 0 {
			continue
		}

		display := digest[:min(8, len(digest))]
		id := digest
		if workspace, ok := resolved[digest]; ok {
			display = filepath.Base(workspace)
			id = workspace
		}
		groupings = append(groupings, source.Grouping{
			ID:               id,
			DisplayLabel:     "kimi:" + display,
			Origin:           s.Name(),
			EstimatedRecords: records,
			EstimatedBytes:   bytes,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	sessionRoot := filepath.Join(s.root, "sessions")
	resolved := s.workspaceMap()

	return filepath.WalkDir(sessionRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || filepath.Base(path) != "context.jsonl" {
			return nil
		}
		projectDigest := filepath.Base(filepath.Dir(filepath.Dir(path)))
		groupID := projectDigest
		workingDir := ""
		if resolvedPath, ok := resolved[projectDigest]; ok {
			groupID = resolvedPath
			workingDir = resolvedPath
		}
		if groupID != grouping.ID {
			return nil
		}

		recordID := filepath.Base(filepath.Dir(path))
		record, err := s.parseSessionFile(path, grouping.DisplayLabel, recordID, workingDir)
		if err != nil {
			return err
		}
		if len(record.Turns) == 0 {
			return nil
		}
		return emit(record)
	})
}

func (s *Source) LookupSession(_ context.Context, sessionID string) (schema.Record, bool, error) {
	sessionRoot := filepath.Join(s.root, "sessions")
	resolved := s.workspaceMap()

	var found schema.Record
	err := filepath.WalkDir(sessionRoot, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry == nil || entry.IsDir() || filepath.Base(path) != "context.jsonl" {
			return nil
		}
		if filepath.Base(filepath.Dir(path)) != sessionID {
			return nil
		}
		projectDigest := filepath.Base(filepath.Dir(filepath.Dir(path)))
		workingDir := resolved[projectDigest]
		record, parseErr := s.parseSessionFile(path, "", sessionID, workingDir)
		if parseErr != nil || len(record.Turns) == 0 {
			return nil
		}
		found = record
		return os.ErrClosed
	})
	switch {
	case err == nil:
		return schema.Record{}, false, nil
	case err == os.ErrClosed:
		return found, true, nil
	default:
		return schema.Record{}, false, err
	}
}

func (s *Source) parseSessionFile(path, grouping, recordID, workingDir string) (schema.Record, error) {
	record := schema.Record{
		RecordID:   recordID,
		Origin:     s.Name(),
		Grouping:   grouping,
		Model:      "kimi-k2",
		WorkingDir: workingDir,
		Turns:      make([]schema.Turn, 0),
	}
	outputTokens := 0

	err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			return nil
		}
		switch source.ExtractString(line, "role") {
		case "user":
			record.Turns = append(record.Turns, schema.Turn{
				Role: "user",
				Text: source.ExtractString(line, "content"),
			})
		case "assistant":
			turn := schema.Turn{Role: "assistant"}
			for _, item := range source.ExtractSlice(line, "content") {
				block, ok := item.(map[string]any)
				if !ok {
					continue
				}
				switch source.ExtractString(block, "type") {
				case "text":
					turn.Text += source.ExtractString(block, "text")
				case "think":
					if turn.Reasoning != "" {
						turn.Reasoning += "\n"
					}
					turn.Reasoning += source.ExtractString(block, "text")
				}
			}
			for _, item := range source.ExtractSlice(line, "tool_calls") {
				callMap, ok := item.(map[string]any)
				if !ok {
					continue
				}
				fn := source.ExtractMap(callMap, "function")
				args := source.JSONString(source.ExtractString(fn, "arguments"))
				turn.ToolCalls = append(turn.ToolCalls, schema.ToolCall{
					Tool:  source.ExtractString(fn, "name"),
					Input: args,
				})
			}
			record.Turns = append(record.Turns, turn)
		case "_usage":
			if count, ok := line["token_count"].(float64); ok && int(count) > outputTokens {
				outputTokens = int(count)
			}
		}
		return nil
	})
	if err != nil {
		return schema.Record{}, err
	}

	record.Usage = schema.Usage{
		UserTurns:      source.CountTurns(record.Turns, "user"),
		AssistantTurns: source.CountTurns(record.Turns, "assistant"),
		ToolCalls:      source.CountToolCalls(record.Turns),
		InputTokens:    0,
		OutputTokens:   outputTokens,
	}
	return record, nil
}

func (s *Source) workspaceMap() map[string]string {
	configPath := filepath.Join(s.root, "kimi.json")
	if !source.FileExists(configPath) {
		return map[string]string{}
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return map[string]string{}
	}
	var payload struct {
		WorkDirs []struct {
			Path string `json:"path"`
		} `json:"work_dirs"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(payload.WorkDirs))
	for _, item := range payload.WorkDirs {
		if strings.TrimSpace(item.Path) == "" {
			continue
		}
		out[source.HashMD5(filepath.Clean(item.Path))] = filepath.Clean(item.Path)
	}
	return out
}
