package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/cache"
	incusosprovider "github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/schemas/core"
)

func TestWithLockedCachePrunesAfterSuccess(t *testing.T) {
	clearIMGCLIEnv(t)
	root := t.TempDir()
	oldDigest := sha256HexForBuildTest([]byte("old!"))
	recentDigest := sha256HexForBuildTest([]byte("new?"))

	err := withLockedCache(context.Background(), Config{
		CacheDir:          root,
		CacheMaxSizeBytes: 4,
	}, "", func(catalog incusosprovider.Catalog, downloader incusosprovider.Downloader) error {
		require.NotNil(t, catalog)
		require.NotNil(t, downloader)
		assertCacheLocked(t, root)
		writeBuildTestCachedBlob(t, root, []byte("old!"), time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
		writeBuildTestCachedBlob(t, root, []byte("new?"), time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC))
		return nil
	})

	require.NoError(t, err)
	assert.NoFileExists(t, buildTestCachedBlobPath(root, oldDigest))
	assert.FileExists(t, buildTestCachedBlobPath(root, recentDigest))
	assertCacheUnlocked(t, root)
}

func TestWithLockedCacheSkipsPruneAfterBuildError(t *testing.T) {
	clearIMGCLIEnv(t)
	root := t.TempDir()
	buildErr := errors.New("build failed")
	oldDigest := sha256HexForBuildTest([]byte("old!"))

	err := withLockedCache(context.Background(), Config{
		CacheDir:          root,
		CacheMaxSizeBytes: 1,
	}, "", func(_ incusosprovider.Catalog, _ incusosprovider.Downloader) error {
		assertCacheLocked(t, root)
		writeBuildTestCachedBlob(t, root, []byte("old!"), time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC))
		return buildErr
	})

	require.ErrorIs(t, err, buildErr)
	assert.FileExists(t, buildTestCachedBlobPath(root, oldDigest))
	assertCacheUnlocked(t, root)
}

func TestWithLockedCacheUsesIncusOSCDNBaseURL(t *testing.T) {
	clearIMGCLIEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/index.json", r.URL.Path)
		_, err := w.Write([]byte(`{
			"updates": [{
				"channels": ["testing"],
				"version": "202604261712",
				"url": "/202604261712",
				"files": [{
					"architecture": "x86_64",
					"component": "os",
					"filename": "x86_64/IncusOS_202604261712.img.gz",
					"sha256": "test-sha",
					"size": 42,
					"type": "image-raw"
				}]
			}]
		}`))
		assert.NoError(t, err)
	}))
	t.Cleanup(server.Close)

	err := withLockedCache(context.Background(), Config{
		CacheDir:          t.TempDir(),
		CacheMaxSizeBytes: 0,
	}, server.URL, func(catalog incusosprovider.Catalog, _ incusosprovider.Downloader) error {
		asset, resolveErr := catalog.ResolveImage(context.Background(), incusosprovider.ImageQuery{
			Channel:      incusosprovider.ChannelTesting,
			Version:      incusosprovider.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusosprovider.ImageTypeRaw,
		})
		require.NoError(t, resolveErr)
		assert.Equal(t, server.URL+"/202604261712/x86_64/IncusOS_202604261712.img.gz", asset.URL)
		return nil
	})

	require.NoError(t, err)
}

func assertCacheLocked(t *testing.T, root string) {
	t.Helper()

	store, err := cache.NewDiskStore(cache.WithRoot(root))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	lock, err := store.Lock(ctx)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, lock)
}

func assertCacheUnlocked(t *testing.T, root string) {
	t.Helper()

	store, err := cache.NewDiskStore(cache.WithRoot(root))
	require.NoError(t, err)
	lock, err := store.Lock(context.Background())
	require.NoError(t, err)
	require.NoError(t, lock.Unlock())
}

func writeBuildTestCachedBlob(t *testing.T, root string, body []byte, modTime time.Time) {
	t.Helper()

	digest := sha256HexForBuildTest(body)
	path := buildTestCachedBlobPath(root, digest)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, body, 0o400))
	require.NoError(t, os.Chtimes(path, modTime, modTime))
}

func buildTestCachedBlobPath(root string, digest string) string {
	return filepath.Join(root, "blobs", "sha256", digest[:2], digest)
}

func sha256HexForBuildTest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
