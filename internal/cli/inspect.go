package cli

import (
	"errors"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/source"
)

func newInspectCommand(rt *runtime) *cobra.Command {
	var scope string

	return &cobra.Command{
		Use:   "inspect",
		Short: "Enumerate detected groupings for a source scope",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig(rt.configPath)
			if err != nil {
				return err
			}
			scope = strings.TrimSpace(firstNonEmpty(scope, cfg.OriginScope))
			if scope == "" {
				return errors.New("inspect requires --scope or a configured origin scope")
			}
			selected, err := selectedSources(scope)
			if err != nil {
				return err
			}

			excluded := make(map[string]struct{}, len(cfg.ExcludedGroupings))
			for _, item := range cfg.ExcludedGroupings {
				excluded[item] = struct{}{}
			}

			groupings := make([]source.Grouping, 0)
			for _, src := range selected {
				items, err := src.Discover(cmd.Context())
				if err != nil {
					return err
				}
				for _, item := range items {
					_, item.Excluded = excluded[item.DisplayLabel]
					groupings = append(groupings, item)
				}
			}
			sort.Slice(groupings, func(i, j int) bool {
				return groupings[i].DisplayLabel < groupings[j].DisplayLabel
			})
			return printJSON(cmd.OutOrStdout(), groupings)
		},
	}
}
