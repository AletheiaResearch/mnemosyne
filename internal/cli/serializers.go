package cli

import (
	"github.com/spf13/cobra"

	"github.com/Quantumlyy/mnemosyne/internal/serialize"
)

func newSerializersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serializers",
		Short: "List available serializer formats",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rows := make([]map[string]string, 0, len(serialize.Registry()))
			for _, serializer := range serialize.Registry() {
				rows = append(rows, map[string]string{
					"name":        serializer.Name(),
					"description": serializer.Description(),
				})
			}
			return printJSON(cmd.OutOrStdout(), rows)
		},
	}
}
