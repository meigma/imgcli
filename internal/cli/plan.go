package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/providers/incusos/cdn"
	imgschemas "github.com/meigma/imgcli/schemas"
	"github.com/meigma/imgcli/schemas/core"
)

func newPlanCommand(rt *runtime) *cobra.Command {
	return &cobra.Command{
		Use:   "plan CONFIG",
		Short: "Print the resolved artifact plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			config, err := loadImageConfig(args[0])
			if err != nil {
				return err
			}

			plan, err := rt.runIncusOSPlan(cmd.Context(), config)
			if err != nil {
				return err
			}

			return printResolvedPlan(rt.opts.stdout(), plan)
		},
	}
}

func (rt *runtime) runIncusOSPlan(
	ctx context.Context,
	config imgschemas.Config,
) (providers.Plan, error) {
	provider := incusosprovider.New(*config.Incusos, incusosprovider.Options{
		Catalog: rt.incusOSPlanCatalog(),
	})

	return provider.Plan(ctx, providers.PlanRequest{
		Image:     config.Image,
		OutputDir: buildOutputDir(config.Output),
	})
}

func (rt *runtime) incusOSPlanCatalog() incusosprovider.Catalog {
	if rt.opts.IncusOSCatalog != nil {
		return rt.opts.IncusOSCatalog
	}

	options := []cdn.Option{}
	if strings.TrimSpace(rt.opts.IncusOSCDNBaseURL) != "" {
		options = append(options, cdn.WithBaseURL(rt.opts.IncusOSCDNBaseURL))
	}

	return cdn.NewClient(options...)
}

func printResolvedPlan(output io.Writer, plan providers.Plan) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(resolvedPlanForOutput(plan)); err != nil {
		return fmt.Errorf("write resolved artifact plan: %w", err)
	}

	return nil
}

func resolvedPlanForOutput(plan providers.Plan) core.ResolvedPlan {
	resolved := core.ResolvedPlan{
		Image:     plan.Image,
		Version:   plan.Version,
		OutputDir: plan.OutputDir,
		Artifacts: make(map[core.ArtifactKey]core.ResolvedArtifact, len(plan.Artifacts)),
	}

	for _, artifact := range plan.Artifacts {
		resolved.Artifacts[artifact.Key] = resolvedArtifactForOutput(plan, artifact)
	}

	return resolved
}

func resolvedArtifactForOutput(plan providers.Plan, artifact providers.ArtifactPlan) core.ResolvedArtifact {
	return core.ResolvedArtifact{
		ArtifactKey:  artifact.Key,
		ImageName:    string(plan.Image.Name),
		Version:      plan.Version,
		Variant:      artifact.Variant,
		Provider:     plan.Provider,
		Os:           artifact.OperatingSystem,
		Architecture: artifact.Architecture,
		Format:       artifact.Format,
		MediaType:    artifact.MediaType,
		Path:         artifact.OutputPath,
		Labels:       artifact.Labels,
		Annotations:  artifact.Annotations,
		Source:       resolvedArtifactSourceForOutput(artifact),
	}
}

func resolvedArtifactSourceForOutput(artifact providers.ArtifactPlan) *core.ResolvedArtifactSource {
	if artifact.Source == nil {
		return nil
	}

	return &core.ResolvedArtifactSource{
		Version: artifact.Source.Version,
		URL:     artifact.Source.URL,
		Digest:  qualifiedSHA256(artifact.Source.SHA256),
		Size:    artifact.Source.Size,
	}
}

func qualifiedSHA256(sha256Digest string) string {
	if strings.HasPrefix(sha256Digest, "sha256:") {
		return sha256Digest
	}
	return "sha256:" + sha256Digest
}
