package incusos

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/schemas/core"
	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

const (
	defaultOutputDir = "dist"
	defaultImageName = "image"
	providerName     = core.ProviderName("incusos")
)

var _ providers.Provider = (*Provider)(nil)

// Config is the generated IncusOS provider configuration.
type Config = incusosschema.Config

// Options wires provider-specific ports used by the IncusOS implementation.
type Options struct {
	// Catalog resolves IncusOS release metadata into source image assets.
	Catalog Catalog

	// Downloader retrieves and verifies IncusOS source image assets.
	Downloader Downloader

	// SeedBuilder creates IncusOS seed archives from provider configuration.
	SeedBuilder SeedBuilder

	// ImageInjector writes seed archives into local IncusOS images.
	ImageInjector ImageInjector
}

// Provider plans and builds IncusOS image artifacts.
type Provider struct {
	config  Config
	options Options
}

// New constructs an IncusOS provider from generated configuration and ports.
func New(config Config, options Options) *Provider {
	return &Provider{
		config:  config,
		options: options,
	}
}

// Name returns the IncusOS provider name.
func (p *Provider) Name() core.ProviderName {
	return providerName
}

// Plan resolves IncusOS configuration into concrete artifact work.
func (p *Provider) Plan(_ context.Context, _ providers.PlanRequest) (providers.Plan, error) {
	return providers.Plan{}, ErrNotImplemented
}

// Build creates IncusOS artifacts from an already resolved plan.
func (p *Provider) Build(ctx context.Context, req providers.BuildRequest) (providers.BuildResult, error) {
	if p.options.Catalog == nil {
		return providers.BuildResult{}, errors.New("incusos catalog is required")
	}
	if p.options.Downloader == nil {
		return providers.BuildResult{}, errors.New("incusos downloader is required")
	}
	if p.options.SeedBuilder == nil {
		return providers.BuildResult{}, errors.New("incusos seed builder is required")
	}
	if p.options.ImageInjector == nil {
		return providers.BuildResult{}, errors.New("incusos image injector is required")
	}

	variantName, variant, err := singleVariant(p.config)
	if err != nil {
		return providers.BuildResult{}, err
	}

	imageType, err := imageTypeForFormat(variant.Artifact.Format)
	if err != nil {
		return providers.BuildResult{}, err
	}

	outputPath, err := artifactOutputPath(req, variantName, variant.Artifact)
	if err != nil {
		return providers.BuildResult{}, err
	}

	source := resolveSource(p.config.Defaults, variant.Source)
	asset, err := p.options.Catalog.ResolveImage(ctx, ImageQuery{
		Channel:      source.Channel,
		Version:      source.Version,
		Architecture: variant.Artifact.Architecture,
		Type:         imageType,
	})
	if err != nil {
		return providers.BuildResult{}, err
	}

	downloaded, err := p.options.Downloader.DownloadImage(ctx, asset)
	if err != nil {
		return providers.BuildResult{}, err
	}

	seed, err := p.options.SeedBuilder.BuildSeed(ctx, p.config)
	if err != nil {
		return providers.BuildResult{}, err
	}

	customized, err := p.options.ImageInjector.InjectSeed(ctx, downloaded, seed, outputPath)
	if err != nil {
		return providers.BuildResult{}, err
	}

	artifactPlan := providers.ArtifactPlan{
		Key:          core.ArtifactKey(variantName),
		Variant:      variantName,
		Architecture: variant.Artifact.Architecture,
		Format:       variant.Artifact.Format,
		MediaType:    variant.Artifact.MediaType,
		OutputPath:   outputPath,
		Labels:       variant.Artifact.Labels,
		Annotations:  variant.Artifact.Annotations,
	}
	plan := req.Plan
	plan.Provider = providerName
	plan.OutputDir = outputDir(req)
	plan.Artifacts = []providers.ArtifactPlan{artifactPlan}

	return providers.BuildResult{
		Plan: plan,
		Artifacts: []providers.BuiltArtifact{
			{
				Plan:   artifactPlan,
				Path:   customized.Path,
				Size:   customized.Size,
				SHA256: customized.SHA256,
			},
		},
	}, nil
}

func singleVariant(config Config) (core.VariantName, incusosschema.Variant, error) {
	switch len(config.Variants) {
	case 0:
		return "", incusosschema.Variant{}, errors.New("incusos build requires exactly one variant, got 0")
	case 1:
		for name, variant := range config.Variants {
			return name, variant, nil
		}
	}

	return "", incusosschema.Variant{}, fmt.Errorf(
		"incusos build requires exactly one variant, got %d",
		len(config.Variants),
	)
}

func artifactOutputPath(
	req providers.BuildRequest,
	variantName core.VariantName,
	artifact core.ArtifactIntent,
) (string, error) {
	filename := strings.TrimSpace(artifact.Filename)
	if filename == "" {
		filename = fallbackArtifactFilename(req.Plan.Image.Name, variantName, artifact)
	}
	if filepath.IsAbs(filename) {
		return "", fmt.Errorf("incusos artifact filename must be relative: %q", filename)
	}

	cleanFilename := filepath.Clean(filename)
	if cleanFilename == "." || cleanFilename == ".." ||
		strings.HasPrefix(cleanFilename, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("incusos artifact filename must stay within output directory: %q", filename)
	}

	return filepath.Join(outputDir(req), cleanFilename), nil
}

func fallbackArtifactFilename(imageName core.Name, variantName core.VariantName, artifact core.ArtifactIntent) string {
	name := strings.TrimSpace(string(imageName))
	if name == "" {
		name = defaultImageName
	}

	return fmt.Sprintf("%s-%s-%s.%s", name, variantName, artifact.Architecture, artifact.Format)
}

func outputDir(req providers.BuildRequest) string {
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = strings.TrimSpace(req.Plan.OutputDir)
	}
	if outputDir == "" {
		return defaultOutputDir
	}

	return outputDir
}

func resolveSource(defaults *incusosschema.Defaults, variantSource *incusosschema.Source) incusosschema.Source {
	var source incusosschema.Source
	if defaults != nil && defaults.Source != nil {
		source = *defaults.Source
	}
	if variantSource != nil {
		if variantSource.Channel != "" {
			source.Channel = variantSource.Channel
		}
		if variantSource.Version != "" {
			source.Version = variantSource.Version
		}
	}
	if source.Channel == "" {
		source.Channel = ChannelStable
	}

	return source
}

func imageTypeForFormat(format core.ArtifactFormat) (ImageType, error) {
	switch format {
	case "raw", "raw.gz":
		return ImageTypeRaw, nil
	default:
		return "", fmt.Errorf("unsupported incusos artifact format %q", format)
	}
}
