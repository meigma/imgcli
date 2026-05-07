package providers

import (
	"context"

	"github.com/meigma/imgcli/schemas/core"
)

// Provider plans and builds artifacts for one provider-specific configuration.
type Provider interface {
	// Name returns the provider name used in plans and artifact metadata.
	Name() core.ProviderName

	// Plan resolves provider-specific configuration into concrete artifact work.
	Plan(ctx context.Context, req PlanRequest) (Plan, error)

	// Build creates artifacts from an already resolved provider plan.
	Build(ctx context.Context, req BuildRequest) (BuildResult, error)
}

// PlanRequest carries command-level inputs shared by all providers.
type PlanRequest struct {
	// Image is the top-level image identity from the imgcli configuration.
	Image core.Image

	// Version is the optional release version supplied by the command or release pipeline.
	Version string

	// OutputDir is the root directory where local artifacts should be planned.
	OutputDir string
}

// BuildRequest carries the provider plan and build-time locations.
type BuildRequest struct {
	// Plan is the concrete artifact work to execute.
	Plan Plan

	// CacheDir is the directory providers may use for reusable downloads or intermediates.
	CacheDir string

	// OutputDir is the root directory where local artifacts should be written.
	OutputDir string
}

// Plan is the provider-neutral representation printed by plan and consumed by build.
type Plan struct {
	// Provider is the provider responsible for this plan.
	Provider core.ProviderName

	// Image is the top-level image identity from the imgcli configuration.
	Image core.Image

	// Version is the optional release version supplied by the command or release pipeline.
	Version string

	// OutputDir is the root directory where local artifacts should be written.
	OutputDir string

	// Artifacts is the concrete artifact work in this plan.
	Artifacts []ArtifactPlan
}

// ArtifactPlan describes one concrete artifact a provider can build.
type ArtifactPlan struct {
	// Key is the local handle for the artifact.
	Key core.ArtifactKey

	// Variant is the provider variant that produces this artifact.
	Variant core.VariantName

	// Architecture is the target architecture for this artifact.
	Architecture core.Architecture

	// OperatingSystem is the artifact operating-system token published to imgsrv.
	OperatingSystem string

	// Format is the artifact file format.
	Format core.ArtifactFormat

	// MediaType is the content type expected for the artifact.
	MediaType string

	// OutputPath is the planned local artifact path.
	OutputPath string

	// Labels are provider or user labels copied to artifact metadata.
	Labels map[string]string

	// Annotations are provider or user annotations copied to artifact metadata.
	Annotations map[string]string
}

// BuildResult describes artifacts produced by a provider build.
type BuildResult struct {
	// Plan is the provider plan that was executed.
	Plan Plan

	// Artifacts are the files produced by the build.
	Artifacts []BuiltArtifact
}

// BuiltArtifact describes one artifact written by a provider.
type BuiltArtifact struct {
	// Plan is the planned artifact that produced this file.
	Plan ArtifactPlan

	// Path is the final local artifact path.
	Path string

	// Size is the artifact size in bytes.
	Size int64

	// SHA256 is the artifact SHA-256 digest in lowercase hex.
	SHA256 string
}
