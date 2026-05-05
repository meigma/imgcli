package cdn

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/cache"
	cachemocks "github.com/meigma/imgcli/internal/cache/mocks"
	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/schemas/core"
)

func TestDownloadImage(t *testing.T) {
	tests := []struct {
		name     string
		asset    incusos.ImageAsset
		blob     cache.Blob
		wantReq  cache.FetchRequest
		wantSize int64
	}{
		{
			name:  "downloads known-size asset through cache",
			asset: downloadAsset(42),
			blob: cache.Blob{
				Path:   "/cache/blobs/sha256/aa/blob",
				SHA256: strings.Repeat("a", 64),
				Size:   42,
			},
			wantReq: cache.FetchRequest{
				URL:            "https://example.invalid/incusos.img.gz",
				ExpectedSHA256: strings.Repeat("a", 64),
				ExpectedSize:   42,
			},
			wantSize: 42,
		},
		{
			name:  "passes unknown size through to cache",
			asset: downloadAsset(0),
			blob: cache.Blob{
				Path:   "/cache/blobs/sha256/aa/blob",
				SHA256: strings.Repeat("a", 64),
				Size:   99,
			},
			wantReq: cache.FetchRequest{
				URL:            "https://example.invalid/incusos.img.gz",
				ExpectedSHA256: strings.Repeat("a", 64),
				ExpectedSize:   0,
			},
			wantSize: 99,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			cacheService := cachemocks.NewMockService(t)
			cacheService.EXPECT().Fetch(ctx, tt.wantReq).Return(tt.blob, nil).Once()
			client := NewClient(WithCacheService(cacheService))

			got, err := client.DownloadImage(ctx, tt.asset)

			require.NoError(t, err)
			assert.Equal(t, incusos.DownloadedImage{
				Asset:  tt.asset,
				Path:   tt.blob.Path,
				SHA256: tt.blob.SHA256,
				Size:   tt.wantSize,
			}, got)
		})
	}
}

func TestDownloadImageValidatesInputs(t *testing.T) {
	validAsset := downloadAsset(42)
	tests := []struct {
		name    string
		asset   incusos.ImageAsset
		wantErr string
	}{
		{
			name: "empty URL",
			asset: incusos.ImageAsset{
				SHA256: validAsset.SHA256,
			},
			wantErr: "incusos image URL is required",
		},
		{
			name: "empty SHA-256",
			asset: incusos.ImageAsset{
				URL: validAsset.URL,
			},
			wantErr: "incusos image SHA-256 is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheService := cachemocks.NewMockService(t)
			client := NewClient(WithCacheService(cacheService))

			got, err := client.DownloadImage(context.Background(), tt.asset)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Empty(t, got)
		})
	}
}

func TestDownloadImageRequiresCacheService(t *testing.T) {
	client := NewClient()

	got, err := client.DownloadImage(context.Background(), downloadAsset(42))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "incusos cache service is required")
	assert.Empty(t, got)
}

func TestDownloadImagePropagatesCacheErrors(t *testing.T) {
	ctx := context.Background()
	asset := downloadAsset(42)
	cacheErr := errors.New("cache failed")
	cacheService := cachemocks.NewMockService(t)
	cacheService.EXPECT().Fetch(ctx, cache.FetchRequest{
		URL:            asset.URL,
		ExpectedSHA256: asset.SHA256,
		ExpectedSize:   asset.Size,
	}).Return(cache.Blob{}, cacheErr).Once()
	client := NewClient(WithCacheService(cacheService))

	got, err := client.DownloadImage(ctx, asset)

	require.ErrorIs(t, err, cacheErr)
	assert.Contains(t, err.Error(), "download incusos image through cache")
	assert.Empty(t, got)
}

func downloadAsset(size int64) incusos.ImageAsset {
	return incusos.ImageAsset{
		Version:      incusos.Version("202604261712"),
		Architecture: core.Architecture("amd64"),
		Type:         incusos.ImageTypeRaw,
		URL:          "https://example.invalid/incusos.img.gz",
		SHA256:       strings.Repeat("a", 64),
		Size:         size,
	}
}
