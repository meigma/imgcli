package publish_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
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

func TestUploaderUploadsMultipartArtifact(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := append(bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes)), []byte("end")...)
	path := writePublishTestArtifact(t, "artifact.raw.gz", body)
	artifact := publish.Artifact{
		Path:      path,
		Size:      int64(len(body)),
		SHA256:    "abc123",
		MediaType: "application/gzip",
	}
	mediaTypeHint := "application/gzip"
	filenameHint := "artifact.raw.gz"

	beginRequest := imgsrv.BeginUploadRequest{
		ExpectedDigest:    "sha256:abc123",
		ExpectedSizeBytes: int64(len(body)),
		MediaTypeHint:     &mediaTypeHint,
		FilenameHint:      &filenameHint,
	}
	uploads.EXPECT().
		BeginUpload(mock.Anything, beginRequest).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCreated}, nil).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 1, mock.Anything, publish.MinPartSizeBytes).
		RunAndReturn(func(_ context.Context, _ string, _ int, reader io.Reader, _ int64) (imgsrv.UploadPart, error) {
			assertReaderBytes(t, reader, body[:int(publish.MinPartSizeBytes)])
			return imgsrv.UploadPart{PartNumber: 1, ETag: "etag-1", SizeBytes: publish.MinPartSizeBytes}, nil
		}).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 2, mock.Anything, int64(3)).
		RunAndReturn(func(_ context.Context, _ string, _ int, reader io.Reader, _ int64) (imgsrv.UploadPart, error) {
			assertReaderBytes(t, reader, []byte("end"))
			return imgsrv.UploadPart{PartNumber: 2, ETag: "etag-2", SizeBytes: 3}, nil
		}).
		Once()
	uploads.EXPECT().
		CompleteUpload(mock.Anything, "upload-1", imgsrv.CompleteUploadRequest{
			Parts: []imgsrv.CompleteUploadPart{
				{Number: 1, ETag: "etag-1", SizeBytes: publish.MinPartSizeBytes},
				{Number: 2, ETag: "etag-2", SizeBytes: 3},
			},
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCompleted}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          false,
	})
	result, err := uploader.UploadArtifact(context.Background(), artifact)

	require.NoError(t, err)
	assert.Equal(t, imgsrv.Digest("sha256:abc123"), result.Digest)
	assert.Equal(t, imgsrv.UploadID("upload-1"), result.UploadID)
	assert.Equal(t, imgsrv.UploadStateCompleted, result.State)
}

func TestUploaderSkipsMultipartUploadWhenDigestIsReady(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw.gz", body)
	filenameHint := "artifact.raw.gz"
	uploads.EXPECT().
		BeginUpload(mock.Anything, imgsrv.BeginUploadRequest{
			ExpectedDigest:    "sha256:abc123",
			ExpectedSizeBytes: int64(len(body)),
			FilenameHint:      &filenameHint,
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateReady}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          true,
		Timeout:       time.Second,
		PollInterval:  time.Nanosecond,
	})
	result, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.NoError(t, err)
	assert.Equal(t, imgsrv.Digest("sha256:abc123"), result.Digest)
	assert.Equal(t, imgsrv.UploadID("upload-1"), result.UploadID)
	assert.Equal(t, imgsrv.UploadStateReady, result.State)
}

func TestUploaderWaitsUntilReady(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)

	expectSinglePartUpload(t, uploads, int64(len(body)), imgsrv.UploadStateCompleted)
	uploads.EXPECT().
		GetUpload(mock.Anything, "upload-1").
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateIngesting}, nil).
		Once()
	uploads.EXPECT().
		GetUpload(mock.Anything, "upload-1").
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateReady}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          true,
		Timeout:       time.Second,
		PollInterval:  time.Nanosecond,
	})
	result, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.NoError(t, err)
	assert.Equal(t, imgsrv.UploadStateReady, result.State)
}

func TestUploaderFailsWhenUploadFails(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)
	expectSinglePartUpload(t, uploads, int64(len(body)), imgsrv.UploadStateFailed)

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          true,
		Timeout:       time.Second,
		PollInterval:  time.Nanosecond,
	})
	_, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "imgsrv upload upload-1 failed")
}

func TestUploaderTimesOutWhenUploadNeverBecomesReady(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)
	expectSinglePartUpload(t, uploads, int64(len(body)), imgsrv.UploadStateCompleted)

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          true,
		Timeout:       time.Nanosecond,
		PollInterval:  time.Hour,
	})
	_, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "upload completed but did not become ready before timeout")
	require.ErrorContains(t, err, `last state was "completed"`)
}

func TestUploaderSurfacesProblemErrorWithContext(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)
	problem := &imgsrv.ProblemError{
		HTTPStatus: http.StatusInternalServerError,
		Title:      "Upload failed",
		Detail:     "object store unavailable",
	}
	uploads.EXPECT().
		BeginUpload(mock.Anything, mock.Anything).
		Return(imgsrv.UploadSession{}, problem).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          false,
	})
	_, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "begin imgsrv upload for sha256:abc123")
	require.ErrorContains(t, err, "imgsrv: 500 Upload failed: object store unavailable")
	var gotProblem *imgsrv.ProblemError
	require.ErrorAs(t, err, &gotProblem)
}

func TestUploaderSurfacesHTTPErrorWithContext(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)
	httpErr := &imgsrv.HTTPError{
		StatusCode: http.StatusServiceUnavailable,
		Status:     "503 Service Unavailable",
		Body:       []byte("down"),
	}
	uploads.EXPECT().
		BeginUpload(mock.Anything, mock.Anything).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCreated}, nil).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 1, mock.Anything, publish.MinPartSizeBytes).
		Return(imgsrv.UploadPart{}, httpErr).
		Once()
	uploads.EXPECT().
		AbortUpload(mock.Anything, "upload-1").
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateAborted}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          false,
	})
	_, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.Error(t, err)
	require.ErrorContains(t, err, "upload imgsrv part 1 for upload-1")
	require.ErrorContains(t, err, "imgsrv: 503 Service Unavailable: down")
	var gotHTTP *imgsrv.HTTPError
	require.ErrorAs(t, err, &gotHTTP)
}

func TestUploaderAbortsWhenCompleteFails(t *testing.T) {
	uploads := mocks.NewMockUploadsClient(t)
	body := bytes.Repeat([]byte("a"), int(publish.MinPartSizeBytes))
	path := writePublishTestArtifact(t, "artifact.raw", body)
	completeErr := errors.New("object store failed")
	uploads.EXPECT().
		BeginUpload(mock.Anything, mock.Anything).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCreated}, nil).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 1, mock.Anything, publish.MinPartSizeBytes).
		Return(imgsrv.UploadPart{PartNumber: 1, ETag: "etag-1", SizeBytes: publish.MinPartSizeBytes}, nil).
		Once()
	uploads.EXPECT().
		CompleteUpload(mock.Anything, "upload-1", imgsrv.CompleteUploadRequest{
			Parts: []imgsrv.CompleteUploadPart{{Number: 1, ETag: "etag-1", SizeBytes: publish.MinPartSizeBytes}},
		}).
		Return(imgsrv.UploadSession{}, completeErr).
		Once()
	uploads.EXPECT().
		AbortUpload(mock.Anything, "upload-1").
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateAborted}, nil).
		Once()

	uploader := newPublishTestUploader(t, uploads, publish.Options{
		PartSizeBytes: publish.MinPartSizeBytes,
		Wait:          false,
	})
	_, err := uploader.UploadArtifact(context.Background(), publish.Artifact{
		Path:   path,
		Size:   int64(len(body)),
		SHA256: "abc123",
	})

	require.ErrorIs(t, err, completeErr)
	require.ErrorContains(t, err, "complete imgsrv upload upload-1")
}

func expectSinglePartUpload(
	t *testing.T,
	uploads *mocks.MockUploadsClient,
	size int64,
	completeState imgsrv.UploadState,
) {
	t.Helper()

	uploads.EXPECT().
		BeginUpload(mock.Anything, mock.Anything).
		Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateCreated}, nil).
		Once()
	uploads.EXPECT().
		PutUploadPart(mock.Anything, "upload-1", 1, mock.Anything, size).
		Return(imgsrv.UploadPart{PartNumber: 1, ETag: "etag-1", SizeBytes: size}, nil).
		Once()
	uploads.EXPECT().
		CompleteUpload(mock.Anything, "upload-1", imgsrv.CompleteUploadRequest{
			Parts: []imgsrv.CompleteUploadPart{{Number: 1, ETag: "etag-1", SizeBytes: size}},
		}).
		Return(imgsrv.UploadSession{ID: "upload-1", State: completeState}, nil).
		Once()
}

func newPublishTestUploader(t *testing.T, uploads publish.UploadsClient, options publish.Options) *publish.Uploader {
	t.Helper()

	uploader, err := publish.NewUploader(uploads, options)
	require.NoError(t, err)
	return uploader
}

func writePublishTestArtifact(t *testing.T, name string, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, body, 0o600))
	return path
}

func assertReaderBytes(t *testing.T, reader io.Reader, want []byte) {
	t.Helper()

	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
