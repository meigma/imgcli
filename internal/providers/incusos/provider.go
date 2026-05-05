package incusos

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/schemas/core"
	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

const providerName core.ProviderName = "incusos"

var _ providers.Provider = (*Provider)(nil)

// Config is the generated IncusOS provider configuration.
type Config = incusosschema.Config

// Options wires provider-specific ports used by the IncusOS implementation.
type Options struct {
	// Catalog resolves IncusOS release metadata into source image assets.
	Catalog Catalog

	// Output receives temporary shallow build output. Nil discards output.
	Output io.Writer

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
func (p *Provider) Build(ctx context.Context, _ providers.BuildRequest) (providers.BuildResult, error) {
	if p.options.Catalog == nil {
		return providers.BuildResult{}, errors.New("incusos catalog is required")
	}

	variant, err := singleVariant(p.config)
	if err != nil {
		return providers.BuildResult{}, err
	}

	imageType, err := imageTypeForFormat(variant.Artifact.Format)
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

	if _, err := fmt.Fprintln(p.output(), asset.URL); err != nil {
		return providers.BuildResult{}, fmt.Errorf("write incusos image URL: %w", err)
	}

	return providers.BuildResult{}, nil
}

func (p *Provider) output() io.Writer {
	if p.options.Output == nil {
		return io.Discard
	}

	return p.options.Output
}

func singleVariant(config Config) (incusosschema.Variant, error) {
	switch len(config.Variants) {
	case 0:
		return incusosschema.Variant{}, errors.New("incusos build requires exactly one variant, got 0")
	case 1:
		for _, variant := range config.Variants {
			return variant, nil
		}
	}

	return incusosschema.Variant{}, fmt.Errorf(
		"incusos build requires exactly one variant, got %d",
		len(config.Variants),
	)
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
	case "iso":
		return ImageTypeISO, nil
	default:
		return "", fmt.Errorf("unsupported incusos artifact format %q", format)
	}
}
