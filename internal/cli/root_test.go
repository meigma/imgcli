package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	imgsrv "github.com/meigma/imgsrv/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers/incusos"
	publishmocks "github.com/meigma/imgcli/internal/publish/mocks"
	"github.com/meigma/imgcli/schemas/core"
)

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func TestVersionOutput(t *testing.T) {
	tests := []struct {
		name    string
		version string
		args    []string
		want    string
	}{
		{
			name: "version command uses dev by default",
			args: []string{"version"},
			want: "dev\n",
		},
		{
			name: "root version flag uses dev by default",
			args: []string{"--version"},
			want: "dev\n",
		},
		{
			name:    "version command prints injected version",
			version: "1.2.3",
			args:    []string{"version"},
			want:    "1.2.3\n",
		},
		{
			name:    "root version flag prints injected version",
			version: "1.2.3",
			args:    []string{"--version"},
			want:    "1.2.3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{Version: tt.version}, tt.args...)

			require.NoError(t, result.err)
			assert.Equal(t, tt.want, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestInvalidLogSettings(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid log level",
			args:    []string{"--log-level", "verbose", "version"},
			wantErr: `invalid log level "verbose"`,
		},
		{
			name:    "invalid log format",
			args:    []string{"--log-format", "yaml", "version"},
			wantErr: `invalid log format "yaml"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.args...)

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, tt.wantErr)
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestBaseCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		placeholder bool
	}{
		{
			name:        "plan",
			command:     "plan",
			placeholder: true,
		},
		{
			name:    "build",
			command: "build",
		},
		{
			name:    "publish",
			command: "publish",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" appears in root help", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, "--help")

			require.NoError(t, result.err)
			assert.Contains(t, result.stdout, tt.command)
			assert.Empty(t, result.stderr)
		})

		t.Run(tt.name+" requires config operand", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.command)

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, `accepts 1 arg(s), received 0`)
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})

		if !tt.placeholder {
			continue
		}

		t.Run(tt.name+" returns placeholder error", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.command, "image.cue")

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, tt.command+" command is not implemented yet")
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestBuildCommand(t *testing.T) {
	t.Run("missing incusos provider fails explicitly", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
`)

		result := executeCommand(t, Options{}, "build", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "must specify provider incusos")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("unsupported provider fails explicitly", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
talos: {}
`)

		result := executeCommand(t, Options{}, "build", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `unsupported provider "talos": only incusos is supported`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("prints customized IncusOS artifact path", func(t *testing.T) {
		clearIMGCLIEnv(t)
		outputDir := filepath.Join(t.TempDir(), "out")
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
output: dir: "`+outputDir+`"
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
`)
		catalog := &testCatalog{
			asset: incusos.ImageAsset{
				URL:    "https://example.invalid/os/202604261712/x86_64/IncusOS_202604261712.img.gz",
				SHA256: "source-sha",
				Size:   42,
			},
		}
		downloader := &testDownloader{
			image: incusos.DownloadedImage{
				Path:   "/cache/source.img.gz",
				SHA256: "source-sha",
				Size:   42,
			},
		}
		seedBuilder := &testSeedBuilder{
			seed: incusos.SeedArchive{Data: []byte("seed")},
		}
		injector := &testImageInjector{}
		cacheDir := filepath.Join(t.TempDir(), "cache")

		result := executeCommand(t, Options{
			IncusOSCatalog:       catalog,
			IncusOSDownloader:    downloader,
			IncusOSSeedBuilder:   seedBuilder,
			IncusOSImageInjector: injector,
		}, "--cache-dir", cacheDir, "build", configPath)

		require.NoError(t, result.err)
		wantOutputPath := filepath.Join(outputDir, "test-image-default-amd64.raw.gz")
		assert.Equal(t, wantOutputPath+"\n", result.stdout)
		assert.Empty(t, result.stderr)
		assert.NoDirExists(t, cacheDir)
		require.Len(t, catalog.queries, 1)
		assert.Equal(t, incusos.ImageQuery{
			Channel:      incusos.ChannelTesting,
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
		}, catalog.queries[0])
		assert.Equal(t, []incusos.ImageAsset{catalog.asset}, downloader.assets)
		assert.Len(t, seedBuilder.configs, 1)
		require.Len(t, injector.calls, 1)
		assert.Equal(t, wantOutputPath, injector.calls[0].outputPath)
	})
}

func TestPublishCommand(t *testing.T) {
	t.Run("requires imgsrv url", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(t, Options{}, "publish", "image.cue")

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "publish requires imgsrv.url")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("builds and uploads IncusOS artifact", func(t *testing.T) {
		clearIMGCLIEnv(t)
		outputDir := filepath.Join(t.TempDir(), "out")
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
output: dir: "`+outputDir+`"
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
`)
		artifactBody := []byte("artifact")
		wantOutputPath := filepath.Join(outputDir, "test-image-default-amd64.raw.gz")
		catalog := &testCatalog{
			asset: incusos.ImageAsset{
				URL:    "https://example.invalid/os/202604261712/x86_64/IncusOS_202604261712.img.gz",
				SHA256: "source-sha",
				Size:   42,
			},
		}
		downloader := &testDownloader{
			image: incusos.DownloadedImage{
				Path:   "/cache/source.img.gz",
				SHA256: "source-sha",
				Size:   42,
			},
		}
		seedBuilder := &testSeedBuilder{
			seed: incusos.SeedArchive{Data: []byte("seed")},
		}
		injector := &testImageInjector{
			body:   artifactBody,
			sha256: "abc123",
		}
		uploads := publishmocks.NewMockUploadsClient(t)
		filenameHint := filepath.Base(wantOutputPath)
		uploads.EXPECT().
			BeginUpload(mock.Anything, imgsrv.BeginUploadRequest{
				ExpectedDigest:    "sha256:abc123",
				ExpectedSizeBytes: int64(len(artifactBody)),
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
				Parts: []imgsrv.CompleteUploadPart{
					{Number: 1, ETag: "etag-1", SizeBytes: int64(len(artifactBody))},
				},
			}).
			Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateReady}, nil).
			Once()

		result := executeCommand(t, Options{
			IncusOSCatalog:       catalog,
			IncusOSDownloader:    downloader,
			IncusOSSeedBuilder:   seedBuilder,
			IncusOSImageInjector: injector,
			ImgsrvUploadsClient:  uploads,
		}, "--imgsrv-url", "https://imgsrv.example.invalid", "publish", configPath)

		require.NoError(t, result.err)
		assert.Equal(t, "sha256:abc123\n", result.stdout)
		assert.Empty(t, result.stderr)
		require.Len(t, catalog.queries, 1)
		assert.Equal(t, incusos.ImageQuery{
			Channel:      incusos.ChannelTesting,
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
		}, catalog.queries[0])
		assert.Equal(t, []incusos.ImageAsset{catalog.asset}, downloader.assets)
		assert.Len(t, seedBuilder.configs, 1)
		require.Len(t, injector.calls, 1)
		assert.Equal(t, wantOutputPath, injector.calls[0].outputPath)
	})
}

func TestPublishConfigIsPublishOnly(t *testing.T) {
	t.Run("invalid part size does not fail other commands", func(t *testing.T) {
		clearIMGCLIEnv(t)
		t.Setenv("IMGCLI_PUBLISH_PART_SIZE", "1MB")

		result := executeCommand(t, Options{}, "version")

		require.NoError(t, result.err)
		assert.Equal(t, "dev\n", result.stdout)
	})

	t.Run("invalid part size fails publish", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(
			t,
			Options{},
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--publish-part-size",
			"1MB",
			"publish",
			"image.cue",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "publish.part-size")
		require.ErrorContains(t, result.err, "at least 5MB")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("invalid duration does not fail other commands", func(t *testing.T) {
		clearIMGCLIEnv(t)
		t.Setenv("IMGCLI_PUBLISH_TIMEOUT", "soon")

		result := executeCommand(t, Options{}, "version")

		require.NoError(t, result.err)
		assert.Equal(t, "dev\n", result.stdout)
	})

	t.Run("invalid duration fails publish", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(
			t,
			Options{},
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--publish-timeout",
			"soon",
			"publish",
			"image.cue",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `invalid publish.timeout "soon"`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})
}

func executeCommand(t *testing.T, opts Options, args ...string) commandResult {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	opts.Stdin = strings.NewReader("")
	opts.Stdout = &stdout
	opts.Stderr = &stderr
	opts.Environ = []string{"TERM=dumb"}

	cmd, err := NewRootCommand(opts)
	require.NoError(t, err)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())

	return commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func clearIMGCLIEnv(t *testing.T) {
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
		"IMGCLI_PUBLISH_WAIT",
	} {
		t.Setenv(key, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func writeImageConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "image.cue")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

type testCatalog struct {
	asset   incusos.ImageAsset
	queries []incusos.ImageQuery
}

func (c *testCatalog) ResolveImage(_ context.Context, query incusos.ImageQuery) (incusos.ImageAsset, error) {
	c.queries = append(c.queries, query)
	return c.asset, nil
}

type testDownloader struct {
	image  incusos.DownloadedImage
	assets []incusos.ImageAsset
}

func (d *testDownloader) DownloadImage(_ context.Context, asset incusos.ImageAsset) (incusos.DownloadedImage, error) {
	d.assets = append(d.assets, asset)
	image := d.image
	image.Asset = asset
	return image, nil
}

type testSeedBuilder struct {
	seed    incusos.SeedArchive
	configs []incusos.Config
}

func (b *testSeedBuilder) BuildSeed(_ context.Context, config incusos.Config) (incusos.SeedArchive, error) {
	b.configs = append(b.configs, config)
	return b.seed, nil
}

type testImageInjector struct {
	body   []byte
	sha256 string
	calls  []testInjectCall
}

type testInjectCall struct {
	image      incusos.DownloadedImage
	seed       incusos.SeedArchive
	outputPath string
}

func (i *testImageInjector) InjectSeed(
	_ context.Context,
	image incusos.DownloadedImage,
	seed incusos.SeedArchive,
	outputPath string,
) (incusos.CustomizedImage, error) {
	i.calls = append(i.calls, testInjectCall{image: image, seed: seed, outputPath: outputPath})
	if i.body != nil {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
			return incusos.CustomizedImage{}, err
		}
		if err := os.WriteFile(outputPath, i.body, 0o600); err != nil {
			return incusos.CustomizedImage{}, err
		}
		sha256 := i.sha256
		if sha256 == "" {
			sha256 = "custom-sha"
		}
		return incusos.CustomizedImage{
			Source: image,
			Path:   outputPath,
			Size:   int64(len(i.body)),
			SHA256: sha256,
		}, nil
	}
	return incusos.CustomizedImage{
		Source: image,
		Path:   outputPath,
		Size:   99,
		SHA256: "custom-sha",
	}, nil
}
