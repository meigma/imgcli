package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newPlanCommand(_ *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "plan CONFIG",
		Short: "Print the resolved artifact plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("plan command is not implemented yet")
		},
	}
}
