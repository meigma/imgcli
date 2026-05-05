package cli

import (
	"fmt"

	imgsrv "github.com/meigma/imgsrv/client"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/internal/publish"
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

			config, err := loadImageConfig(args[0])
			if err != nil {
				return err
			}

			result, err := rt.runIncusOSBuild(cmd.Context(), config)
			if err != nil {
				return err
			}

			uploadsClient, err := rt.imgsrvUploadsClient(pubConfig)
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

			digests := make([]string, 0, len(result.Artifacts))
			for _, artifact := range result.Artifacts {
				uploadResult, err := uploader.UploadArtifact(cmd.Context(), publishArtifact(artifact))
				if err != nil {
					return err
				}
				digests = append(digests, uploadResult.Digest.String())
			}

			for _, digest := range digests {
				if _, err := fmt.Fprintln(rt.opts.stdout(), digest); err != nil {
					return fmt.Errorf("write published artifact digest: %w", err)
				}
			}

			return nil
		},
	}

	flags := cmd.Flags()
	flags.String(flagImgsrvURL, "", "imgsrv API base URL")
	flags.String(flagImgsrvToken, "", "imgsrv bearer token")
	flags.String(flagPublishPartSize, defaultPublishPartSize, "Multipart upload part size")
	flags.Bool(flagPublishWait, true, "Wait until imgsrv marks the upload ready")
	flags.String(flagPublishTimeout, defaultPublishTimeout, "Maximum time to wait for imgsrv readiness")
	flags.String(flagPublishPollInterval, defaultPublishPollInterval, "imgsrv readiness poll interval")

	mustBindPublishFlag(rt, flags, KeyImgsrvURL, flagImgsrvURL)
	mustBindPublishFlag(rt, flags, KeyImgsrvToken, flagImgsrvToken)
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

func (rt *runtime) imgsrvUploadsClient(cfg publishConfig) (publish.UploadsClient, error) {
	if rt.opts.ImgsrvUploadsClient != nil {
		return rt.opts.ImgsrvUploadsClient, nil
	}

	client, err := imgsrv.New(imgsrv.Options{
		BaseURL:     cfg.imgsrvURL,
		BearerToken: cfg.imgsrvToken,
		UserAgent:   "imgcli/" + rt.opts.version(),
	})
	if err != nil {
		return nil, fmt.Errorf("configure imgsrv client: %w", err)
	}

	return client.Uploads(), nil
}

func publishArtifact(artifact providers.BuiltArtifact) publish.Artifact {
	return publish.Artifact{
		Path:      artifact.Path,
		Size:      artifact.Size,
		SHA256:    artifact.SHA256,
		MediaType: artifact.Plan.MediaType,
	}
}
