package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the imgcli version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), rt.opts.version())
			return err
		},
	}
}
