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
			serializers := make([]map[string]string, 0, len(serialize.Registry()))
			for _, s := range serialize.Registry() {
				serializers = append(serializers, map[string]string{
					"name":        s.Name(),
					"description": s.Description(),
				})
			}

			builtinTemplates := make([]map[string]string, 0)
			for _, name := range serialize.BuiltinTemplateNames() {
				tmpl, err := serialize.NewBuiltinTemplate(name, serialize.TemplateOptions{})
				if err != nil {
					continue
				}
				builtinTemplates = append(builtinTemplates, map[string]string{
					"name":        name,
					"description": tmpl.Description(),
				})
			}

			return printJSON(cmd.OutOrStdout(), map[string]any{
				"serializers":       serializers,
				"builtin_templates": builtinTemplates,
			})
		},
	}
}
