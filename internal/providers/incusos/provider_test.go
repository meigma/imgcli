package incusos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/schemas/core"
	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

func TestProviderName(t *testing.T) {
	provider := New(Config{}, Options{})

	assert.Equal(t, core.ProviderName("incusos"), provider.Name())
}

func TestProviderPlanPlaceholderOperation(t *testing.T) {
	provider := New(Config{}, Options{})

	plan, err := provider.Plan(context.Background(), providers.PlanRequest{})
	require.ErrorIs(t, err, ErrNotImplemented)
	assert.Empty(t, plan)
}

func TestProviderBuildCreatesCustomizedImage(t *testing.T) {
	tests := []struct {
		name           string
		artifact       core.ArtifactIntent
		wantOutputPath func(outputDir string) string
	}{
		{
			name: "uses configured artifact filename",
			artifact: core.ArtifactIntent{
				Architecture: core.Architecture("amd64"),
				Format:       core.ArtifactFormat("raw.gz"),
				Filename:     "custom/incusos-smoke.img.gz",
				MediaType:    "application/gzip",
				Labels:       map[string]string{"tier": "smoke"},
				Annotations:  map[string]string{"note": "e2e"},
			},
			wantOutputPath: func(outputDir string) string {
				return filepath.Join(outputDir, "custom", "incusos-smoke.img.gz")
			},
		},
		{
			name: "derives artifact filename when omitted",
			artifact: core.ArtifactIntent{
				Architecture: core.Architecture("amd64"),
				Format:       core.ArtifactFormat("raw.gz"),
			},
			wantOutputPath: func(outputDir string) string {
				return filepath.Join(outputDir, "test-image-default-amd64.raw.gz")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			outputDir := t.TempDir()
			asset := ImageAsset{
				Version:      Version("202604261712"),
				Architecture: core.Architecture("amd64"),
				Type:         ImageTypeRaw,
				URL:          "https://example.invalid/incusos.img.gz",
				SHA256:       "source-sha",
				Size:         42,
			}
			downloaded := DownloadedImage{
				Asset:  asset,
				Path:   "/cache/source.img.gz",
				SHA256: "source-sha",
				Size:   42,
			}
			seed := SeedArchive{Data: []byte("seed")}
			customized := CustomizedImage{
				Source: downloaded,
				Size:   99,
				SHA256: "custom-sha",
			}
			catalog := &recordingCatalog{asset: asset}
			downloader := &recordingDownloader{image: downloaded}
			seedBuilder := &recordingSeedBuilder{seed: seed}
			injector := &recordingImageInjector{image: customized}
			config := Config{
				Defaults: &incusosschema.Defaults{
					Source: &incusosschema.Source{
						Channel: ChannelStable,
						Version: Version("202604202240"),
					},
				},
				Seed: &incusosschema.Seed{},
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {
						Source: &incusosschema.Source{
							Channel: ChannelTesting,
							Version: Version("202604261712"),
						},
						Artifact: tt.artifact,
					},
				},
			}
			provider := New(config, Options{
				Catalog:       catalog,
				Downloader:    downloader,
				SeedBuilder:   seedBuilder,
				ImageInjector: injector,
			})

			result, err := provider.Build(ctx, providers.BuildRequest{
				Plan: providers.Plan{
					Image: core.Image{Name: core.Name("test-image")},
				},
				OutputDir: outputDir,
			})

			require.NoError(t, err)
			wantOutputPath := tt.wantOutputPath(outputDir)
			assert.Equal(t, []ImageQuery{
				{
					Channel:      ChannelTesting,
					Version:      Version("202604261712"),
					Architecture: core.Architecture("amd64"),
					Type:         ImageTypeRaw,
				},
			}, catalog.queries)
			assert.Equal(t, []ImageAsset{asset}, downloader.assets)
			assert.Equal(t, []Config{config}, seedBuilder.configs)
			require.Len(t, injector.calls, 1)
			assert.Equal(t, downloaded, injector.calls[0].image)
			assert.Equal(t, seed, injector.calls[0].seed)
			assert.Equal(t, wantOutputPath, injector.calls[0].outputPath)

			require.Len(t, result.Artifacts, 1)
			assert.Equal(t, wantOutputPath, result.Artifacts[0].Path)
			assert.Equal(t, int64(99), result.Artifacts[0].Size)
			assert.Equal(t, "custom-sha", result.Artifacts[0].SHA256)
			assert.Equal(t, providerName, result.Plan.Provider)
			assert.Equal(t, core.Image{Name: core.Name("test-image")}, result.Plan.Image)
			assert.Equal(t, outputDir, result.Plan.OutputDir)
			require.Len(t, result.Plan.Artifacts, 1)
			assert.Equal(t, result.Plan.Artifacts[0], result.Artifacts[0].Plan)
			assert.Equal(t, providers.ArtifactPlan{
				Key:             core.ArtifactKey("default"),
				Variant:         core.VariantName("default"),
				Architecture:    tt.artifact.Architecture,
				OperatingSystem: "incusos",
				Format:          tt.artifact.Format,
				MediaType:       artifactMediaType(tt.artifact),
				OutputPath:      wantOutputPath,
				Labels:          tt.artifact.Labels,
				Annotations:     tt.artifact.Annotations,
			}, result.Plan.Artifacts[0])
		})
	}
}

func TestProviderBuildCreatesMultipleVariantsInStableOrder(t *testing.T) {
	ctx := context.Background()
	outputDir := t.TempDir()
	asset := ImageAsset{
		Version:      Version("202604261712"),
		Architecture: core.Architecture("amd64"),
		Type:         ImageTypeRaw,
		URL:          "https://example.invalid/incusos.img.gz",
		SHA256:       "source-sha",
		Size:         42,
	}
	downloaded := DownloadedImage{
		Asset:  asset,
		Path:   "/cache/source.img.gz",
		SHA256: "source-sha",
		Size:   42,
	}
	seed := SeedArchive{Data: []byte("seed")}
	customized := CustomizedImage{
		Source: downloaded,
		Size:   99,
		SHA256: "custom-sha",
	}
	catalog := &recordingCatalog{asset: asset}
	downloader := &recordingDownloader{image: downloaded}
	seedBuilder := &recordingSeedBuilder{seed: seed}
	injector := &recordingImageInjector{image: customized}
	config := Config{
		Defaults: &incusosschema.Defaults{
			Source: &incusosschema.Source{
				Channel: ChannelStable,
				Version: Version("202604202240"),
			},
		},
		Seed: &incusosschema.Seed{},
		Variants: map[core.VariantName]incusosschema.Variant{
			"secureboot": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
				},
			},
			"default": {
				Source: &incusosschema.Source{
					Channel: ChannelTesting,
					Version: Version("202604261712"),
				},
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
				},
			},
		},
	}
	provider := New(config, Options{
		Catalog:       catalog,
		Downloader:    downloader,
		SeedBuilder:   seedBuilder,
		ImageInjector: injector,
	})

	result, err := provider.Build(ctx, providers.BuildRequest{
		Plan: providers.Plan{
			Image: core.Image{Name: core.Name("test-image")},
		},
		OutputDir: outputDir,
	})

	require.NoError(t, err)
	assert.Equal(t, []ImageQuery{
		{
			Channel:      ChannelTesting,
			Version:      Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         ImageTypeRaw,
		},
		{
			Channel:      ChannelStable,
			Version:      Version("202604202240"),
			Architecture: core.Architecture("amd64"),
			Type:         ImageTypeRaw,
		},
	}, catalog.queries)
	assert.Equal(t, []Config{config}, seedBuilder.configs)
	require.Len(t, result.Artifacts, 2)
	assert.Equal(t, []providers.ArtifactPlan{
		{
			Key:             core.ArtifactKey("default"),
			Variant:         core.VariantName("default"),
			Architecture:    core.Architecture("amd64"),
			OperatingSystem: "incusos",
			Format:          core.ArtifactFormat("raw.gz"),
			MediaType:       "application/gzip",
			OutputPath:      filepath.Join(outputDir, "test-image-default-amd64.raw.gz"),
		},
		{
			Key:             core.ArtifactKey("secureboot"),
			Variant:         core.VariantName("secureboot"),
			Architecture:    core.Architecture("amd64"),
			OperatingSystem: "incusos",
			Format:          core.ArtifactFormat("raw.gz"),
			MediaType:       "application/gzip",
			OutputPath:      filepath.Join(outputDir, "test-image-secureboot-amd64.raw.gz"),
		},
	}, result.Plan.Artifacts)
	assert.Equal(t, result.Plan.Artifacts[0], result.Artifacts[0].Plan)
	assert.Equal(t, result.Plan.Artifacts[1], result.Artifacts[1].Plan)
	assert.Equal(t, []injectCall{
		{
			image:      downloaded,
			seed:       seed,
			outputPath: filepath.Join(outputDir, "test-image-default-amd64.raw.gz"),
		},
		{
			image:      downloaded,
			seed:       seed,
			outputPath: filepath.Join(outputDir, "test-image-secureboot-amd64.raw.gz"),
		},
	}, injector.calls)
}

func TestProviderBuildRejectsDuplicateOutputPathsBeforeBuild(t *testing.T) {
	catalog := &recordingCatalog{asset: ImageAsset{}}
	downloader := &recordingDownloader{}
	seedBuilder := &recordingSeedBuilder{seed: SeedArchive{Data: []byte("seed")}}
	injector := &recordingImageInjector{}
	config := Config{
		Seed: &incusosschema.Seed{},
		Variants: map[core.VariantName]incusosschema.Variant{
			"default": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					Filename:     "same.img.gz",
				},
			},
			"secureboot": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					Filename:     "same.img.gz",
				},
			},
		},
	}
	provider := New(config, Options{
		Catalog:       catalog,
		Downloader:    downloader,
		SeedBuilder:   seedBuilder,
		ImageInjector: injector,
	})

	result, err := provider.Build(context.Background(), providers.BuildRequest{
		Plan:      providers.Plan{Image: core.Image{Name: core.Name("test-image")}},
		OutputDir: t.TempDir(),
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "incusos artifact output path")
	assert.Empty(t, result)
	assert.Empty(t, catalog.queries)
	assert.Empty(t, downloader.assets)
	assert.Empty(t, seedBuilder.configs)
	assert.Empty(t, injector.calls)
}

func TestProviderBuildCleansUpNewOutputsAfterLaterVariantFailure(t *testing.T) {
	outputDir := t.TempDir()
	injectErr := errors.New("inject failed")
	injector := &writingImageInjector{failOnCall: 2, err: injectErr}
	provider := New(multiVariantConfig(), Options{
		Catalog:       &recordingCatalog{asset: ImageAsset{}},
		Downloader:    &recordingDownloader{},
		SeedBuilder:   &recordingSeedBuilder{seed: SeedArchive{Data: []byte("seed")}},
		ImageInjector: injector,
	})

	result, err := provider.Build(context.Background(), providers.BuildRequest{
		Plan:      providers.Plan{Image: core.Image{Name: core.Name("test-image")}},
		OutputDir: outputDir,
	})

	require.ErrorIs(t, err, injectErr)
	assert.Empty(t, result)
	assert.NoFileExists(t, filepath.Join(outputDir, "test-image-default-amd64.raw.gz"))
	assert.NoFileExists(t, filepath.Join(outputDir, "test-image-secureboot-amd64.raw.gz"))
	require.Len(t, injector.calls, 2)
}

func TestProviderBuildRejectsPreExistingOutputBeforeBuild(t *testing.T) {
	outputDir := t.TempDir()
	defaultOutputPath := filepath.Join(outputDir, "test-image-default-amd64.raw.gz")
	require.NoError(t, os.WriteFile(defaultOutputPath, []byte("pre-existing"), 0o600))
	catalog := &recordingCatalog{asset: ImageAsset{}}
	downloader := &recordingDownloader{}
	seedBuilder := &recordingSeedBuilder{seed: SeedArchive{Data: []byte("seed")}}
	injector := &writingImageInjector{}
	provider := New(multiVariantConfig(), Options{
		Catalog:       catalog,
		Downloader:    downloader,
		SeedBuilder:   seedBuilder,
		ImageInjector: injector,
	})

	result, err := provider.Build(context.Background(), providers.BuildRequest{
		Plan:      providers.Plan{Image: core.Image{Name: core.Name("test-image")}},
		OutputDir: outputDir,
	})

	require.ErrorContains(t, err, "incusos artifact output path already exists")
	assert.Empty(t, result)
	assert.FileExists(t, defaultOutputPath)
	assert.Empty(t, catalog.queries)
	assert.Empty(t, downloader.assets)
	assert.Empty(t, seedBuilder.configs)
	assert.Empty(t, injector.calls)
	assert.NoFileExists(t, filepath.Join(outputDir, "test-image-secureboot-amd64.raw.gz"))
}

func TestCleanupBuiltOutputsJoinsCleanupErrors(t *testing.T) {
	cause := errors.New("build failed")
	outputPath := filepath.Join(t.TempDir(), "artifact.raw.gz")
	require.NoError(t, os.Mkdir(outputPath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(outputPath, "child"), []byte("data"), 0o600))

	err := cleanupBuiltOutputs(cause, []string{outputPath})

	require.ErrorIs(t, err, cause)
	require.ErrorContains(t, err, "remove partial incusos artifact")
	assert.DirExists(t, outputPath)
}

func TestProviderBuildErrors(t *testing.T) {
	catalogErr := errors.New("catalog failed")
	downloadErr := errors.New("download failed")
	seedErr := errors.New("seed failed")
	injectErr := errors.New("inject failed")

	tests := []struct {
		name      string
		config    Config
		options   Options
		wantErr   string
		wantErrIs error
	}{
		{
			name:    "missing catalog",
			config:  configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(func(options *Options) { options.Catalog = nil }),
			wantErr: "incusos catalog is required",
		},
		{
			name:    "missing downloader",
			config:  configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(func(options *Options) { options.Downloader = nil }),
			wantErr: "incusos downloader is required",
		},
		{
			name:    "missing seed builder",
			config:  configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(func(options *Options) { options.SeedBuilder = nil }),
			wantErr: "incusos seed builder is required",
		},
		{
			name:    "missing image injector",
			config:  configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(func(options *Options) { options.ImageInjector = nil }),
			wantErr: "incusos image injector is required",
		},
		{
			name: "zero variants",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{},
			},
			options: optionsWithout(func(_ *Options) {}),
			wantErr: "incusos build requires at least one variant",
		},
		{
			name:    "unsupported format",
			config:  configWithVariant(core.ArtifactFormat("iso")),
			options: optionsWithout(func(_ *Options) {}),
			wantErr: `unsupported incusos artifact format "iso"`,
		},
		{
			name:      "catalog error",
			config:    configWithVariant(core.ArtifactFormat("raw")),
			options:   optionsWithout(func(options *Options) { options.Catalog = &recordingCatalog{err: catalogErr} }),
			wantErrIs: catalogErr,
		},
		{
			name:   "download error",
			config: configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(
				func(options *Options) { options.Downloader = &recordingDownloader{err: downloadErr} },
			),
			wantErrIs: downloadErr,
		},
		{
			name:   "seed error",
			config: configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(
				func(options *Options) { options.SeedBuilder = &recordingSeedBuilder{err: seedErr} },
			),
			wantErrIs: seedErr,
		},
		{
			name:   "inject error",
			config: configWithVariant(core.ArtifactFormat("raw")),
			options: optionsWithout(
				func(options *Options) { options.ImageInjector = &recordingImageInjector{err: injectErr} },
			),
			wantErrIs: injectErr,
		},
		{
			name: "absolute artifact filename",
			config: configWithArtifact(core.ArtifactIntent{
				Architecture: core.Architecture("amd64"),
				Format:       core.ArtifactFormat("raw"),
				Filename:     "/tmp/out.img",
			}),
			options: optionsWithout(func(_ *Options) {}),
			wantErr: `incusos artifact filename must be relative`,
		},
		{
			name: "escaping artifact filename",
			config: configWithArtifact(core.ArtifactIntent{
				Architecture: core.Architecture("amd64"),
				Format:       core.ArtifactFormat("raw"),
				Filename:     "../out.img",
			}),
			options: optionsWithout(func(_ *Options) {}),
			wantErr: `incusos artifact filename must stay within output directory`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := New(tt.config, tt.options)

			result, err := provider.Build(context.Background(), providers.BuildRequest{
				Plan:      providers.Plan{Image: core.Image{Name: core.Name("test-image")}},
				OutputDir: t.TempDir(),
			})

			require.Error(t, err)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			}
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
			}
			assert.Empty(t, result)
		})
	}
}

func multiVariantConfig() Config {
	return Config{
		Seed: &incusosschema.Seed{},
		Variants: map[core.VariantName]incusosschema.Variant{
			"default": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
				},
			},
			"secureboot": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
				},
			},
		},
	}
}

func configWithVariant(format core.ArtifactFormat) Config {
	return configWithArtifact(core.ArtifactIntent{
		Architecture: core.Architecture("amd64"),
		Format:       format,
	})
}

func configWithArtifact(artifact core.ArtifactIntent) Config {
	return Config{
		Seed: &incusosschema.Seed{},
		Variants: map[core.VariantName]incusosschema.Variant{
			"default": {
				Artifact: artifact,
			},
		},
	}
}

func optionsWithout(mutator func(*Options)) Options {
	options := Options{
		Catalog:       &recordingCatalog{asset: ImageAsset{}},
		Downloader:    &recordingDownloader{},
		SeedBuilder:   &recordingSeedBuilder{seed: SeedArchive{Data: []byte("seed")}},
		ImageInjector: &recordingImageInjector{},
	}
	mutator(&options)
	return options
}

type recordingCatalog struct {
	asset   ImageAsset
	err     error
	queries []ImageQuery
}

func (c *recordingCatalog) ResolveImage(_ context.Context, query ImageQuery) (ImageAsset, error) {
	c.queries = append(c.queries, query)
	if c.err != nil {
		return ImageAsset{}, c.err
	}

	return c.asset, nil
}

type recordingDownloader struct {
	image  DownloadedImage
	err    error
	assets []ImageAsset
}

func (d *recordingDownloader) DownloadImage(_ context.Context, asset ImageAsset) (DownloadedImage, error) {
	d.assets = append(d.assets, asset)
	if d.err != nil {
		return DownloadedImage{}, d.err
	}

	return d.image, nil
}

type recordingSeedBuilder struct {
	seed    SeedArchive
	err     error
	configs []Config
}

func (b *recordingSeedBuilder) BuildSeed(_ context.Context, config Config) (SeedArchive, error) {
	b.configs = append(b.configs, config)
	if b.err != nil {
		return SeedArchive{}, b.err
	}

	return b.seed, nil
}

type recordingImageInjector struct {
	image CustomizedImage
	err   error
	calls []injectCall
}

type injectCall struct {
	image      DownloadedImage
	seed       SeedArchive
	outputPath string
}

func (i *recordingImageInjector) InjectSeed(
	_ context.Context,
	image DownloadedImage,
	seed SeedArchive,
	outputPath string,
) (CustomizedImage, error) {
	i.calls = append(i.calls, injectCall{image: image, seed: seed, outputPath: outputPath})
	if i.err != nil {
		return CustomizedImage{}, i.err
	}

	customized := i.image
	if customized.Source == (DownloadedImage{}) {
		customized.Source = image
	}
	if customized.Path == "" {
		customized.Path = outputPath
	}
	return customized, nil
}

type writingImageInjector struct {
	failOnCall int
	err        error
	calls      []injectCall
}

func (i *writingImageInjector) InjectSeed(
	_ context.Context,
	image DownloadedImage,
	seed SeedArchive,
	outputPath string,
) (CustomizedImage, error) {
	i.calls = append(i.calls, injectCall{image: image, seed: seed, outputPath: outputPath})
	if i.failOnCall == len(i.calls) {
		return CustomizedImage{}, i.err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return CustomizedImage{}, err
	}
	if err := os.WriteFile(outputPath, []byte(outputPath), 0o600); err != nil {
		return CustomizedImage{}, err
	}

	return CustomizedImage{
		Source: image,
		Path:   outputPath,
		Size:   int64(len(outputPath)),
		SHA256: "custom-sha",
	}, nil
}
