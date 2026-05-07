// Package publish publishes built image artifacts to imgsrv.
package publish

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	imgsrv "github.com/meigma/imgsrv/client"
)

const (
	// MinPartSizeBytes is the minimum non-final multipart upload part size imgsrv accepts.
	MinPartSizeBytes int64 = 5 * 1024 * 1024

	// MaxPartSizeBytes is the maximum multipart upload part size imgsrv accepts.
	MaxPartSizeBytes int64 = 5 * 1024 * 1024 * 1024

	maxPartNumber = 10000
	abortTimeout  = 30 * time.Second
)

// UploadsClient is the imgsrv upload operation seam used by the publisher.
type UploadsClient interface {
	BeginUpload(context.Context, imgsrv.BeginUploadRequest) (imgsrv.UploadSession, error)
	PutUploadPart(context.Context, string, int, io.Reader, int64) (imgsrv.UploadPart, error)
	CompleteUpload(context.Context, string, imgsrv.CompleteUploadRequest) (imgsrv.UploadSession, error)
	AbortUpload(context.Context, string) (imgsrv.UploadSession, error)
	GetUpload(context.Context, string) (imgsrv.UploadSession, error)
}

// Artifact is a built local artifact ready to upload.
type Artifact struct {
	Path      string
	Size      int64
	SHA256    string
	MediaType string
}

// Options configures artifact uploads.
type Options struct {
	PartSizeBytes int64
	Wait          bool
	Timeout       time.Duration
	PollInterval  time.Duration
}

// Result describes one uploaded artifact.
type Result struct {
	Digest   imgsrv.Digest
	UploadID imgsrv.UploadID
	State    imgsrv.UploadState
}

// Uploader sends artifacts through imgsrv's multipart upload API.
type Uploader struct {
	client  UploadsClient
	options Options
}

// NewUploader constructs an imgsrv artifact uploader.
func NewUploader(client UploadsClient, options Options) (*Uploader, error) {
	if client == nil {
		return nil, errors.New("configure imgsrv uploader: uploads client is required")
	}
	if options.PartSizeBytes < MinPartSizeBytes {
		return nil, fmt.Errorf("configure imgsrv uploader: part size must be at least %d bytes", MinPartSizeBytes)
	}
	if options.PartSizeBytes > MaxPartSizeBytes {
		return nil, fmt.Errorf("configure imgsrv uploader: part size must be at most %d bytes", MaxPartSizeBytes)
	}
	if options.Wait {
		if options.Timeout <= 0 {
			return nil, errors.New("configure imgsrv uploader: wait timeout must be positive")
		}
		if options.PollInterval <= 0 {
			return nil, errors.New("configure imgsrv uploader: wait poll interval must be positive")
		}
	}

	return &Uploader{
		client:  client,
		options: options,
	}, nil
}

// UploadArtifact uploads one local artifact and optionally waits until imgsrv marks it ready.
func (u *Uploader) UploadArtifact(ctx context.Context, artifact Artifact) (Result, error) {
	if artifact.Path == "" {
		return Result{}, errors.New("upload artifact: path is required")
	}
	if artifact.SHA256 == "" {
		return Result{}, fmt.Errorf("upload artifact %q: sha256 digest is required", artifact.Path)
	}
	if artifact.Size <= 0 {
		return Result{}, fmt.Errorf("upload artifact %q: size must be positive", artifact.Path)
	}
	if parts := partCount(artifact.Size, u.options.PartSizeBytes); parts > maxPartNumber {
		return Result{}, fmt.Errorf(
			"upload artifact %q: %d parts exceeds imgsrv maximum %d; increase --publish-part-size",
			artifact.Path,
			parts,
			maxPartNumber,
		)
	}

	file, err := os.Open(artifact.Path)
	if err != nil {
		return Result{}, fmt.Errorf("open artifact %q: %w", artifact.Path, err)
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return Result{}, fmt.Errorf("stat artifact %q: %w", artifact.Path, err)
	}
	if stat.Size() != artifact.Size {
		return Result{}, fmt.Errorf(
			"upload artifact %q: expected size %d, found %d",
			artifact.Path,
			artifact.Size,
			stat.Size(),
		)
	}

	digest := imgsrv.Digest("sha256:" + artifact.SHA256)
	request := imgsrv.BeginUploadRequest{
		ExpectedDigest:    digest,
		ExpectedSizeBytes: artifact.Size,
		MediaTypeHint:     optionalString(artifact.MediaType),
		FilenameHint:      optionalString(filepath.Base(artifact.Path)),
	}
	session, err := u.client.BeginUpload(ctx, request)
	if err != nil {
		return Result{}, fmt.Errorf("begin imgsrv upload for %s: %w", digest, err)
	}
	if session.State == imgsrv.UploadStateReady {
		return Result{
			Digest:   digest,
			UploadID: session.ID,
			State:    session.State,
		}, nil
	}
	if stateErr := terminalStateError(session); stateErr != nil {
		return Result{}, stateErr
	}
	uploadID := session.ID.String()

	parts, err := u.putParts(ctx, file, uploadID, artifact.Size)
	if err != nil {
		return Result{}, u.abortIncomplete(ctx, uploadID, err)
	}

	session, err = u.client.CompleteUpload(ctx, uploadID, imgsrv.CompleteUploadRequest{Parts: parts})
	if err != nil {
		return Result{}, u.abortIncomplete(ctx, uploadID, fmt.Errorf("complete imgsrv upload %s: %w", uploadID, err))
	}

	if u.options.Wait {
		session, err = u.waitReady(ctx, session)
		if err != nil {
			return Result{}, err
		}
	}

	return Result{
		Digest:   digest,
		UploadID: session.ID,
		State:    session.State,
	}, nil
}

func (u *Uploader) abortIncomplete(ctx context.Context, uploadID string, cause error) error {
	if uploadID == "" {
		return cause
	}

	abortCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), abortTimeout)
	defer cancel()

	if _, abortErr := u.client.AbortUpload(abortCtx, uploadID); abortErr != nil {
		return errors.Join(cause, fmt.Errorf("abort imgsrv upload %s: %w", uploadID, abortErr))
	}

	return cause
}

func (u *Uploader) putParts(
	ctx context.Context,
	file *os.File,
	uploadID string,
	totalSize int64,
) ([]imgsrv.CompleteUploadPart, error) {
	var parts []imgsrv.CompleteUploadPart
	for offset, partNumber := int64(0), 1; offset < totalSize; partNumber++ {
		size := min(u.options.PartSizeBytes, totalSize-offset)
		reader := io.NewSectionReader(file, offset, size)

		part, err := u.client.PutUploadPart(ctx, uploadID, partNumber, reader, size)
		if err != nil {
			return nil, fmt.Errorf("upload imgsrv part %d for %s: %w", partNumber, uploadID, err)
		}

		parts = append(parts, imgsrv.CompleteUploadPart{
			Number:    part.PartNumber,
			ETag:      part.ETag,
			SizeBytes: part.SizeBytes,
		})
		offset += size
	}

	return parts, nil
}

func (u *Uploader) waitReady(ctx context.Context, session imgsrv.UploadSession) (imgsrv.UploadSession, error) {
	if err := terminalStateError(session); err != nil {
		return imgsrv.UploadSession{}, err
	}
	if session.State == imgsrv.UploadStateReady {
		return session, nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, u.options.Timeout)
	defer cancel()

	ticker := time.NewTicker(u.options.PollInterval)
	defer ticker.Stop()

	last := session
	for {
		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return imgsrv.UploadSession{}, fmt.Errorf(
					"upload completed but did not become ready before timeout; last state was %q",
					last.State,
				)
			}
			return imgsrv.UploadSession{}, fmt.Errorf("wait for imgsrv upload %s: %w", session.ID, waitCtx.Err())
		case <-ticker.C:
			current, err := u.client.GetUpload(waitCtx, session.ID.String())
			if err != nil {
				return imgsrv.UploadSession{}, fmt.Errorf("get imgsrv upload %s status: %w", session.ID, err)
			}
			if err := terminalStateError(current); err != nil {
				return imgsrv.UploadSession{}, err
			}
			if current.State == imgsrv.UploadStateReady {
				return current, nil
			}
			last = current
		}
	}
}

func terminalStateError(session imgsrv.UploadSession) error {
	switch session.State {
	case imgsrv.UploadStateCreated,
		imgsrv.UploadStateUploading,
		imgsrv.UploadStateCompleted,
		imgsrv.UploadStateIngesting,
		imgsrv.UploadStateReady:
		return nil
	case imgsrv.UploadStateFailed:
		return fmt.Errorf("imgsrv upload %s failed", session.ID)
	case imgsrv.UploadStateAborted:
		return fmt.Errorf("imgsrv upload %s was aborted", session.ID)
	default:
		return nil
	}
}

func optionalString(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func partCount(size int64, partSize int64) int64 {
	return (size + partSize - 1) / partSize
}
