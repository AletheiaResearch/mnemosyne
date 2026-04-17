package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newAttestCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "attest",
		Short: "Review an export and record attestation text",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return errors.New("attest is not implemented yet")
		},
	}
}
