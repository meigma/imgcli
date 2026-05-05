package cache_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/cache"
)

var _ cache.Service = (*cache.DiskStore)(nil)

func TestDiskStoreFetchKnownDigest(t *testing.T) {
	body := []byte("provider image bytes")
	server, requests := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.NoError(t, err)
	assert.Equal(t, cache.Blob{
		Path:   cachedBlobPath(root, digest),
		SHA256: digest,
		Size:   int64(len(body)),
	}, blob)
	assert.Equal(t, int64(1), requests.Load())
	assertFileContent(t, blob.Path, body)
	assertReadOnlyFile(t, blob.Path)
}

func TestDiskStoreFetchUsesVerifiedCacheHit(t *testing.T) {
	body := []byte("already cached bytes")
	server, requests := newBlobServer(t, http.StatusInternalServerError, []byte("should not be requested"))
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)
	writeCachedBlob(t, root, digest, body)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.NoError(t, err)
	assert.Equal(t, cachedBlobPath(root, digest), blob.Path)
	assert.Equal(t, digest, blob.SHA256)
	assert.Equal(t, int64(len(body)), blob.Size)
	assert.Equal(t, int64(0), requests.Load())
	assertReadOnlyFile(t, blob.Path)
}

func TestDiskStoreFetchRejectsCacheHitWithConflictingSize(t *testing.T) {
	body := []byte("already cached bytes")
	server, requests := newBlobServer(t, http.StatusInternalServerError, []byte("should not be requested"))
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)
	writeCachedBlob(t, root, digest, body)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)) + 1,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cached blob size conflicts with request")
	assert.Empty(t, blob)
	assert.Equal(t, int64(0), requests.Load())
	assertFileContent(t, cachedBlobPath(root, digest), body)
}

func TestDiskStoreFetchUnknownDigest(t *testing.T) {
	body := []byte("unknown digest bytes")
	server, requests := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL: server.URL + "/blob",
	})

	require.NoError(t, err)
	assert.Equal(t, cache.Blob{
		Path:   cachedBlobPath(root, digest),
		SHA256: digest,
		Size:   int64(len(body)),
	}, blob)
	assert.Equal(t, int64(1), requests.Load())
	assertFileContent(t, blob.Path, body)
	assertReadOnlyFile(t, blob.Path)
}

func TestDiskStoreFetchReplacesCorruptCacheEntry(t *testing.T) {
	body := []byte("correct cached bytes")
	server, requests := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)
	writeCachedBlob(t, root, digest, []byte("corrupt"))

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.NoError(t, err)
	assert.Equal(t, cachedBlobPath(root, digest), blob.Path)
	assert.Equal(t, digest, blob.SHA256)
	assert.Equal(t, int64(1), requests.Load())
	assertFileContent(t, blob.Path, body)
	assertReadOnlyFile(t, blob.Path)
}

func TestDiskStoreFetchReplacesSymlinkCacheEntry(t *testing.T) {
	body := []byte("correct cached bytes")
	server, requests := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)
	path := cachedBlobPath(root, digest)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))

	externalPath := filepath.Join(t.TempDir(), "external")
	require.NoError(t, os.WriteFile(externalPath, body, 0o600))
	if err := os.Symlink(externalPath, path); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.NoError(t, err)
	assert.Equal(t, cachedBlobPath(root, digest), blob.Path)
	assert.Equal(t, digest, blob.SHA256)
	assert.Equal(t, int64(1), requests.Load())
	info, err := os.Lstat(blob.Path)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
	assertFileContent(t, blob.Path, body)
	assertReadOnlyFile(t, blob.Path)
}

func TestDiskStoreFetchRejectsSymlinkShardDirectory(t *testing.T) {
	body := []byte("correct cached bytes")
	server, requests := newBlobServer(t, http.StatusInternalServerError, []byte("should not be requested"))
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)
	shardPath := filepath.Join(root, "blobs", "sha256", digest[:2])
	require.NoError(t, os.MkdirAll(filepath.Dir(shardPath), 0o750))

	externalDir := t.TempDir()
	externalBlob := filepath.Join(externalDir, digest)
	require.NoError(t, os.WriteFile(externalBlob, body, 0o600))
	if err := os.Symlink(externalDir, shardPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "validate cache blob shard directory")
	assert.Empty(t, blob)
	assert.Equal(t, int64(0), requests.Load())
	assertFileContent(t, externalBlob, body)
}

func TestDiskStoreFetchValidatesInputs(t *testing.T) {
	tests := []struct {
		name    string
		req     cache.FetchRequest
		wantErr string
	}{
		{
			name:    "empty URL",
			req:     cache.FetchRequest{ExpectedSHA256: sha256Hex([]byte("bytes"))},
			wantErr: "cache fetch URL is required",
		},
		{
			name: "invalid digest",
			req: cache.FetchRequest{
				URL:            "https://example.invalid/blob",
				ExpectedSHA256: "not-a-sha256",
			},
			wantErr: "cache expected SHA-256 must be",
		},
		{
			name: "negative size",
			req: cache.FetchRequest{
				URL:          "https://example.invalid/blob",
				ExpectedSize: -1,
			},
			wantErr: "cache expected size must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newDiskStore(t, t.TempDir())

			blob, err := store.Fetch(context.Background(), tt.req)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, blob)
		})
	}
}

func TestDiskStoreFetchHandlesHTTPFailure(t *testing.T) {
	body := []byte("unavailable")
	server, _ := newBlobServer(t, http.StatusServiceUnavailable, body)
	root := t.TempDir()
	store := newDiskStore(t, root)
	digest := sha256Hex(body)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL:            server.URL + "/blob",
		ExpectedSHA256: digest,
		ExpectedSize:   int64(len(body)),
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP status 503 Service Unavailable")
	assert.Empty(t, blob)
	assert.NoFileExists(t, cachedBlobPath(root, digest))
	assertTmpEmpty(t, root)
}

func TestDiskStoreFetchHonorsContext(t *testing.T) {
	server, _ := newBlobServer(t, http.StatusOK, []byte("bytes"))
	root := t.TempDir()
	store := newDiskStore(t, root)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	blob, err := store.Fetch(ctx, cache.FetchRequest{
		URL: server.URL + "/blob",
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, blob)
	assertTmpEmpty(t, root)
}

func TestDiskStoreFetchVerifiesDownloadedContent(t *testing.T) {
	tests := []struct {
		name    string
		req     func(url string, body []byte) cache.FetchRequest
		wantErr string
	}{
		{
			name: "digest mismatch",
			req: func(url string, body []byte) cache.FetchRequest {
				return cache.FetchRequest{
					URL:            url,
					ExpectedSHA256: sha256Hex([]byte("different")),
					ExpectedSize:   int64(len(body)),
				}
			},
			wantErr: "verify cache blob SHA-256",
		},
		{
			name: "size mismatch",
			req: func(url string, body []byte) cache.FetchRequest {
				return cache.FetchRequest{
					URL:            url,
					ExpectedSHA256: sha256Hex(body),
					ExpectedSize:   int64(len(body)) + 1,
				}
			},
			wantErr: "verify cache blob size",
		},
		{
			name: "oversized download",
			req: func(url string, body []byte) cache.FetchRequest {
				return cache.FetchRequest{
					URL:            url,
					ExpectedSHA256: sha256Hex(body),
					ExpectedSize:   int64(len(body) - 1),
				}
			},
			wantErr: "verify cache blob size",
		},
		{
			name: "unknown-size download exceeds cache cap",
			req: func(url string, _ []byte) cache.FetchRequest {
				return cache.FetchRequest{
					URL: url,
				}
			},
			wantErr: "unknown-size fetch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := []byte("downloaded bytes")
			server, _ := newBlobServer(t, http.StatusOK, body)
			root := t.TempDir()
			store := newDiskStoreWithOptions(
				t,
				cache.WithRoot(root),
				cache.WithMaxUnknownSizeBytes(int64(len(body)-1)),
			)
			if tt.name != "unknown-size download exceeds cache cap" {
				store = newDiskStore(t, root)
			}

			blob, err := store.Fetch(context.Background(), tt.req(server.URL+"/blob", body))

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, blob)
			assertTmpEmpty(t, root)
			assertNoCachedBlobs(t, root)
		})
	}
}

func TestDiskStoreRepairsUnsafeCacheDirectoryPermissions(t *testing.T) {
	body := []byte("cached bytes")
	server, _ := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	for _, dir := range []string{
		root,
		filepath.Join(root, "blobs"),
		filepath.Join(root, "blobs", "sha256"),
		filepath.Join(root, "tmp"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o750))
		require.NoError(t, os.Chmod(dir, 0o777))
	}
	store := newDiskStore(t, root)

	_, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL: server.URL + "/blob",
	})

	require.NoError(t, err)
	assertDirPerm(t, root, 0o750)
	assertDirPerm(t, filepath.Join(root, "blobs"), 0o750)
	assertDirPerm(t, filepath.Join(root, "blobs", "sha256"), 0o750)
	assertDirPerm(t, filepath.Join(root, "tmp"), 0o700)
}

func TestDiskStoreRejectsSymlinkCacheDirectory(t *testing.T) {
	body := []byte("cached bytes")
	server, requests := newBlobServer(t, http.StatusOK, body)
	root := t.TempDir()
	target := t.TempDir()
	require.NoError(t, os.Symlink(target, filepath.Join(root, "tmp")))
	store := newDiskStore(t, root)

	blob, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL: server.URL + "/blob",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache directory")
	assert.Empty(t, blob)
	assert.Equal(t, int64(0), requests.Load())
}

func TestDiskStoreCreatesRestrictiveCacheDirectories(t *testing.T) {
	body := []byte("cached bytes")
	server, _ := newBlobServer(t, http.StatusOK, body)
	parent := t.TempDir()
	root := filepath.Join(parent, "imgcli-cache")
	store := newDiskStore(t, root)

	_, err := store.Fetch(context.Background(), cache.FetchRequest{
		URL: server.URL + "/blob",
	})

	require.NoError(t, err)
	assertDirPerm(t, root, 0o750)
	assertDirPerm(t, filepath.Join(root, "blobs"), 0o750)
	assertDirPerm(t, filepath.Join(root, "blobs", "sha256"), 0o750)
	assertDirPerm(t, filepath.Join(root, "tmp"), 0o700)
}

func TestNewDiskStoreValidatesOptions(t *testing.T) {
	store, err := cache.NewDiskStore(
		cache.WithRoot(t.TempDir()),
		cache.WithMaxUnknownSizeBytes(-1),
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cache max unknown-size bytes must be non-negative")
	assert.Nil(t, store)
}

func newDiskStore(t *testing.T, root string) *cache.DiskStore {
	t.Helper()

	return newDiskStoreWithOptions(t, cache.WithRoot(root))
}

func newDiskStoreWithOptions(t *testing.T, options ...cache.Option) *cache.DiskStore {
	t.Helper()

	store, err := cache.NewDiskStore(options...)
	require.NoError(t, err)
	return store
}

func newBlobServer(t *testing.T, status int, body []byte) (*httptest.Server, *atomic.Int64) {
	t.Helper()

	requests := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		assert.Equal(t, "/blob", r.URL.Path)
		w.WriteHeader(status)
		_, err := w.Write(body)
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	return server, requests
}

func writeCachedBlob(t *testing.T, root string, digest string, body []byte) {
	t.Helper()

	path := cachedBlobPath(root, digest)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, body, 0o600))
}

func cachedBlobPath(root string, digest string) string {
	return filepath.Join(root, "blobs", "sha256", digest[:2], digest)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func assertFileContent(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func assertTmpEmpty(t *testing.T, root string) {
	t.Helper()

	tmpDir := filepath.Join(root, "tmp")
	entries, err := os.ReadDir(tmpDir)
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func assertNoCachedBlobs(t *testing.T, root string) {
	t.Helper()

	shaDir := filepath.Join(root, "blobs", "sha256")
	err := filepath.WalkDir(shaDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			t.Fatalf("unexpected cached blob %s", path)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return
	}
	require.NoError(t, err)
}

func assertDirPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
	assert.Equal(t, want, info.Mode().Perm())
}

func assertReadOnlyFile(t *testing.T, path string) {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.True(t, info.Mode().IsRegular())
	assert.Equal(t, os.FileMode(0o400), info.Mode().Perm())
}
