//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	imgsrv "github.com/meigma/imgsrv/client"
	imgsrvtest "github.com/meigma/imgsrv/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/cli"
	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/publish"
	"github.com/meigma/imgcli/schemas/core"
)

const integrationAPIToken = "testtok.imgcli-publish"

func TestPublishIncusOSReleaseToImgsrv(t *testing.T) {
	clearIntegrationEnv(t)
	ctx := context.Background()
	env := imgsrvtest.Start(t, imgsrvtest.WithCASPromotion(), imgsrvtest.WithAPIToken(integrationAPIToken))
	imageName := "incusos-test"
	version := "2026.05.06"
	outputDir := filepath.Join(t.TempDir(), "out")
	configPath := writeIntegrationConfig(t, imageName, outputDir)
	artifactBody := []byte("published IncusOS artifact bytes")
	catalog := &integrationCatalog{
		asset: incusos.ImageAsset{
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
			URL:          "https://example.invalid/os/202604261712/x86_64/IncusOS_202604261712.img.gz",
			SHA256:       "source-sha",
			Size:         42,
		},
	}
	downloader := &integrationDownloader{
		image: incusos.DownloadedImage{
			Path:   "/cache/source.img.gz",
			SHA256: "source-sha",
			Size:   42,
		},
	}
	seedBuilder := &integrationSeedBuilder{seed: incusos.SeedArchive{Data: []byte("seed")}}
	injector := &integrationInjector{body: artifactBody}
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	cmd, err := cli.NewRootCommand(cli.Options{
		Stdout:               &stdout,
		Stderr:               &stderr,
		Stdin:                bytes.NewReader(nil),
		Environ:              []string{"TERM=dumb"},
		IncusOSCatalog:       catalog,
		IncusOSDownloader:    downloader,
		IncusOSSeedBuilder:   seedBuilder,
		IncusOSImageInjector: injector,
	})
	require.NoError(t, err)
	cmd.SetArgs([]string{
		"publish",
		configPath,
		"--imgsrv-url",
		env.BaseURL(),
		"--imgsrv-token",
		integrationAPIToken,
		"--release-version",
		version,
		"--alias",
		"latest",
		"--publish-timeout",
		"10s",
		"--publish-poll-interval",
		"10ms",
	})

	require.NoError(t, cmd.ExecuteContext(ctx))
	assert.Empty(t, stderr.String())
	var published publish.ReleaseResult
	require.NoError(t, json.Unmarshal(stdout.Bytes(), &published))
	assert.Equal(t, imageName, published.Image)
	assert.Equal(t, version, published.Version)
	assert.Equal(t, imgsrv.ImageVersionStatePublished, published.State)
	assert.Equal(t, []string{"latest"}, published.Aliases)
	require.Len(t, published.Artifacts, 1)
	assert.Equal(t, "incusos", published.Artifacts[0].OperatingSystem)
	assert.Equal(t, "x86_64", published.Artifacts[0].Architecture)
	assert.Equal(t, imgsrv.ArtifactFormatRawGZ, published.Artifacts[0].Format)

	imgsrvClient := env.Client(t)
	manifest, err := imgsrvClient.Catalog().ResolveManifest(ctx, imageName, "latest")
	require.NoError(t, err)
	require.Len(t, manifest.Artifacts, 1)
	artifact := manifest.Artifacts[0].Artifact
	assert.Equal(t, imgsrv.ArtifactFormatRawGZ, artifact.Format)
	assert.Equal(t, "x86_64", artifact.Architecture)
	assert.Equal(t, "incusos", artifact.OperatingSystem)

	download, err := imgsrvClient.Catalog().OpenArtifactDownload(
		ctx,
		imageName,
		version,
		artifact.ID.String(),
		imgsrv.OpenBlobOptions{},
	)
	require.NoError(t, err)
	defer download.Body.Close()
	got, err := io.ReadAll(download.Body)
	require.NoError(t, err)
	assert.Equal(t, artifactBody, got)
}

func clearIntegrationEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"IMGCLI_CACHE_DIR",
		"IMGCLI_CACHE_MAX_SIZE",
		"IMGCLI_CONFIG",
		"IMGCLI_IMGSRV_TOKEN",
		"IMGCLI_IMGSRV_URL",
		"IMGCLI_LOG_LEVEL",
		"IMGCLI_LOG_FORMAT",
		"IMGCLI_NO_COLOR",
		"IMGCLI_PUBLISH_PART_SIZE",
		"IMGCLI_PUBLISH_POLL_INTERVAL",
		"IMGCLI_PUBLISH_TIMEOUT",
		"IMGCLI_PUBLISH_VERSION",
		"IMGCLI_PUBLISH_WAIT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func writeIntegrationConfig(t *testing.T, imageName string, outputDir string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "image.cue")
	content := `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "` + imageName + `"
output: dir: "` + outputDir + `"
incusos: {
	defaults: source: channel: "testing"
	seed: install: {}
	variants: default: {
		source: version: "202604261712"
		artifact: {
			architecture: "amd64"
			format:       "raw.gz"
		}
	}
}
`
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

type integrationCatalog struct {
	asset incusos.ImageAsset
}

func (c *integrationCatalog) ResolveImage(_ context.Context, query incusos.ImageQuery) (incusos.ImageAsset, error) {
	c.asset.Architecture = query.Architecture
	c.asset.Type = query.Type
	return c.asset, nil
}

type integrationDownloader struct {
	image incusos.DownloadedImage
}

func (d *integrationDownloader) DownloadImage(
	_ context.Context,
	asset incusos.ImageAsset,
) (incusos.DownloadedImage, error) {
	image := d.image
	image.Asset = asset
	return image, nil
}

type integrationSeedBuilder struct {
	seed incusos.SeedArchive
}

func (b *integrationSeedBuilder) BuildSeed(_ context.Context, _ incusos.Config) (incusos.SeedArchive, error) {
	return b.seed, nil
}

type integrationInjector struct {
	body []byte
}

func (i *integrationInjector) InjectSeed(
	_ context.Context,
	image incusos.DownloadedImage,
	_ incusos.SeedArchive,
	outputPath string,
) (incusos.CustomizedImage, error) {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return incusos.CustomizedImage{}, err
	}
	if err := os.WriteFile(outputPath, i.body, 0o600); err != nil {
		return incusos.CustomizedImage{}, err
	}

	sum := sha256.Sum256(i.body)
	return incusos.CustomizedImage{
		Source: image,
		Path:   outputPath,
		Size:   int64(len(i.body)),
		SHA256: hex.EncodeToString(sum[:]),
	}, nil
}
