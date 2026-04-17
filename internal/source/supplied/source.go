package supplied

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/AletheiaResearch/mnemosyne/internal/config"
	"github.com/AletheiaResearch/mnemosyne/internal/schema"
	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

type Source struct {
	root string
}

func New(root string) *Source {
	if root == "" {
		cfgDir, err := config.Dir()
		if err == nil {
			root = filepath.Join(cfgDir, "supplied")
		}
	}
	return &Source{root: root}
}

func (s *Source) Name() string {
	return "supplied"
}

func (s *Source) Discover(context.Context) ([]source.Grouping, error) {
	if !source.DirExists(s.root) {
		return nil, nil
	}
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, err
	}

	groupings := make([]source.Grouping, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.root, entry.Name())
		files, err := source.CollectFiles(dir, func(path string, _ os.DirEntry) bool {
			return filepath.Ext(path) == ".jsonl"
		})
		if err != nil || len(files) == 0 {
			continue
		}

		records := 0
		var bytes int64
		for _, path := range files {
			info, err := os.Stat(path)
			if err == nil {
				bytes += info.Size()
			}
			_ = source.ReadJSONLines(path, func(_ int, _ []byte) error {
				records++
				return nil
			})
		}

		groupings = append(groupings, source.Grouping{
			ID:               entry.Name(),
			DisplayLabel:     "supplied:" + entry.Name(),
			Origin:           s.Name(),
			EstimatedRecords: records,
			EstimatedBytes:   bytes,
		})
	}
	return groupings, nil
}

func (s *Source) Extract(ctx context.Context, grouping source.Grouping, _ source.ExtractionContext, emit func(schema.Record) error) error {
	dir := filepath.Join(s.root, grouping.ID)
	files, err := source.CollectFiles(dir, func(path string, _ os.DirEntry) bool {
		return filepath.Ext(path) == ".jsonl"
	})
	if err != nil {
		return err
	}

	for _, path := range files {
		if err := source.ReadJSONLines(path, func(_ int, raw []byte) error {
			var record schema.Record
			if err := json.Unmarshal(raw, &record); err != nil {
				return nil
			}
			if strings.TrimSpace(record.RecordID) == "" || strings.TrimSpace(record.Model) == "" || len(record.Turns) == 0 {
				return nil
			}
			record.Origin = s.Name()
			record.Grouping = grouping.DisplayLabel
			return emit(record)
		}); err != nil {
			return err
		}
	}
	return nil
}
