package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newPublishCommand(_ *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "publish CONFIG",
		Short: "Build and publish disk image artifacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("publish command is not implemented yet")
		},
	}
}
