package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/meigma/imgcli/internal/providers"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
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

			plan, err := runIncusOSPlan(cmd.Context(), config)
			if err != nil {
				return err
			}

			return printResolvedPlan(rt.opts.stdout(), plan)
		},
	}
}

func runIncusOSPlan(
	ctx context.Context,
	config imgschemas.Config,
) (providers.Plan, error) {
	provider := incusosprovider.New(*config.Incusos, incusosprovider.Options{})

	return provider.Plan(ctx, providers.PlanRequest{
		Image:     config.Image,
		OutputDir: buildOutputDir(config.Output),
	})
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
	}
}
