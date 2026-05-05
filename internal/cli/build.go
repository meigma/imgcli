package cli

import (
	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/providers/incusos/cdn"
)

func newBuildCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "build CONFIG",
		Short: "Build disk image artifacts from configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadImageConfig(args[0])
			if err != nil {
				return err
			}

			catalog := rt.opts.IncusOSCatalog
			if catalog == nil {
				catalog = cdn.NewClient()
			}

			provider := incusosprovider.New(*config.Incusos, incusosprovider.Options{
				Catalog: catalog,
				Output:  rt.opts.stdout(),
			})

			_, err = provider.Build(cmd.Context(), providers.BuildRequest{})
			return err
		},
	}
}
