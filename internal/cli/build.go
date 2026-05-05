package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/cache"
	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/providers/incusos/cdn"
	"github.com/meigma/imgcli/internal/providers/incusos/imagefile"
	"github.com/meigma/imgcli/schemas/core"
)

const defaultBuildOutputDir = "dist"

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

			ports, err := rt.incusOSBuildPorts()
			if err != nil {
				return err
			}

			provider := incusosprovider.New(*config.Incusos, incusosprovider.Options{
				Catalog:       ports.catalog,
				Downloader:    ports.downloader,
				SeedBuilder:   ports.seedBuilder,
				ImageInjector: ports.imageInjector,
			})

			result, err := provider.Build(cmd.Context(), providers.BuildRequest{
				Plan: providers.Plan{
					Image: config.Image,
				},
				OutputDir: buildOutputDir(config.Output),
			})
			if err != nil {
				return err
			}

			for _, artifact := range result.Artifacts {
				if _, err := fmt.Fprintln(rt.opts.stdout(), artifact.Path); err != nil {
					return fmt.Errorf("write build artifact path: %w", err)
				}
			}

			return nil
		},
	}
}

type incusOSBuildPorts struct {
	catalog       incusosprovider.Catalog
	downloader    incusosprovider.Downloader
	seedBuilder   incusosprovider.SeedBuilder
	imageInjector incusosprovider.ImageInjector
}

func (rt *runtime) incusOSBuildPorts() (incusOSBuildPorts, error) {
	ports := incusOSBuildPorts{
		catalog:       rt.opts.IncusOSCatalog,
		downloader:    rt.opts.IncusOSDownloader,
		seedBuilder:   rt.opts.IncusOSSeedBuilder,
		imageInjector: rt.opts.IncusOSImageInjector,
	}

	if ports.catalog == nil || ports.downloader == nil {
		cacheStore, err := cache.NewDiskStore()
		if err != nil {
			return incusOSBuildPorts{}, fmt.Errorf("configure incusos cache: %w", err)
		}

		client := cdn.NewClient(cdn.WithCacheService(cacheStore))
		if ports.catalog == nil {
			ports.catalog = client
		}
		if ports.downloader == nil {
			ports.downloader = client
		}
	}

	if ports.seedBuilder == nil {
		ports.seedBuilder = incusosprovider.SeedArchiveBuilder{}
	}
	if ports.imageInjector == nil {
		ports.imageInjector = imagefile.Injector{}
	}

	return ports, nil
}

func buildOutputDir(output *core.OutputDefaults) string {
	if output == nil || output.Dir == "" {
		return defaultBuildOutputDir
	}

	return output.Dir
}
