package cli

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/cache"
	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/providers/incusos/cdn"
	"github.com/meigma/imgcli/internal/providers/incusos/imagefile"
	imgschemas "github.com/meigma/imgcli/schemas"
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

			if rt.usesDefaultIncusOSCache() {
				return withLockedCache(cmd.Context(), rt.config, func(
					catalog incusosprovider.Catalog,
					downloader incusosprovider.Downloader,
				) error {
					ports, portsErr := rt.incusOSBuildPorts(catalog, downloader)
					if portsErr != nil {
						return portsErr
					}
					return runIncusOSBuild(cmd.Context(), config, ports, rt.opts.stdout())
				})
			}

			ports, err := rt.incusOSBuildPorts(nil, nil)
			if err != nil {
				return err
			}
			return runIncusOSBuild(cmd.Context(), config, ports, rt.opts.stdout())
		},
	}
}

type incusOSBuildPorts struct {
	catalog       incusosprovider.Catalog
	downloader    incusosprovider.Downloader
	seedBuilder   incusosprovider.SeedBuilder
	imageInjector incusosprovider.ImageInjector
}

func (rt *runtime) usesDefaultIncusOSCache() bool {
	return rt.opts.IncusOSCatalog == nil && rt.opts.IncusOSDownloader == nil
}

func (rt *runtime) incusOSBuildPorts(
	defaultCatalog incusosprovider.Catalog,
	defaultDownloader incusosprovider.Downloader,
) (incusOSBuildPorts, error) {
	ports := incusOSBuildPorts{
		catalog:       rt.opts.IncusOSCatalog,
		downloader:    rt.opts.IncusOSDownloader,
		seedBuilder:   rt.opts.IncusOSSeedBuilder,
		imageInjector: rt.opts.IncusOSImageInjector,
	}

	if ports.catalog == nil {
		ports.catalog = defaultCatalog
	}
	if ports.downloader == nil {
		if typedDownloader, ok := ports.catalog.(incusosprovider.Downloader); ok {
			ports.downloader = typedDownloader
		} else {
			ports.downloader = defaultDownloader
		}
	}
	if ports.catalog == nil {
		return incusOSBuildPorts{}, errors.New("configure incusos catalog: catalog is required")
	}
	if ports.downloader == nil {
		return incusOSBuildPorts{}, errors.New("configure incusos downloader: downloader is required")
	}

	if ports.seedBuilder == nil {
		ports.seedBuilder = incusosprovider.SeedArchiveBuilder{}
	}
	if ports.imageInjector == nil {
		ports.imageInjector = imagefile.Injector{}
	}

	return ports, nil
}

func runIncusOSBuild(
	ctx context.Context,
	config imgschemas.Config,
	ports incusOSBuildPorts,
	output io.Writer,
) error {
	provider := incusosprovider.New(*config.Incusos, incusosprovider.Options{
		Catalog:       ports.catalog,
		Downloader:    ports.downloader,
		SeedBuilder:   ports.seedBuilder,
		ImageInjector: ports.imageInjector,
	})

	result, err := provider.Build(ctx, providers.BuildRequest{
		Plan: providers.Plan{
			Image: config.Image,
		},
		OutputDir: buildOutputDir(config.Output),
	})
	if err != nil {
		return err
	}

	for _, artifact := range result.Artifacts {
		if _, err := fmt.Fprintln(output, artifact.Path); err != nil {
			return fmt.Errorf("write build artifact path: %w", err)
		}
	}

	return nil
}

func newCacheStore(cfg Config) (*cache.DiskStore, error) {
	options := []cache.Option{
		cache.WithMaxSizeBytes(cfg.CacheMaxSizeBytes),
	}
	if cfg.CacheDir != "" {
		options = append(options, cache.WithRoot(cfg.CacheDir))
	}

	return cache.NewDiskStore(options...)
}

func withLockedCache(
	ctx context.Context,
	cfg Config,
	run func(catalog incusosprovider.Catalog, downloader incusosprovider.Downloader) error,
) (err error) {
	cacheStore, err := newCacheStore(cfg)
	if err != nil {
		return err
	}

	cacheLock, err := cacheStore.Lock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := cacheLock.Unlock(); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	client := cdn.NewClient(cdn.WithCacheService(cacheStore))
	if err := run(client, client); err != nil {
		return err
	}
	if err := cacheStore.Prune(ctx); err != nil {
		return err
	}

	return nil
}

func buildOutputDir(output *core.OutputDefaults) string {
	if output == nil || output.Dir == "" {
		return defaultBuildOutputDir
	}

	return output.Dir
}
