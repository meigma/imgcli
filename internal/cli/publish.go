package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	imgsrv "github.com/meigma/imgsrv/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/internal/publish"
	imgschemas "github.com/meigma/imgcli/schemas"
	"github.com/meigma/imgcli/schemas/core"
)

func newPublishCommand(rt *runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish CONFIG",
		Short: "Build and publish disk image artifacts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pubConfig, err := loadPublishConfig(rt.viper)
			if err != nil {
				return err
			}
			aliases, err := publishAliases(cmd)
			if err != nil {
				return err
			}

			config, err := loadImageConfig(args[0])
			if err != nil {
				return err
			}

			buildResult, err := rt.runIncusOSBuild(cmd.Context(), config)
			if err != nil {
				return err
			}

			uploadsClient, catalogClient, err := rt.imgsrvPublishClients(pubConfig)
			if err != nil {
				return err
			}

			uploader, err := publish.NewUploader(uploadsClient, publish.Options{
				PartSizeBytes: pubConfig.partSizeBytes,
				Wait:          pubConfig.wait,
				Timeout:       pubConfig.timeout,
				PollInterval:  pubConfig.pollInterval,
			})
			if err != nil {
				return err
			}

			publisher, err := publish.NewPublisher(catalogClient, uploader)
			if err != nil {
				return err
			}

			request, err := publishReleaseRequest(config, buildResult, pubConfig.version, aliases)
			if err != nil {
				return err
			}
			result, err := publisher.PublishRelease(cmd.Context(), request)
			if err != nil {
				return err
			}

			return printPublishResult(rt.opts.stdout(), result)
		},
	}

	flags := cmd.Flags()
	flags.String(flagImgsrvURL, "", "imgsrv API base URL")
	flags.String(flagImgsrvToken, "", "imgsrv bearer token")
	flags.String(flagReleaseVersion, "", "imgsrv image version to publish")
	flags.StringArray(flagAlias, nil, "imgsrv alias to point at the published version")
	flags.String(flagPublishPartSize, defaultPublishPartSize, "Multipart upload part size")
	flags.Bool(flagPublishWait, true, "Wait until imgsrv marks the upload ready")
	flags.String(flagPublishTimeout, defaultPublishTimeout, "Maximum time to wait for imgsrv readiness")
	flags.String(flagPublishPollInterval, defaultPublishPollInterval, "imgsrv readiness poll interval")

	mustBindPublishFlag(rt, flags, KeyImgsrvURL, flagImgsrvURL)
	mustBindPublishFlag(rt, flags, KeyImgsrvToken, flagImgsrvToken)
	mustBindPublishFlag(rt, flags, KeyPublishVersion, flagReleaseVersion)
	mustBindPublishFlag(rt, flags, KeyPublishPartSize, flagPublishPartSize)
	mustBindPublishFlag(rt, flags, KeyPublishWait, flagPublishWait)
	mustBindPublishFlag(rt, flags, KeyPublishTimeout, flagPublishTimeout)
	mustBindPublishFlag(rt, flags, KeyPublishPollInterval, flagPublishPollInterval)

	return cmd
}

func mustBindPublishFlag(rt *runtime, flags *pflag.FlagSet, key string, flagName string) {
	if err := bindConfigFlag(rt.viper, flags, key, flagName); err != nil {
		panic(err)
	}
}

func (rt *runtime) imgsrvPublishClients(
	cfg publishConfig,
) (publish.UploadsClient, publish.CatalogClient, error) {
	uploads := rt.opts.ImgsrvUploadsClient
	catalog := rt.opts.ImgsrvCatalogClient
	if uploads != nil && catalog != nil {
		return uploads, catalog, nil
	}

	client, err := imgsrv.New(imgsrv.Options{
		BaseURL:     cfg.imgsrvURL,
		BearerToken: cfg.imgsrvToken,
		UserAgent:   "imgcli/" + rt.opts.version(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("configure imgsrv client: %w", err)
	}

	if uploads == nil {
		uploads = client.Uploads()
	}
	if catalog == nil {
		catalog = client.Catalog()
	}

	return uploads, catalog, nil
}

func publishAliases(cmd *cobra.Command) ([]string, error) {
	values, err := cmd.Flags().GetStringArray(flagAlias)
	if err != nil {
		return nil, fmt.Errorf("read publish aliases: %w", err)
	}

	aliases := make([]string, 0, len(values))
	for _, value := range values {
		alias := strings.TrimSpace(value)
		if alias == "" {
			return nil, errors.New("publish alias must not be empty")
		}
		aliases = append(aliases, alias)
	}

	return aliases, nil
}

func publishReleaseRequest(
	config imgschemas.Config,
	buildResult providers.BuildResult,
	version string,
	aliases []string,
) (publish.ReleaseRequest, error) {
	request := publish.ReleaseRequest{
		ImageName:        publishImageName(config),
		ImageDescription: config.Image.Description,
		Version:          version,
		Aliases:          aliases,
		Artifacts:        make([]publish.ReleaseArtifact, 0, len(buildResult.Artifacts)),
	}

	for _, artifact := range buildResult.Artifacts {
		format, err := imgsrvArtifactFormat(artifact.Plan.Format)
		if err != nil {
			return publish.ReleaseRequest{}, err
		}

		request.Artifacts = append(request.Artifacts, publish.ReleaseArtifact{
			Key:             string(artifact.Plan.Key),
			Variant:         string(artifact.Plan.Variant),
			LocalPath:       artifact.Path,
			OperatingSystem: artifact.Plan.OperatingSystem,
			Architecture:    imgsrvArchitecture(artifact.Plan.Architecture),
			Format:          format,
			Digest:          artifact.SHA256,
			Size:            artifact.Size,
			MediaType:       artifact.Plan.MediaType,
		})
	}

	return request, nil
}

func publishImageName(config imgschemas.Config) string {
	if config.Publish != nil {
		imageName := strings.TrimSpace(string(config.Publish.ImageName))
		if imageName != "" {
			return imageName
		}
	}

	return string(config.Image.Name)
}

func imgsrvArchitecture(architecture core.Architecture) string {
	switch architecture {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	default:
		return string(architecture)
	}
}

func imgsrvArtifactFormat(format core.ArtifactFormat) (imgsrv.ArtifactFormat, error) {
	switch format {
	case "raw":
		return imgsrv.ArtifactFormatRaw, nil
	case "raw.gz":
		return imgsrv.ArtifactFormatRawGZ, nil
	default:
		return "", fmt.Errorf("unsupported imgsrv artifact format %q", format)
	}
}

func printPublishResult(output io.Writer, result publish.ReleaseResult) error {
	encoder := json.NewEncoder(output)
	if err := encoder.Encode(result); err != nil {
		return fmt.Errorf("write published release manifest: %w", err)
	}

	return nil
}
