package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newPublishCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "publish",
		Short: "Publish an attested export",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errors.New("publish is not implemented yet")
		},
	}
}
