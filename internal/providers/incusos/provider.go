package incusos

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
func (p *Provider) Plan(_ context.Context, req providers.PlanRequest) (providers.Plan, error) {
	artifacts, err := planArtifacts(req, p.config)
	if err != nil {
		return providers.Plan{}, err
	}

	return providerPlan(req, artifacts), nil
}

// Build creates IncusOS artifacts from a resolved provider plan.
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

	plan, err := p.Plan(ctx, buildPlanRequest(req))
	if err != nil {
		return providers.BuildResult{}, err
	}

	artifacts, err := plannedArtifactsForExecution(plan, p.config)
	if err != nil {
		return providers.BuildResult{}, err
	}
	if outputErr := rejectExistingOutputPaths(artifacts); outputErr != nil {
		return providers.BuildResult{}, outputErr
	}

	seed, err := p.options.SeedBuilder.BuildSeed(ctx, p.config)
	if err != nil {
		return providers.BuildResult{}, err
	}

	builtArtifacts := make([]providers.BuiltArtifact, 0, len(artifacts))
	cleanupOutputs := make([]string, 0, len(artifacts))
	for _, artifact := range artifacts {
		source := resolveSource(p.config.Defaults, artifact.variant.Source)
		asset, err := p.options.Catalog.ResolveImage(ctx, ImageQuery{
			Channel:      source.Channel,
			Version:      source.Version,
			Architecture: artifact.plan.Architecture,
			Type:         artifact.imageType,
		})
		if err != nil {
			return providers.BuildResult{}, cleanupBuiltOutputs(err, cleanupOutputs)
		}

		downloaded, err := p.options.Downloader.DownloadImage(ctx, asset)
		if err != nil {
			return providers.BuildResult{}, cleanupBuiltOutputs(err, cleanupOutputs)
		}

		customized, err := p.options.ImageInjector.InjectSeed(ctx, downloaded, seed, artifact.plan.OutputPath)
		if err != nil {
			return providers.BuildResult{}, cleanupBuiltOutputs(err, cleanupOutputs)
		}
		cleanupOutputs = append(cleanupOutputs, artifact.plan.OutputPath)

		builtArtifacts = append(builtArtifacts, providers.BuiltArtifact{
			Plan:   artifact.plan,
			Path:   customized.Path,
			Size:   customized.Size,
			SHA256: customized.SHA256,
		})
	}

	return providers.BuildResult{
		Plan:      plan,
		Artifacts: builtArtifacts,
	}, nil
}

type plannedArtifact struct {
	variant   incusosschema.Variant
	imageType ImageType
	plan      providers.ArtifactPlan
}

func planArtifacts(req providers.PlanRequest, config Config) ([]plannedArtifact, error) {
	if len(config.Variants) == 0 {
		return nil, errors.New("incusos build requires at least one variant")
	}

	names := make([]string, 0, len(config.Variants))
	for name := range config.Variants {
		names = append(names, string(name))
	}
	sort.Strings(names)

	artifacts := make([]plannedArtifact, 0, len(names))
	outputPaths := map[string]core.VariantName{}
	for _, name := range names {
		variantName := core.VariantName(name)
		variant := config.Variants[variantName]
		imageType, err := imageTypeForFormat(variant.Artifact.Format)
		if err != nil {
			return nil, err
		}
		outputPath, err := artifactOutputPath(req, variantName, variant.Artifact)
		if err != nil {
			return nil, err
		}
		if previous, exists := outputPaths[outputPath]; exists {
			return nil, fmt.Errorf(
				"incusos artifact output path %q is used by variants %q and %q",
				outputPath,
				previous,
				variantName,
			)
		}
		outputPaths[outputPath] = variantName

		artifactPlan := providers.ArtifactPlan{
			Key:             core.ArtifactKey(variantName),
			Variant:         variantName,
			Architecture:    variant.Artifact.Architecture,
			OperatingSystem: artifactOperatingSystem(variant.Artifact),
			Format:          variant.Artifact.Format,
			MediaType:       artifactMediaType(variant.Artifact),
			OutputPath:      outputPath,
			Labels:          variant.Artifact.Labels,
			Annotations:     variant.Artifact.Annotations,
		}
		artifacts = append(artifacts, plannedArtifact{
			variant:   variant,
			imageType: imageType,
			plan:      artifactPlan,
		})
	}

	return artifacts, nil
}

func providerPlan(req providers.PlanRequest, artifacts []plannedArtifact) providers.Plan {
	plan := providers.Plan{
		Provider:  providerName,
		Image:     req.Image,
		Version:   req.Version,
		OutputDir: outputDir(req),
		Artifacts: make([]providers.ArtifactPlan, 0, len(artifacts)),
	}
	for _, artifact := range artifacts {
		plan.Artifacts = append(plan.Artifacts, artifact.plan)
	}

	return plan
}

func buildPlanRequest(req providers.BuildRequest) providers.PlanRequest {
	return providers.PlanRequest{
		Image:     req.Plan.Image,
		Version:   req.Plan.Version,
		OutputDir: buildOutputDir(req),
	}
}

func plannedArtifactsForExecution(plan providers.Plan, config Config) ([]plannedArtifact, error) {
	artifacts := make([]plannedArtifact, 0, len(plan.Artifacts))
	for _, artifactPlan := range plan.Artifacts {
		variant, ok := config.Variants[artifactPlan.Variant]
		if !ok {
			return nil, fmt.Errorf("incusos planned artifact references unknown variant %q", artifactPlan.Variant)
		}

		imageType, err := imageTypeForFormat(artifactPlan.Format)
		if err != nil {
			return nil, err
		}

		artifacts = append(artifacts, plannedArtifact{
			variant:   variant,
			imageType: imageType,
			plan:      artifactPlan,
		})
	}

	return artifacts, nil
}

func rejectExistingOutputPaths(artifacts []plannedArtifact) error {
	for _, artifact := range artifacts {
		path := artifact.plan.OutputPath
		_, err := os.Stat(path)
		switch {
		case err == nil:
			return fmt.Errorf("incusos artifact output path already exists: %q", path)
		case errors.Is(err, os.ErrNotExist):
			continue
		default:
			return fmt.Errorf("stat incusos artifact output path %q: %w", path, err)
		}
	}

	return nil
}

func cleanupBuiltOutputs(cause error, paths []string) error {
	if len(paths) == 0 {
		return cause
	}

	errs := []error{cause}
	for index := len(paths) - 1; index >= 0; index-- {
		path := paths[index]
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove partial incusos artifact %q: %w", path, err))
		}
	}

	if len(errs) == 1 {
		return cause
	}
	return errors.Join(errs...)
}

func artifactOutputPath(
	req providers.PlanRequest,
	variantName core.VariantName,
	artifact core.ArtifactIntent,
) (string, error) {
	filename := strings.TrimSpace(artifact.Filename)
	if filename == "" {
		filename = fallbackArtifactFilename(req.Image.Name, variantName, artifact)
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

func buildOutputDir(req providers.BuildRequest) string {
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = strings.TrimSpace(req.Plan.OutputDir)
	}
	if outputDir == "" {
		return defaultOutputDir
	}

	return outputDir
}

func outputDir(req providers.PlanRequest) string {
	outputDir := strings.TrimSpace(req.OutputDir)
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

func artifactMediaType(artifact core.ArtifactIntent) string {
	if strings.TrimSpace(artifact.MediaType) != "" {
		return artifact.MediaType
	}

	switch artifact.Format {
	case "raw.gz":
		return "application/gzip"
	default:
		return "application/octet-stream"
	}
}

func artifactOperatingSystem(artifact core.ArtifactIntent) string {
	if strings.TrimSpace(artifact.Os) != "" {
		return artifact.Os
	}

	return string(providerName)
}
