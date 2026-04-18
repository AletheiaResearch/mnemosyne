package cli

import (
	"github.com/spf13/cobra"

	"github.com/AletheiaResearch/mnemosyne/internal/serialize"
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

func newTemplatesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "templates",
		Short: "List built-in chat templates available to transform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			names := serialize.BuiltinTemplateNames()
			rows := make([]map[string]string, 0, len(names))
			for _, name := range names {
				tmpl, err := serialize.NewBuiltinTemplate(name, serialize.TemplateOptions{})
				if err != nil {
					continue
				}
				rows = append(rows, map[string]string{
					"name":        name,
					"description": tmpl.Description(),
				})
			}
			return printJSON(cmd.OutOrStdout(), rows)
		},
	}
}
