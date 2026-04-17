package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newExtractCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "extract",
		Short: "Extract local records into canonical JSONL",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errors.New("extract is not implemented yet")
		},
	}
}
