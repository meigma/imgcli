// Package cache provides content-addressed local blob caching for providers.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	cacheDirName = "imgcli"

	bytesPerKiB                int64 = 1024
	bytesPerMiB                      = bytesPerKiB * bytesPerKiB
	bytesPerGiB                      = bytesPerMiB * bytesPerKiB
	defaultMaxUnknownSizeGiB         = 64
	defaultMaxUnknownSizeBytes       = defaultMaxUnknownSizeGiB * bytesPerGiB

	blobPerm          = 0o400
	hexCharsPerByte   = 2
	digestShardLength = 2
	dirPerm           = 0o750
	sha256HexLength   = sha256.Size * hexCharsPerByte
	tmpPerm           = 0o700
	writePermMask     = 0o222
)

// Service fetches remote blobs into a verified local cache.
type Service interface {
	// Fetch returns a verified local cache path for the requested blob.
	Fetch(ctx context.Context, req FetchRequest) (Blob, error)
}

// FetchRequest describes a remote blob to fetch into the cache.
type FetchRequest struct {
	// URL is the remote HTTP URL to download.
	URL string

	// ExpectedSHA256 is the optional expected SHA-256 digest in lowercase or uppercase hex.
	ExpectedSHA256 string

	// ExpectedSize is the optional expected blob size in bytes. Zero means unknown.
	ExpectedSize int64
}

// Blob describes a verified blob stored in the cache.
type Blob struct {
	// Path is the local path to the immutable cached blob.
	Path string

	// SHA256 is the blob SHA-256 digest in lowercase hex.
	SHA256 string

	// Size is the blob size in bytes.
	Size int64
}

// Option configures a DiskStore.
type Option func(*DiskStore)

// DiskStore is a filesystem-backed content-addressed cache.
type DiskStore struct {
	root                string
	httpClient          *http.Client
	maxUnknownSizeBytes int64
}

// WithRoot configures the cache root directory.
func WithRoot(root string) Option {
	return func(store *DiskStore) {
		store.root = root
	}
}

// WithMaxUnknownSizeBytes configures the download cap when FetchRequest.ExpectedSize is unknown.
//
// Zero makes unknown-size downloads explicitly unbounded.
func WithMaxUnknownSizeBytes(size int64) Option {
	return func(store *DiskStore) {
		store.maxUnknownSizeBytes = size
	}
}

// NewDiskStore constructs a disk-backed cache store.
func NewDiskStore(options ...Option) (*DiskStore, error) {
	root, err := defaultRoot()
	if err != nil {
		return nil, err
	}

	store := &DiskStore{
		root:                root,
		httpClient:          http.DefaultClient,
		maxUnknownSizeBytes: defaultMaxUnknownSizeBytes,
	}
	for _, option := range options {
		option(store)
	}

	if strings.TrimSpace(store.root) == "" {
		return nil, errors.New("cache root is required")
	}
	if store.maxUnknownSizeBytes < 0 {
		return nil, errors.New("cache max unknown-size bytes must be non-negative")
	}

	return store, nil
}

// Fetch returns a verified local cache path for the requested blob.
func (s *DiskStore) Fetch(ctx context.Context, req FetchRequest) (Blob, error) {
	normalized, err := normalizeFetchRequest(req)
	if err != nil {
		return Blob{}, err
	}

	if normalized.ExpectedSHA256 != "" {
		path := s.blobPath(normalized.ExpectedSHA256)
		if pathErr := s.ensureExpectedDigestPath(path); pathErr != nil {
			return Blob{}, pathErr
		}
		blob, ok, verifyErr := verifyExistingBlob(path, normalized.ExpectedSHA256, normalized.ExpectedSize)
		if verifyErr != nil {
			return Blob{}, verifyErr
		}
		if ok {
			return blob, nil
		}
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return Blob{}, fmt.Errorf("remove corrupt cached blob: %w", removeErr)
		}
	}

	tmpPath, downloaded, err := s.download(ctx, normalized)
	if tmpPath != "" {
		defer os.Remove(tmpPath)
	}
	if err != nil {
		return Blob{}, err
	}

	blob, err := s.publish(tmpPath, downloaded)
	if err != nil {
		return Blob{}, err
	}

	return blob, nil
}

func (s *DiskStore) ensureExpectedDigestPath(path string) error {
	if err := s.ensureDirs(); err != nil {
		return err
	}
	if err := ensureCacheDir(filepath.Dir(path), dirPerm); err != nil {
		return fmt.Errorf("validate cache blob shard directory: %w", err)
	}

	return nil
}

func defaultRoot() (string, error) {
	userCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("resolve user cache directory: %w", err)
	}

	return filepath.Join(userCacheDir, cacheDirName), nil
}

func normalizeFetchRequest(req FetchRequest) (FetchRequest, error) {
	req.URL = strings.TrimSpace(req.URL)
	req.ExpectedSHA256 = strings.ToLower(strings.TrimSpace(req.ExpectedSHA256))

	if req.URL == "" {
		return FetchRequest{}, errors.New("cache fetch URL is required")
	}
	if req.ExpectedSize < 0 {
		return FetchRequest{}, errors.New("cache expected size must be non-negative")
	}
	if req.ExpectedSHA256 != "" {
		if len(req.ExpectedSHA256) != sha256HexLength {
			return FetchRequest{}, fmt.Errorf("cache expected SHA-256 must be %d hex characters", sha256HexLength)
		}
		if _, err := hex.DecodeString(req.ExpectedSHA256); err != nil {
			return FetchRequest{}, fmt.Errorf("cache expected SHA-256 must be hex: %w", err)
		}
	}

	return req, nil
}

func (s *DiskStore) download(ctx context.Context, req FetchRequest) (string, Blob, error) {
	if err := s.ensureDirs(); err != nil {
		return "", Blob{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, req.URL, nil)
	if err != nil {
		return "", Blob{}, fmt.Errorf("create cache fetch request: %w", err)
	}

	resp, err := s.httpClientOrDefault().Do(httpReq)
	if err != nil {
		return "", Blob{}, fmt.Errorf("fetch cache blob: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", Blob{}, fmt.Errorf("fetch cache blob: unexpected HTTP status %s", resp.Status)
	}

	tmp, err := os.CreateTemp(s.tmpDir(), "fetch-*.tmp")
	if err != nil {
		return "", Blob{}, fmt.Errorf("create cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	closeTmp := true
	defer func() {
		if closeTmp {
			_ = tmp.Close()
		}
	}()

	hasher := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, hasher), limitedBody(resp.Body, s.downloadLimit(req)))
	if err != nil {
		return tmpPath, Blob{}, fmt.Errorf("write cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return tmpPath, Blob{}, fmt.Errorf("close cache temp file: %w", err)
	}
	closeTmp = false

	digest := hex.EncodeToString(hasher.Sum(nil))
	if req.ExpectedSize != 0 && size != req.ExpectedSize {
		return tmpPath, Blob{}, fmt.Errorf(
			"verify cache blob size: got %d bytes, want %d bytes",
			size,
			req.ExpectedSize,
		)
	}
	if req.ExpectedSize == 0 && s.maxUnknownSizeBytes != 0 && size > s.maxUnknownSizeBytes {
		return tmpPath, Blob{}, fmt.Errorf(
			"verify cache blob size: got more than %d bytes for unknown-size fetch",
			s.maxUnknownSizeBytes,
		)
	}
	if req.ExpectedSHA256 != "" && digest != req.ExpectedSHA256 {
		return tmpPath, Blob{}, fmt.Errorf("verify cache blob SHA-256: got %s, want %s", digest, req.ExpectedSHA256)
	}
	if err := os.Chmod(tmpPath, blobPerm); err != nil {
		return tmpPath, Blob{}, fmt.Errorf("make cache temp file read-only: %w", err)
	}

	return tmpPath, Blob{
		Path:   s.blobPath(digest),
		SHA256: digest,
		Size:   size,
	}, nil
}

func (s *DiskStore) downloadLimit(req FetchRequest) int64 {
	if req.ExpectedSize != 0 {
		return req.ExpectedSize
	}

	return s.maxUnknownSizeBytes
}

func limitedBody(reader io.Reader, limit int64) io.Reader {
	if limit == 0 {
		return reader
	}

	return io.LimitReader(reader, limit+1)
}

func (s *DiskStore) publish(tmpPath string, downloaded Blob) (Blob, error) {
	finalPath := s.blobPath(downloaded.SHA256)
	if err := ensureCacheDir(filepath.Dir(finalPath), dirPerm); err != nil {
		return Blob{}, fmt.Errorf("create cache blob shard directory: %w", err)
	}

	if err := os.Link(tmpPath, finalPath); err == nil {
		return Blob{
			Path:   finalPath,
			SHA256: downloaded.SHA256,
			Size:   downloaded.Size,
		}, nil
	} else if !errors.Is(err, os.ErrExist) {
		return Blob{}, fmt.Errorf("publish cache blob: %w", err)
	}

	blob, ok, err := verifyExistingBlob(finalPath, downloaded.SHA256, downloaded.Size)
	if err != nil {
		return Blob{}, err
	}
	if ok {
		return blob, nil
	}

	if err := os.Remove(finalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return Blob{}, fmt.Errorf("remove corrupt cached blob: %w", err)
	}
	if err := os.Link(tmpPath, finalPath); err != nil {
		return handlePublishConflict(err, finalPath, downloaded)
	}

	return Blob{
		Path:   finalPath,
		SHA256: downloaded.SHA256,
		Size:   downloaded.Size,
	}, nil
}

func handlePublishConflict(err error, finalPath string, downloaded Blob) (Blob, error) {
	if !errors.Is(err, os.ErrExist) {
		return Blob{}, fmt.Errorf("publish cache blob: %w", err)
	}

	blob, ok, verifyErr := verifyExistingBlob(finalPath, downloaded.SHA256, downloaded.Size)
	if verifyErr != nil {
		return Blob{}, verifyErr
	}
	if ok {
		return blob, nil
	}

	return Blob{}, fmt.Errorf("publish cache blob: existing cache blob %q is corrupt", finalPath)
}

func (s *DiskStore) ensureDirs() error {
	for _, dir := range []struct {
		path string
		perm os.FileMode
	}{
		{path: s.root, perm: dirPerm},
		{path: filepath.Join(s.root, "blobs"), perm: dirPerm},
		{path: filepath.Join(s.root, "blobs", "sha256"), perm: dirPerm},
		{path: s.tmpDir(), perm: tmpPerm},
	} {
		if err := ensureCacheDir(dir.path, dir.perm); err != nil {
			return err
		}
	}

	return nil
}

func ensureCacheDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return fmt.Errorf("create cache directory %q: %w", path, err)
	}
	if err := validateCacheDir(path, perm); err != nil {
		return err
	}

	return nil
}

func validateCacheDir(path string, perm os.FileMode) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect cache directory %q: %w", path, err)
	}
	if !info.Mode().IsDir() {
		return fmt.Errorf("cache directory %q is not a directory", path)
	}
	if info.Mode().Perm() != perm {
		if err := os.Chmod(path, perm); err != nil {
			return fmt.Errorf("repair cache directory permissions %q: %w", path, err)
		}
	}

	return nil
}

func verifyExistingBlob(path string, expectedSHA256 string, expectedSize int64) (Blob, bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Blob{}, false, nil
		}
		return Blob{}, false, fmt.Errorf("inspect cached blob: %w", err)
	}
	if !info.Mode().IsRegular() {
		return Blob{}, false, nil
	}
	digest, size, err := hashFile(path, info)
	if err != nil {
		return Blob{}, false, fmt.Errorf("verify cached blob: %w", err)
	}
	if digest != expectedSHA256 {
		return Blob{}, false, nil
	}
	if expectedSize != 0 && size != expectedSize {
		return Blob{}, false, fmt.Errorf(
			"cached blob size conflicts with request: got %d bytes, want %d bytes",
			size,
			expectedSize,
		)
	}
	if info.Mode().Perm()&writePermMask != 0 {
		if err := os.Chmod(path, blobPerm); err != nil {
			return Blob{}, false, fmt.Errorf("repair cached blob permissions: %w", err)
		}
	}

	return Blob{
		Path:   path,
		SHA256: digest,
		Size:   size,
	}, true, nil
}

func (s *DiskStore) blobPath(sha256Digest string) string {
	return filepath.Join(s.root, "blobs", "sha256", sha256Digest[:digestShardLength], sha256Digest)
}

func (s *DiskStore) tmpDir() string {
	return filepath.Join(s.root, "tmp")
}

func (s *DiskStore) httpClientOrDefault() *http.Client {
	if s.httpClient == nil {
		return http.DefaultClient
	}

	return s.httpClient
}

func hashFile(path string, expected os.FileInfo) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	current, err := os.Lstat(path)
	if err != nil {
		return "", 0, err
	}
	if !current.Mode().IsRegular() || !os.SameFile(expected, current) {
		return "", 0, fmt.Errorf("cached blob changed while opening %q", path)
	}

	info, err := file.Stat()
	if err != nil {
		return "", 0, err
	}
	if !os.SameFile(expected, info) {
		return "", 0, fmt.Errorf("cached blob changed while opening %q", path)
	}

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
