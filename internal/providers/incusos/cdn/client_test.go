package cdn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/schemas/core"
)

func TestResolveImage(t *testing.T) {
	tests := []struct {
		name   string
		query  incusos.ImageQuery
		assert func(t *testing.T, baseURL string, asset incusos.ImageAsset, err error)
	}{
		{
			name: "selects latest stable raw image by default",
			query: incusos.ImageQuery{
				Architecture: core.Architecture("amd64"),
				Type:         incusos.ImageTypeRaw,
			},
			assert: func(t *testing.T, baseURL string, asset incusos.ImageAsset, err error) {
				require.NoError(t, err)
				assert.Equal(t, incusos.Version("202604261712"), asset.Version)
				assert.Equal(t, core.Architecture("amd64"), asset.Architecture)
				assert.Equal(t, incusos.ImageTypeRaw, asset.Type)
				assert.Equal(t, baseURL+"/202604261712/x86_64/IncusOS_202604261712.img.gz", asset.URL)
				assert.Equal(t, "stable-raw-sha", asset.SHA256)
				assert.Equal(t, int64(606774869), asset.Size)
			},
		},
		{
			name: "selects exact testing iso image",
			query: incusos.ImageQuery{
				Channel:      incusos.ChannelTesting,
				Version:      incusos.Version("202604282312"),
				Architecture: core.Architecture("arm64"),
				Type:         incusos.ImageTypeISO,
			},
			assert: func(t *testing.T, baseURL string, asset incusos.ImageAsset, err error) {
				require.NoError(t, err)
				assert.Equal(t, incusos.Version("202604282312"), asset.Version)
				assert.Equal(t, core.Architecture("arm64"), asset.Architecture)
				assert.Equal(t, incusos.ImageTypeISO, asset.Type)
				assert.Equal(t, baseURL+"/202604282312/aarch64/IncusOS_202604282312.iso.gz", asset.URL)
				assert.Equal(t, "testing-iso-sha", asset.SHA256)
				assert.Equal(t, int64(426796700), asset.Size)
			},
		},
		{
			name: "requires version to belong to selected channel",
			query: incusos.ImageQuery{
				Channel:      incusos.ChannelStable,
				Version:      incusos.Version("202604282312"),
				Architecture: core.Architecture("amd64"),
				Type:         incusos.ImageTypeRaw,
			},
			assert: func(t *testing.T, _ string, _ incusos.ImageAsset, err error) {
				require.ErrorIs(t, err, incusos.ErrImageNotFound)
			},
		},
		{
			name: "reports missing architecture asset",
			query: incusos.ImageQuery{
				Channel:      incusos.ChannelStable,
				Version:      incusos.Version("202604261712"),
				Architecture: core.Architecture("arm64"),
				Type:         incusos.ImageTypeRaw,
			},
			assert: func(t *testing.T, _ string, _ incusos.ImageAsset, err error) {
				require.ErrorIs(t, err, incusos.ErrImageNotFound)
			},
		},
		{
			name: "rejects unsupported architecture",
			query: incusos.ImageQuery{
				Channel:      incusos.ChannelStable,
				Architecture: core.Architecture("riscv64"),
				Type:         incusos.ImageTypeRaw,
			},
			assert: func(t *testing.T, _ string, _ incusos.ImageAsset, err error) {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "unsupported incusos architecture")
			},
		},
	}

	server := newCatalogServer(t, http.StatusOK, catalogFixture)
	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asset, err := client.ResolveImage(context.Background(), tt.query)
			tt.assert(t, server.URL, asset, err)
		})
	}
}

func TestResolveImageHandlesCatalogFetchFailure(t *testing.T) {
	server := newCatalogServer(t, http.StatusInternalServerError, "failed")
	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))

	asset, err := client.ResolveImage(context.Background(), incusos.ImageQuery{
		Architecture: core.Architecture("amd64"),
		Type:         incusos.ImageTypeRaw,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected HTTP status")
	assert.Empty(t, asset)
}

func TestResolveImageHonorsContext(t *testing.T) {
	client := NewClient(WithHTTPClient(&http.Client{
		Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, context.Canceled
		}),
	}))

	asset, err := client.ResolveImage(context.Background(), incusos.ImageQuery{
		Architecture: core.Architecture("amd64"),
		Type:         incusos.ImageTypeRaw,
	})

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, asset)
}

func newCatalogServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/index.json", r.URL.Path)
		w.WriteHeader(status)
		_, err := w.Write([]byte(body))
		assert.NoError(t, err)
	}))

	t.Cleanup(server.Close)

	return server
}

type roundTripFunc func(req *http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

const catalogFixture = `{
  "format": "1.0",
  "updates": [
    {
      "format": "1.0",
      "channels": ["testing"],
      "version": "202604282312",
      "url": "/202604282312",
      "files": [
        {
          "architecture": "aarch64",
          "component": "os",
          "filename": "aarch64/IncusOS_202604282312.iso.gz",
          "sha256": "testing-iso-sha",
          "size": 426796700,
          "type": "image-iso"
        },
        {
          "architecture": "x86_64",
          "component": "os",
          "filename": "x86_64/IncusOS_202604282312.img.gz",
          "sha256": "testing-raw-sha",
          "size": 606774869,
          "type": "image-raw"
        }
      ]
    },
    {
      "format": "1.0",
      "channels": ["testing", "stable"],
      "version": "202604261712",
      "url": "/202604261712",
      "files": [
        {
          "architecture": "x86_64",
          "component": "os",
          "filename": "x86_64/IncusOS_202604261712.img.gz",
          "sha256": "stable-raw-sha",
          "size": 606774869,
          "type": "image-raw"
        },
        {
          "architecture": "x86_64",
          "component": "os",
          "filename": "x86_64/IncusOS_202604261712.iso.gz",
          "sha256": "stable-iso-sha",
          "size": 607740891,
          "type": "image-iso"
        },
        {
          "architecture": "x86_64",
          "component": "debug",
          "filename": "x86_64/debug.raw.gz",
          "sha256": "debug-sha",
          "size": 5014164,
          "type": "application"
        }
      ]
    },
    {
      "format": "1.0",
      "channels": ["testing", "stable"],
      "version": "202604202240",
      "url": "/202604202240",
      "files": [
        {
          "architecture": "x86_64",
          "component": "os",
          "filename": "x86_64/IncusOS_202604202240.img.gz",
          "sha256": "old-stable-raw-sha",
          "size": 606774869,
          "type": "image-raw"
        }
      ]
    }
  ]
}`
