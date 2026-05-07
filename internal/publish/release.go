package publish

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	imgsrv "github.com/meigma/imgsrv/client"
)

// CatalogClient is the imgsrv catalog operation seam used by the release publisher.
type CatalogClient interface {
	CreateImage(context.Context, imgsrv.CreateImageRequest) (imgsrv.Image, error)
	CreateDraftVersion(context.Context, string, imgsrv.CreateDraftVersionRequest) (imgsrv.ImageVersion, error)
	AddArtifact(context.Context, string, string, imgsrv.AddArtifactRequest) (imgsrv.Artifact, error)
	PublishVersion(context.Context, string, string) (imgsrv.ImageVersion, error)
	PutAlias(context.Context, string, string, imgsrv.PutAliasRequest) (imgsrv.Alias, error)
}

// Publisher publishes uploaded artifacts into imgsrv image versions.
type Publisher struct {
	uploader *Uploader
	catalog  CatalogClient
}

// ReleaseRequest describes one image release publication.
type ReleaseRequest struct {
	ImageName        string
	ImageDescription string
	Version          string
	Aliases          []string
	Artifacts        []ReleaseArtifact
}

// ReleaseArtifact describes one local artifact to upload and publish.
type ReleaseArtifact struct {
	Key             string
	Variant         string
	LocalPath       string
	OperatingSystem string
	Architecture    string
	Format          imgsrv.ArtifactFormat
	Digest          string
	Size            int64
	MediaType       string
}

// ReleaseResult is the stable JSON result printed by publish.
type ReleaseResult struct {
	Image     string                     `json:"image"`
	Version   string                     `json:"version"`
	State     imgsrv.ImageVersionState   `json:"state"`
	Aliases   []string                   `json:"aliases"`
	Artifacts []PublishedReleaseArtifact `json:"artifacts"`
}

// PublishedReleaseArtifact describes one artifact published into imgsrv.
type PublishedReleaseArtifact struct {
	ArtifactKey      string                `json:"artifactKey"`
	Variant          string                `json:"variant"`
	LocalPath        string                `json:"localPath"`
	ServerArtifactID string                `json:"serverArtifactId"`
	OperatingSystem  string                `json:"operatingSystem"`
	Architecture     string                `json:"architecture"`
	Format           imgsrv.ArtifactFormat `json:"format"`
	Digest           imgsrv.Digest         `json:"digest"`
	Size             int64                 `json:"size"`
	MediaType        string                `json:"mediaType"`
}

type uploadedReleaseArtifact struct {
	request ReleaseArtifact
	upload  Result
}

// NewPublisher constructs a release publisher.
func NewPublisher(catalog CatalogClient, uploader *Uploader) (*Publisher, error) {
	if catalog == nil {
		return nil, errors.New("configure imgsrv publisher: catalog client is required")
	}
	if uploader == nil {
		return nil, errors.New("configure imgsrv publisher: uploader is required")
	}

	return &Publisher{
		uploader: uploader,
		catalog:  catalog,
	}, nil
}

// PublishRelease uploads artifacts, creates a draft version, publishes it, and moves aliases.
func (p *Publisher) PublishRelease(ctx context.Context, request ReleaseRequest) (ReleaseResult, error) {
	if err := validateReleaseRequest(request); err != nil {
		return ReleaseResult{}, err
	}

	uploaded := make([]uploadedReleaseArtifact, 0, len(request.Artifacts))
	for _, artifact := range request.Artifacts {
		result, err := p.uploader.UploadArtifact(ctx, uploadArtifact(artifact))
		if err != nil {
			return ReleaseResult{}, err
		}
		if result.State != imgsrv.UploadStateReady {
			return ReleaseResult{}, fmt.Errorf(
				"publish release artifact %q: upload %s is %q; release publishing requires CAS-ready uploads",
				artifact.Key,
				result.UploadID,
				result.State,
			)
		}
		uploaded = append(uploaded, uploadedReleaseArtifact{
			request: artifact,
			upload:  result,
		})
	}

	if err := p.createImage(ctx, request); err != nil {
		return ReleaseResult{}, err
	}

	version, err := p.catalog.CreateDraftVersion(ctx, request.ImageName, imgsrv.CreateDraftVersionRequest{
		Version: request.Version,
	})
	if err != nil {
		return ReleaseResult{}, fmt.Errorf(
			"create imgsrv draft version %s %s: %w",
			request.ImageName,
			request.Version,
			err,
		)
	}

	result := ReleaseResult{
		Image:     request.ImageName,
		Version:   version.Version,
		State:     version.State,
		Aliases:   []string{},
		Artifacts: make([]PublishedReleaseArtifact, 0, len(uploaded)),
	}
	for _, artifact := range uploaded {
		published, addErr := p.addArtifact(ctx, request, artifact)
		if addErr != nil {
			return ReleaseResult{}, addErr
		}
		result.Artifacts = append(result.Artifacts, published)
	}

	publishedVersion, err := p.catalog.PublishVersion(ctx, request.ImageName, request.Version)
	if err != nil {
		return ReleaseResult{}, fmt.Errorf(
			"publish imgsrv version %s %s: %w",
			request.ImageName,
			request.Version,
			err,
		)
	}
	result.State = publishedVersion.State

	for _, alias := range request.Aliases {
		if _, err := p.catalog.PutAlias(ctx, request.ImageName, alias, imgsrv.PutAliasRequest{
			Version: request.Version,
		}); err != nil {
			return result, fmt.Errorf(
				"published imgsrv version %s %s but failed to set alias %q: %w",
				request.ImageName,
				request.Version,
				alias,
				err,
			)
		}
		result.Aliases = append(result.Aliases, alias)
	}

	return result, nil
}

func validateReleaseRequest(request ReleaseRequest) error {
	if strings.TrimSpace(request.ImageName) == "" {
		return errors.New("publish release: image name is required")
	}
	if strings.TrimSpace(request.Version) == "" {
		return errors.New("publish release: version is required")
	}
	if len(request.Artifacts) == 0 {
		return errors.New("publish release: at least one artifact is required")
	}
	for _, artifact := range request.Artifacts {
		if strings.TrimSpace(artifact.LocalPath) == "" {
			return errors.New("publish release: artifact path is required")
		}
		if strings.TrimSpace(artifact.Digest) == "" {
			return fmt.Errorf("publish release artifact %q: digest is required", artifact.LocalPath)
		}
		if artifact.Size <= 0 {
			return fmt.Errorf("publish release artifact %q: size must be positive", artifact.LocalPath)
		}
	}

	return nil
}

func (p *Publisher) createImage(ctx context.Context, request ReleaseRequest) error {
	_, err := p.catalog.CreateImage(ctx, imgsrv.CreateImageRequest{
		Name:        request.ImageName,
		Description: optionalString(request.ImageDescription),
	})
	if err == nil || isConflict(err) {
		return nil
	}

	return fmt.Errorf("create imgsrv image %s: %w", request.ImageName, err)
}

func (p *Publisher) addArtifact(
	ctx context.Context,
	request ReleaseRequest,
	artifact uploadedReleaseArtifact,
) (PublishedReleaseArtifact, error) {
	added, err := p.catalog.AddArtifact(ctx, request.ImageName, request.Version, imgsrv.AddArtifactRequest{
		OperatingSystem:      artifact.request.OperatingSystem,
		Architecture:         artifact.request.Architecture,
		Format:               artifact.request.Format,
		PrimaryBlobDigest:    artifact.upload.Digest,
		PrimaryBlobSizeBytes: artifact.request.Size,
		PrimaryMediaType:     artifact.request.MediaType,
	})
	if err != nil {
		return PublishedReleaseArtifact{}, fmt.Errorf(
			"add imgsrv artifact %s to %s %s: %w",
			artifact.request.Key,
			request.ImageName,
			request.Version,
			err,
		)
	}

	return PublishedReleaseArtifact{
		ArtifactKey:      artifact.request.Key,
		Variant:          artifact.request.Variant,
		LocalPath:        artifact.request.LocalPath,
		ServerArtifactID: added.ID.String(),
		OperatingSystem:  added.OperatingSystem,
		Architecture:     added.Architecture,
		Format:           added.Format,
		Digest:           added.PrimaryBlobDigest,
		Size:             added.PrimaryBlobSizeBytes,
		MediaType:        added.PrimaryMediaType,
	}, nil
}

func uploadArtifact(artifact ReleaseArtifact) Artifact {
	return Artifact{
		Path:      artifact.LocalPath,
		Size:      artifact.Size,
		SHA256:    artifact.Digest,
		MediaType: artifact.MediaType,
	}
}

func isConflict(err error) bool {
	var problem *imgsrv.ProblemError
	if errors.As(err, &problem) && problem.HTTPStatus == http.StatusConflict {
		return true
	}

	var httpErr *imgsrv.HTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusConflict
}
