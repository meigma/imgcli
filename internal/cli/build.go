package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/cache"
	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/providers/incusos/cdn"
	"github.com/meigma/imgcli/internal/providers/incusos/imagefile"
	imgschemas "github.com/meigma/imgcli/schemas"
	"github.com/meigma/imgcli/schemas/core"
)

const (
	defaultBuildOutputDir = "dist"

	artifactMetadataAPIVersion = "imgcli.meigma.io/v0alpha1"
	artifactMetadataKind       = "ArtifactMetadata"
	artifactMetadataFileMode   = 0o600
	artifactMetadataSuffix     = ".artifact.json"

	buildSHA256PrefixLength = 12
	flagBuildFormat         = "format"
	tablePaddingWidth       = 2
)

type buildOutputFormat string

const (
	buildOutputFormatTable buildOutputFormat = "table"
	buildOutputFormatJSON  buildOutputFormat = "json"
	buildOutputFormatPaths buildOutputFormat = "paths"
)

func newBuildCommand(rt *runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build CONFIG",
		Short: "Build disk image artifacts from configuration",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, err := parseBuildOutputFormat(cmd)
			if err != nil {
				return err
			}

			config, err := loadImageConfig(args[0])
			if err != nil {
				return err
			}

			result, err := rt.runIncusOSBuild(cmd.Context(), config)
			if err != nil {
				return err
			}

			artifacts, err := writeBuildArtifactMetadata(result)
			if err != nil {
				return err
			}

			return printBuildArtifacts(rt.opts.stdout(), result, artifacts, format)
		},
	}

	cmd.Flags().String(
		flagBuildFormat,
		string(buildOutputFormatTable),
		"Output format: table, json, or paths",
	)

	return cmd
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

func (rt *runtime) runIncusOSBuild(
	ctx context.Context,
	config imgschemas.Config,
) (providers.BuildResult, error) {
	if rt.usesDefaultIncusOSCache() {
		var result providers.BuildResult
		err := withLockedCache(ctx, rt.config, rt.opts.IncusOSCDNBaseURL, func(
			catalog incusosprovider.Catalog,
			downloader incusosprovider.Downloader,
		) error {
			ports, portsErr := rt.incusOSBuildPorts(catalog, downloader)
			if portsErr != nil {
				return portsErr
			}

			buildResult, buildErr := runIncusOSBuild(ctx, config, ports)
			if buildErr != nil {
				return buildErr
			}
			result = buildResult
			return nil
		})
		if err != nil {
			return providers.BuildResult{}, err
		}

		return result, nil
	}

	ports, err := rt.incusOSBuildPorts(nil, nil)
	if err != nil {
		return providers.BuildResult{}, err
	}
	return runIncusOSBuild(ctx, config, ports)
}

func runIncusOSBuild(
	ctx context.Context,
	config imgschemas.Config,
	ports incusOSBuildPorts,
) (providers.BuildResult, error) {
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
		return providers.BuildResult{}, err
	}

	return result, nil
}

type builtArtifactOutput struct {
	artifact     providers.BuiltArtifact
	metadataPath string
}

type buildOutputSummary struct {
	Image     core.Image                   `json:"image"`
	Artifacts []buildArtifactOutputSummary `json:"artifacts"`
}

type buildArtifactOutputSummary struct {
	ArtifactKey  core.ArtifactKey    `json:"artifactKey"`
	Variant      core.VariantName    `json:"variant"`
	Provider     core.ProviderName   `json:"provider"`
	Os           string              `json:"os"`
	Architecture core.Architecture   `json:"architecture"`
	Format       core.ArtifactFormat `json:"format"`
	Path         string              `json:"path"`
	MetadataPath string              `json:"metadataPath"`
	Size         int64               `json:"size"`
	SHA256       string              `json:"sha256"`
}

func parseBuildOutputFormat(cmd *cobra.Command) (buildOutputFormat, error) {
	value, err := cmd.Flags().GetString(flagBuildFormat)
	if err != nil {
		return "", fmt.Errorf("read build output format: %w", err)
	}

	switch buildOutputFormat(strings.ToLower(strings.TrimSpace(value))) {
	case buildOutputFormatTable:
		return buildOutputFormatTable, nil
	case buildOutputFormatJSON:
		return buildOutputFormatJSON, nil
	case buildOutputFormatPaths:
		return buildOutputFormatPaths, nil
	default:
		return "", fmt.Errorf("invalid build output format %q: expected table, json, or paths", value)
	}
}

func writeBuildArtifactMetadata(result providers.BuildResult) ([]builtArtifactOutput, error) {
	artifacts := make([]builtArtifactOutput, 0, len(result.Artifacts))
	for _, artifact := range result.Artifacts {
		metadataPath := buildArtifactMetadataPath(artifact.Path)
		metadata := buildArtifactMetadata(result.Plan, artifact)
		if err := writeJSONFile(metadataPath, metadata); err != nil {
			return nil, fmt.Errorf("write artifact metadata %q: %w", metadataPath, err)
		}

		artifacts = append(artifacts, builtArtifactOutput{
			artifact:     artifact,
			metadataPath: metadataPath,
		})
	}

	return artifacts, nil
}

func buildArtifactMetadata(plan providers.Plan, artifact providers.BuiltArtifact) imgschemas.ArtifactMetadata {
	resolved := resolvedArtifactForOutput(plan, artifact.Plan)
	resolved.Path = artifact.Path
	resolved.Digest = "sha256:" + artifact.SHA256
	resolved.Size = artifact.Size

	return imgschemas.ArtifactMetadata{
		ApiVersion: artifactMetadataAPIVersion,
		Kind:       artifactMetadataKind,
		Artifact:   resolved,
	}
}

func writeJSONFile(path string, value any) error {
	temp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tempPath := temp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		_ = temp.Close()
		return fmt.Errorf("encode JSON: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Chmod(tempPath, artifactMetadataFileMode); err != nil {
		return fmt.Errorf("set temp file permissions: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("publish temp file: %w", err)
	}
	committed = true

	return nil
}

func printBuildArtifacts(
	output io.Writer,
	result providers.BuildResult,
	artifacts []builtArtifactOutput,
	format buildOutputFormat,
) error {
	switch format {
	case buildOutputFormatTable:
		return printBuildArtifactsTable(output, artifacts)
	case buildOutputFormatJSON:
		return printBuildArtifactsJSON(output, result, artifacts)
	case buildOutputFormatPaths:
		return printBuildArtifactPaths(output, artifacts)
	default:
		return fmt.Errorf("unsupported build output format %q", format)
	}
}

func printBuildArtifactsTable(output io.Writer, artifacts []builtArtifactOutput) error {
	table := tabwriter.NewWriter(output, 0, 0, tablePaddingWidth, ' ', 0)
	if _, err := fmt.Fprintln(
		table,
		"VARIANT\tOS\tARCH\tFORMAT\tSIZE_BYTES\tSHA256_PREFIX\tARTIFACT\tMETADATA",
	); err != nil {
		return fmt.Errorf("write build artifact table header: %w", err)
	}

	for _, artifact := range artifacts {
		plan := artifact.artifact.Plan
		if _, err := fmt.Fprintf(
			table,
			"%s\t%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
			plan.Variant,
			plan.OperatingSystem,
			plan.Architecture,
			plan.Format,
			artifact.artifact.Size,
			shortSHA256(artifact.artifact.SHA256),
			artifact.artifact.Path,
			artifact.metadataPath,
		); err != nil {
			return fmt.Errorf("write build artifact table row: %w", err)
		}
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("flush build artifact table: %w", err)
	}
	return nil
}

func printBuildArtifactsJSON(
	output io.Writer,
	result providers.BuildResult,
	artifacts []builtArtifactOutput,
) error {
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(buildOutputForJSON(result, artifacts)); err != nil {
		return fmt.Errorf("write build artifact summary: %w", err)
	}
	return nil
}

func printBuildArtifactPaths(output io.Writer, artifacts []builtArtifactOutput) error {
	for _, artifact := range artifacts {
		if _, err := fmt.Fprintln(output, artifact.artifact.Path); err != nil {
			return fmt.Errorf("write build artifact path: %w", err)
		}
	}

	return nil
}

func buildOutputForJSON(
	result providers.BuildResult,
	artifacts []builtArtifactOutput,
) buildOutputSummary {
	summary := buildOutputSummary{
		Image:     result.Plan.Image,
		Artifacts: make([]buildArtifactOutputSummary, 0, len(artifacts)),
	}

	for _, artifact := range artifacts {
		plan := artifact.artifact.Plan
		summary.Artifacts = append(summary.Artifacts, buildArtifactOutputSummary{
			ArtifactKey:  plan.Key,
			Variant:      plan.Variant,
			Provider:     result.Plan.Provider,
			Os:           plan.OperatingSystem,
			Architecture: plan.Architecture,
			Format:       plan.Format,
			Path:         artifact.artifact.Path,
			MetadataPath: artifact.metadataPath,
			Size:         artifact.artifact.Size,
			SHA256:       artifact.artifact.SHA256,
		})
	}

	return summary
}

func buildArtifactMetadataPath(path string) string {
	return path + artifactMetadataSuffix
}

func shortSHA256(sha256Digest string) string {
	if len(sha256Digest) <= buildSHA256PrefixLength {
		return sha256Digest
	}
	return sha256Digest[:buildSHA256PrefixLength]
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
	incusOSCDNBaseURL string,
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

	cdnOptions := []cdn.Option{
		cdn.WithCacheService(cacheStore),
	}
	if strings.TrimSpace(incusOSCDNBaseURL) != "" {
		cdnOptions = append(cdnOptions, cdn.WithBaseURL(incusOSCDNBaseURL))
	}
	client := cdn.NewClient(cdnOptions...)
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
