package publish_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	imgsrv "github.com/meigma/imgsrv/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/publish"
	"github.com/meigma/imgcli/internal/publish/mocks"
)

func TestPublisherPublishesReleaseAndAliases(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	artifactBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	artifactPath := writePublishTestArtifact(t, "artifact.raw.gz", artifactBody)
	events := []string{}

	expectReadyUpload(t, uploads, artifactPath, int64(len(artifactBody)), "abc123")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Run(func(_ context.Context, _ imgsrv.CreateImageRequest) { events = append(events, "create-image") }).
		Return(imgsrv.Image{}, &imgsrv.ProblemError{HTTPStatus: http.StatusConflict, Title: "Conflict"}).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Run(func(_ context.Context, _ string, _ imgsrv.CreateDraftVersionRequest) {
			events = append(events, "create-version")
		}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", imgsrv.AddArtifactRequest{
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(artifactBody)),
			PrimaryMediaType:     "application/gzip",
		}).
		Run(func(_ context.Context, _ string, _ string, _ imgsrv.AddArtifactRequest) {
			events = append(events, "add-artifact")
		}).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(artifactBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		PublishVersion(mock.Anything, "incusos", "v1.0.0").
		Run(func(_ context.Context, _ string, _ string) { events = append(events, "publish-version") }).
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: "incusos",
			Version:   "v1.0.0",
			State:     imgsrv.PublishJobStateQueued,
		}, nil).
		Once()
	catalog.EXPECT().
		GetPublishJob(mock.Anything, "publish-job-1").
		Run(func(_ context.Context, _ string) { events = append(events, "get-publish-job") }).
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: "incusos",
			Version:   "v1.0.0",
			State:     imgsrv.PublishJobStateSucceeded,
		}, nil).
		Once()
	catalog.EXPECT().
		PutAlias(mock.Anything, "incusos", "latest", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
		Run(func(_ context.Context, _ string, _ string, _ imgsrv.PutAliasRequest) {
			events = append(events, "alias-latest")
		}).
		Return(imgsrv.Alias{Alias: "latest", Version: "v1.0.0"}, nil).
		Once()
	catalog.EXPECT().
		PutAlias(mock.Anything, "incusos", "prod", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
		Run(func(_ context.Context, _ string, _ string, _ imgsrv.PutAliasRequest) {
			events = append(events, "alias-prod")
		}).
		Return(imgsrv.Alias{Alias: "prod", Version: "v1.0.0"}, nil).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Aliases:   []string{"latest", "prod"},
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(artifactPath, int64(len(artifactBody))),
		},
	})

	require.NoError(t, err)
	assert.Equal(t, "incusos", result.Image)
	assert.Equal(t, "v1.0.0", result.Version)
	assert.Equal(t, imgsrv.ImageVersionStatePublished, result.State)
	assert.Equal(t, []string{"latest", "prod"}, result.Aliases)
	require.Len(t, result.Artifacts, 1)
	assert.Equal(t, publish.PublishedReleaseArtifact{
		ArtifactKey:      "root",
		Variant:          "default",
		LocalPath:        artifactPath,
		ServerArtifactID: "artifact-1",
		OperatingSystem:  "incusos",
		Architecture:     "x86_64",
		Format:           imgsrv.ArtifactFormatRawGZ,
		Digest:           "sha256:abc123",
		Size:             int64(len(artifactBody)),
		MediaType:        "application/gzip",
	}, result.Artifacts[0])
	assert.Equal(t, []string{
		"create-image",
		"create-version",
		"add-artifact",
		"publish-version",
		"get-publish-job",
		"alias-latest",
		"alias-prod",
	}, events)
}

func TestPublisherPublishesMultipleArtifactVariants(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	defaultBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	secureBootBody := bytes.Repeat([]byte("b"), int(publish.MinPartSizeBytes))
	defaultPath := writePublishTestArtifact(t, "default.raw.gz", defaultBody)
	secureBootPath := writePublishTestArtifact(t, "secureboot.raw.gz", secureBootBody)
	defaultArtifact := releaseTestArtifact(defaultPath, int64(len(defaultBody)))
	secureBootArtifact := releaseTestArtifact(secureBootPath, int64(len(secureBootBody)))
	secureBootArtifact.Key = "secureboot"
	secureBootArtifact.Variant = "secureboot"
	secureBootArtifact.Digest = "def456"

	expectReadyUpload(t, uploads, defaultPath, int64(len(defaultBody)), "abc123")
	expectReadyUpload(t, uploads, secureBootPath, int64(len(secureBootBody)), "def456")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", imgsrv.AddArtifactRequest{
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(defaultBody)),
			PrimaryMediaType:     "application/gzip",
		}).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(defaultBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", imgsrv.AddArtifactRequest{
			Variant:              "secureboot",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:def456",
			PrimaryBlobSizeBytes: int64(len(secureBootBody)),
			PrimaryMediaType:     "application/gzip",
		}).
		Return(imgsrv.Artifact{
			ID:                   "artifact-2",
			Variant:              "secureboot",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:def456",
			PrimaryBlobSizeBytes: int64(len(secureBootBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	expectPublishJob(catalog, "incusos", "v1.0.0")

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			defaultArtifact,
			secureBootArtifact,
		},
	})

	require.NoError(t, err)
	assert.Equal(t, publish.ReleaseResult{
		Image:   "incusos",
		Version: "v1.0.0",
		State:   imgsrv.ImageVersionStatePublished,
		Aliases: []string{},
		Artifacts: []publish.PublishedReleaseArtifact{
			{
				ArtifactKey:      "root",
				Variant:          "default",
				LocalPath:        defaultPath,
				ServerArtifactID: "artifact-1",
				OperatingSystem:  "incusos",
				Architecture:     "x86_64",
				Format:           imgsrv.ArtifactFormatRawGZ,
				Digest:           "sha256:abc123",
				Size:             int64(len(defaultBody)),
				MediaType:        "application/gzip",
			},
			{
				ArtifactKey:      "secureboot",
				Variant:          "secureboot",
				LocalPath:        secureBootPath,
				ServerArtifactID: "artifact-2",
				OperatingSystem:  "incusos",
				Architecture:     "x86_64",
				Format:           imgsrv.ArtifactFormatRawGZ,
				Digest:           "sha256:def456",
				Size:             int64(len(secureBootBody)),
				MediaType:        "application/gzip",
			},
		},
	}, result)
}

func TestPublisherFailsOnDraftVersionConflict(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	artifactBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	artifactPath := writePublishTestArtifact(t, "artifact.raw.gz", artifactBody)
	conflict := &imgsrv.ProblemError{HTTPStatus: http.StatusConflict, Title: "Conflict"}

	expectReadyUpload(t, uploads, artifactPath, int64(len(artifactBody)), "abc123")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{}, conflict).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(artifactPath, int64(len(artifactBody))),
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "create imgsrv draft version incusos v1.0.0")
	require.ErrorIs(t, err, conflict)
	assert.Empty(t, result)
}

func TestPublisherFailsBeforeCatalogWhenUploadIsNotReady(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	artifactBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	artifactPath := writePublishTestArtifact(t, "artifact.raw.gz", artifactBody)
	mediaTypeHint := "application/gzip"
	filenameHint := filepath.Base(artifactPath)

	uploads.EXPECT().
		BeginUpload(mock.Anything, imgsrv.BeginUploadRequest{
			ExpectedDigest:    "sha256:abc123",
			ExpectedSizeBytes: int64(len(artifactBody)),
			MediaTypeHint:     &mediaTypeHint,
			FilenameHint:      &filenameHint,
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCreated}, nil).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 1, mock.Anything, int64(len(artifactBody))).
		Return(imgsrv.UploadPart{PartNumber: 1, ETag: "etag-1", SizeBytes: int64(len(artifactBody))}, nil).
		Once()
	uploads.EXPECT().
		CompleteUpload(mock.Anything, "upload-1", imgsrv.CompleteUploadRequest{
			Parts: []imgsrv.CompleteUploadPart{{Number: 1, ETag: "etag-1", SizeBytes: int64(len(artifactBody))}},
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCompleted}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          false,
	})
	publisher, err := publish.NewPublisher(catalog, uploader)
	require.NoError(t, err)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(artifactPath, int64(len(artifactBody))),
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, `publish release artifact "root": upload upload-1 is "completed"`)
	require.ErrorContains(t, err, "release publishing requires CAS-ready uploads")
	assert.Empty(t, result)
}

func TestPublisherDeletesAddedArtifactsWhenLaterAddArtifactFails(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	defaultBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	secureBootBody := bytes.Repeat([]byte("b"), int(publish.MinPartSizeBytes))
	defaultPath := writePublishTestArtifact(t, "default.raw.gz", defaultBody)
	secureBootPath := writePublishTestArtifact(t, "secureboot.raw.gz", secureBootBody)
	secureBootArtifact := releaseTestArtifact(secureBootPath, int64(len(secureBootBody)))
	secureBootArtifact.Key = "secureboot"
	secureBootArtifact.Variant = "secureboot"
	secureBootArtifact.Digest = "def456"
	addErr := errors.New("artifact rejected")

	expectReadyUpload(t, uploads, defaultPath, int64(len(defaultBody)), "abc123")
	expectReadyUpload(t, uploads, secureBootPath, int64(len(secureBootBody)), "def456")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(defaultBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{}, addErr).
		Once()
	catalog.EXPECT().
		DeleteArtifact(mock.Anything, "incusos", "v1.0.0", "artifact-1").
		Return(nil).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(defaultPath, int64(len(defaultBody))),
			secureBootArtifact,
		},
	})

	require.ErrorIs(t, err, addErr)
	assert.Empty(t, result)
}

func TestPublisherDoesNotDeleteAddedArtifactsWhenPublishVersionFails(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	defaultBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	secureBootBody := bytes.Repeat([]byte("b"), int(publish.MinPartSizeBytes))
	defaultPath := writePublishTestArtifact(t, "default.raw.gz", defaultBody)
	secureBootPath := writePublishTestArtifact(t, "secureboot.raw.gz", secureBootBody)
	secureBootArtifact := releaseTestArtifact(secureBootPath, int64(len(secureBootBody)))
	secureBootArtifact.Key = "secureboot"
	secureBootArtifact.Variant = "secureboot"
	secureBootArtifact.Digest = "def456"
	publishErr := errors.New("publish denied")

	expectReadyUpload(t, uploads, defaultPath, int64(len(defaultBody)), "abc123")
	expectReadyUpload(t, uploads, secureBootPath, int64(len(secureBootBody)), "def456")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(defaultBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-2",
			Variant:              "secureboot",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:def456",
			PrimaryBlobSizeBytes: int64(len(secureBootBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		PublishVersion(mock.Anything, "incusos", "v1.0.0").
		Return(imgsrv.PublishJob{}, publishErr).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(defaultPath, int64(len(defaultBody))),
			secureBootArtifact,
		},
	})

	require.ErrorIs(t, err, publishErr)
	require.ErrorContains(t, err, "publish imgsrv version incusos v1.0.0")
	assert.Empty(t, result)
}

func TestPublisherJoinsDraftArtifactCleanupFailure(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	defaultBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	secureBootBody := bytes.Repeat([]byte("b"), int(publish.MinPartSizeBytes))
	defaultPath := writePublishTestArtifact(t, "default.raw.gz", defaultBody)
	secureBootPath := writePublishTestArtifact(t, "secureboot.raw.gz", secureBootBody)
	secureBootArtifact := releaseTestArtifact(secureBootPath, int64(len(secureBootBody)))
	secureBootArtifact.Key = "secureboot"
	secureBootArtifact.Variant = "secureboot"
	secureBootArtifact.Digest = "def456"
	addErr := errors.New("artifact rejected")
	cleanupErr := errors.New("cleanup denied")

	expectReadyUpload(t, uploads, defaultPath, int64(len(defaultBody)), "abc123")
	expectReadyUpload(t, uploads, secureBootPath, int64(len(secureBootBody)), "def456")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(defaultBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{}, addErr).
		Once()
	catalog.EXPECT().
		DeleteArtifact(mock.Anything, "incusos", "v1.0.0", "artifact-1").
		Return(cleanupErr).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(defaultPath, int64(len(defaultBody))),
			secureBootArtifact,
		},
	})

	require.ErrorIs(t, err, addErr)
	require.ErrorIs(t, err, cleanupErr)
	require.ErrorContains(t, err, "delete draft imgsrv artifact artifact-1")
	assert.Empty(t, result)
}

func TestPublisherDoesNotDeleteArtifactsWhenPublishJobFails(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	artifactBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	artifactPath := writePublishTestArtifact(t, "artifact.raw.gz", artifactBody)
	failureMessage := "manifest generation failed"

	expectReadyUpload(t, uploads, artifactPath, int64(len(artifactBody)), "abc123")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(artifactBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		PublishVersion(mock.Anything, "incusos", "v1.0.0").
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: "incusos",
			Version:   "v1.0.0",
			State:     imgsrv.PublishJobStateQueued,
		}, nil).
		Once()
	catalog.EXPECT().
		GetPublishJob(mock.Anything, "publish-job-1").
		Return(imgsrv.PublishJob{
			ID:             "publish-job-1",
			ImageName:      "incusos",
			Version:        "v1.0.0",
			State:          imgsrv.PublishJobStateFailed,
			FailureMessage: &failureMessage,
		}, nil).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(artifactPath, int64(len(artifactBody))),
		},
	})

	require.ErrorContains(t, err, "publish imgsrv job publish-job-1 failed: manifest generation failed")
	assert.Empty(t, result)
}

func TestPublisherSurfacesPartialAliasFailure(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	catalog := mocks.NewMockCatalogClient(t)
	artifactBody := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	artifactPath := writePublishTestArtifact(t, "artifact.raw.gz", artifactBody)
	aliasErr := errors.New("policy denied")

	expectReadyUpload(t, uploads, artifactPath, int64(len(artifactBody)), "abc123")
	catalog.EXPECT().
		CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "incusos"}).
		Return(imgsrv.Image{Name: "incusos"}, nil).
		Once()
	catalog.EXPECT().
		CreateDraftVersion(mock.Anything, "incusos", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
		Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
		Once()
	catalog.EXPECT().
		AddArtifact(mock.Anything, "incusos", "v1.0.0", mock.Anything).
		Return(imgsrv.Artifact{
			ID:                   "artifact-1",
			Variant:              "default",
			OperatingSystem:      "incusos",
			Architecture:         "x86_64",
			Format:               imgsrv.ArtifactFormatRawGZ,
			PrimaryBlobDigest:    "sha256:abc123",
			PrimaryBlobSizeBytes: int64(len(artifactBody)),
			PrimaryMediaType:     "application/gzip",
		}, nil).
		Once()
	catalog.EXPECT().
		PublishVersion(mock.Anything, "incusos", "v1.0.0").
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: "incusos",
			Version:   "v1.0.0",
			State:     imgsrv.PublishJobStateQueued,
		}, nil).
		Once()
	catalog.EXPECT().
		GetPublishJob(mock.Anything, "publish-job-1").
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: "incusos",
			Version:   "v1.0.0",
			State:     imgsrv.PublishJobStateSucceeded,
		}, nil).
		Once()
	catalog.EXPECT().
		PutAlias(mock.Anything, "incusos", "latest", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
		Return(imgsrv.Alias{Alias: "latest", Version: "v1.0.0"}, nil).
		Once()
	catalog.EXPECT().
		PutAlias(mock.Anything, "incusos", "prod", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
		Return(imgsrv.Alias{}, aliasErr).
		Once()

	publisher := newReleaseTestPublisher(t, catalog, uploads)
	result, err := publisher.PublishRelease(context.Background(), publish.ReleaseRequest{
		ImageName: "incusos",
		Version:   "v1.0.0",
		Aliases:   []string{"latest", "prod"},
		Artifacts: []publish.ReleaseArtifact{
			releaseTestArtifact(artifactPath, int64(len(artifactBody))),
		},
	})

	require.Error(t, err)
	require.ErrorContains(t, err, `published imgsrv version incusos v1.0.0 but failed to set alias "prod"`)
	require.ErrorIs(t, err, aliasErr)
	assert.Equal(t, imgsrv.ImageVersionStatePublished, result.State)
	assert.Equal(t, []string{"latest"}, result.Aliases)
	require.Len(t, result.Artifacts, 1)
}

func expectReadyUpload(
	t *testing.T,
	uploads *mocks.MockUploadsClient,
	path string,
	size int64,
	sha256 string,
) {
	t.Helper()

	mediaTypeHint := "application/gzip"
	filenameHint := filepath.Base(path)
	uploads.EXPECT().
		BeginUpload(mock.Anything, imgsrv.BeginUploadRequest{
			ExpectedDigest:    imgsrv.Digest("sha256:" + sha256),
			ExpectedSizeBytes: size,
			MediaTypeHint:     &mediaTypeHint,
			FilenameHint:      &filenameHint,
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateReady}, nil).
		Once()
}

func releaseTestArtifact(path string, size int64) publish.ReleaseArtifact {
	return publish.ReleaseArtifact{
		Key:             "root",
		Variant:         "default",
		LocalPath:       path,
		OperatingSystem: "incusos",
		Architecture:    "x86_64",
		Format:          imgsrv.ArtifactFormatRawGZ,
		Digest:          "abc123",
		Size:            size,
		MediaType:       "application/gzip",
	}
}

func expectPublishJob(catalog *mocks.MockCatalogClient, image string, version string) {
	catalog.EXPECT().
		PublishVersion(mock.Anything, image, version).
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: image,
			Version:   version,
			State:     imgsrv.PublishJobStateQueued,
		}, nil).
		Once()
	catalog.EXPECT().
		GetPublishJob(mock.Anything, "publish-job-1").
		Return(imgsrv.PublishJob{
			ID:        "publish-job-1",
			ImageName: image,
			Version:   version,
			State:     imgsrv.PublishJobStateSucceeded,
		}, nil).
		Once()
}

func newReleaseTestPublisher(
	t *testing.T,
	catalog publish.CatalogClient,
	uploads publish.UploadsClient,
) *publish.Publisher {
	t.Helper()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          true,
		Timeout:       time.Second,
		PollInterval:  time.Nanosecond,
	})
	publisher, err := publish.NewPublisher(catalog, uploader, publish.PublisherOptions{
		Timeout:      time.Second,
		PollInterval: time.Nanosecond,
	})
	require.NoError(t, err)
	return publisher
}
