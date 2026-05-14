package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	imgsrv "github.com/meigma/imgsrv/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/publish"
	publishmocks "github.com/meigma/imgcli/internal/publish/mocks"
	imgschemas "github.com/meigma/imgcli/schemas"
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
		name    string
		command string
	}{
		{
			name:    "plan",
			command: "plan",
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
	}
}

func TestPlanCommand(t *testing.T) {
	t.Run("prints resolved IncusOS artifact plan", func(t *testing.T) {
		clearIMGCLIEnv(t)
		outputDir := filepath.Join(t.TempDir(), "out")
		cacheDir := filepath.Join(t.TempDir(), "cache")
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: {
	name:        "test-image"
	description: "test image"
}
output: dir: "`+outputDir+`"
incusos: {
	defaults: source: channel: "testing"
	seed: install: {}
	variants: {
		secureboot: artifact: {
			architecture: "amd64"
			format:       "raw.gz"
			filename:     "custom/secureboot.img.gz"
		}
		default: artifact: {
			architecture: "amd64"
			format:       "raw.gz"
			labels: tier: "smoke"
			annotations: note: "planned"
		}
	}
}
`)

		result := executeCommand(t, Options{}, "--cache-dir", cacheDir, "plan", configPath)

		require.NoError(t, result.err)
		assert.Empty(t, result.stderr)
		assert.NoDirExists(t, cacheDir)
		var plan core.ResolvedPlan
		require.NoError(t, json.Unmarshal([]byte(result.stdout), &plan))
		assert.Equal(t, core.ResolvedPlan{
			Image: core.Image{
				Name:        core.Name("test-image"),
				Description: "test image",
			},
			OutputDir: outputDir,
			Artifacts: map[core.ArtifactKey]core.ResolvedArtifact{
				"default": {
					ArtifactKey:  core.ArtifactKey("default"),
					ImageName:    "test-image",
					Variant:      core.VariantName("default"),
					Provider:     core.ProviderName("incusos"),
					Os:           "incusos",
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					MediaType:    "application/gzip",
					Path:         filepath.Join(outputDir, "test-image-default-amd64.raw.gz"),
					Labels:       map[string]string{"tier": "smoke"},
					Annotations:  map[string]string{"note": "planned"},
				},
				"secureboot": {
					ArtifactKey:  core.ArtifactKey("secureboot"),
					ImageName:    "test-image",
					Variant:      core.VariantName("secureboot"),
					Provider:     core.ProviderName("incusos"),
					Os:           "incusos",
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					MediaType:    "application/gzip",
					Path:         filepath.Join(outputDir, "custom", "secureboot.img.gz"),
				},
			},
		}, plan)
	})

	t.Run("does not require publish configuration", func(t *testing.T) {
		clearIMGCLIEnv(t)
		t.Setenv("IMGCLI_PUBLISH_PART_SIZE", "1MB")
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: variants: default: artifact: {
	architecture: "amd64"
	format:       "raw.gz"
}
`)

		result := executeCommand(t, Options{}, "plan", configPath)

		require.NoError(t, result.err)
		assert.Empty(t, result.stderr)
		assert.NotEmpty(t, result.stdout)
	})

	t.Run("missing provider fails explicitly", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
`)

		result := executeCommand(t, Options{}, "plan", configPath)

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

		result := executeCommand(t, Options{}, "plan", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `unsupported provider "talos": only incusos is supported`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("provider planning errors fail before build adapters", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: variants: default: artifact: {
	architecture: "amd64"
	format:       "iso"
}
`)

		result := executeCommand(t, Options{}, "plan", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `unsupported incusos artifact format "iso"`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})
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

	t.Run("prints table and writes artifact sidecars by default", func(t *testing.T) {
		run := runSuccessfulBuildCommand(t)

		require.NoError(t, run.result.err)
		assert.Empty(t, run.result.stderr)
		lines := strings.Split(strings.TrimSuffix(run.result.stdout, "\n"), "\n")
		require.Len(t, lines, 3)
		assert.Equal(
			t,
			[]string{"VARIANT", "OS", "ARCH", "FORMAT", "SIZE_BYTES", "SHA256_PREFIX", "ARTIFACT", "METADATA"},
			strings.Fields(lines[0]),
		)
		assert.Equal(
			t,
			[]string{
				"default",
				"incusos",
				"amd64",
				"raw.gz",
				"8",
				run.sha256[:buildSHA256PrefixLength],
				run.defaultPath,
				run.defaultMetadataPath,
			},
			strings.Fields(lines[1]),
		)
		assert.Equal(
			t,
			[]string{
				"secureboot",
				"incusos",
				"amd64",
				"raw.gz",
				"8",
				run.sha256[:buildSHA256PrefixLength],
				run.secureBootPath,
				run.secureBootMetadataPath,
			},
			strings.Fields(lines[2]),
		)
		assertBuildMetadata(t, run.defaultMetadataPath, core.ResolvedArtifact{
			ArtifactKey:  core.ArtifactKey("default"),
			ImageName:    "test-image",
			Variant:      core.VariantName("default"),
			Provider:     core.ProviderName("incusos"),
			Os:           "incusos",
			Architecture: core.Architecture("amd64"),
			Format:       core.ArtifactFormat("raw.gz"),
			MediaType:    "application/gzip",
			Path:         run.defaultPath,
			Labels:       map[string]string{"tier": "smoke"},
			Annotations:  map[string]string{"note": "built"},
			Digest:       "sha256:" + run.sha256,
			Size:         int64(len(run.artifactBody)),
		})
		assertBuildMetadata(t, run.secureBootMetadataPath, core.ResolvedArtifact{
			ArtifactKey:  core.ArtifactKey("secureboot"),
			ImageName:    "test-image",
			Variant:      core.VariantName("secureboot"),
			Provider:     core.ProviderName("incusos"),
			Os:           "incusos",
			Architecture: core.Architecture("amd64"),
			Format:       core.ArtifactFormat("raw.gz"),
			MediaType:    "application/gzip",
			Path:         run.secureBootPath,
			Digest:       "sha256:" + run.sha256,
			Size:         int64(len(run.artifactBody)),
		})
		assertSuccessfulBuildAdapters(t, run)
	})

	t.Run("prints high-level JSON summary", func(t *testing.T) {
		run := runSuccessfulBuildCommand(t, "--format", "json")

		require.NoError(t, run.result.err)
		assert.Empty(t, run.result.stderr)
		var summary buildOutputSummary
		require.NoError(t, json.Unmarshal([]byte(run.result.stdout), &summary))
		assert.Equal(t, buildOutputSummary{
			Image: core.Image{Name: core.Name("test-image")},
			Artifacts: []buildArtifactOutputSummary{
				{
					ArtifactKey:  core.ArtifactKey("default"),
					Variant:      core.VariantName("default"),
					Provider:     core.ProviderName("incusos"),
					Os:           "incusos",
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					Path:         run.defaultPath,
					MetadataPath: run.defaultMetadataPath,
					Size:         int64(len(run.artifactBody)),
					SHA256:       run.sha256,
				},
				{
					ArtifactKey:  core.ArtifactKey("secureboot"),
					Variant:      core.VariantName("secureboot"),
					Provider:     core.ProviderName("incusos"),
					Os:           "incusos",
					Architecture: core.Architecture("amd64"),
					Format:       core.ArtifactFormat("raw.gz"),
					Path:         run.secureBootPath,
					MetadataPath: run.secureBootMetadataPath,
					Size:         int64(len(run.artifactBody)),
					SHA256:       run.sha256,
				},
			},
		}, summary)
		assert.FileExists(t, run.defaultMetadataPath)
		assert.FileExists(t, run.secureBootMetadataPath)
	})

	t.Run("paths format preserves path-only stdout", func(t *testing.T) {
		run := runSuccessfulBuildCommand(t, "--format", "paths")

		require.NoError(t, run.result.err)
		assert.Equal(t, run.defaultPath+"\n"+run.secureBootPath+"\n", run.result.stdout)
		assert.Empty(t, run.result.stderr)
		assert.FileExists(t, run.defaultMetadataPath)
		assert.FileExists(t, run.secureBootMetadataPath)
	})

	t.Run("invalid format fails before reading config or building", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(t, Options{}, "build", "--format", "yaml", "missing.cue")

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `invalid build output format "yaml"`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
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

	t.Run("requires imgsrv token", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(
			t,
			Options{},
			"publish",
			"image.cue",
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--release-version",
			"v1.0.0",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "publish requires imgsrv.token")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("requires release version", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(
			t,
			Options{},
			"publish",
			"image.cue",
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--imgsrv-token",
			"test-token",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "publish requires publish.version")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("rejects disabled upload readiness wait", func(t *testing.T) {
		clearIMGCLIEnv(t)

		result := executeCommand(
			t,
			Options{},
			"publish",
			"image.cue",
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--imgsrv-token",
			"test-token",
			"--release-version",
			"v1.0.0",
			"--publish-wait=false",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "publish requires CAS-ready uploads")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("builds and publishes IncusOS release", func(t *testing.T) {
		clearIMGCLIEnv(t)
		outputDir := filepath.Join(t.TempDir(), "out")
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
publish: imageName: "published-image"
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
		publishCatalog := publishmocks.NewMockCatalogClient(t)
		mediaTypeHint := "application/gzip"
		filenameHint := filepath.Base(wantOutputPath)
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
				Parts: []imgsrv.CompleteUploadPart{
					{Number: 1, ETag: "etag-1", SizeBytes: int64(len(artifactBody))},
				},
			}).
			Return(imgsrv.UploadSession{ID: "upload-1", State: imgsrv.UploadStateReady}, nil).
			Once()
		publishCatalog.EXPECT().
			CreateImage(mock.Anything, imgsrv.CreateImageRequest{Name: "published-image"}).
			Return(imgsrv.Image{Name: "published-image"}, nil).
			Once()
		publishCatalog.EXPECT().
			CreateDraftVersion(mock.Anything, "published-image", imgsrv.CreateDraftVersionRequest{Version: "v1.0.0"}).
			Return(imgsrv.ImageVersion{Version: "v1.0.0", State: imgsrv.ImageVersionStateDraft}, nil).
			Once()
		publishCatalog.EXPECT().
			AddArtifact(mock.Anything, "published-image", "v1.0.0", imgsrv.AddArtifactRequest{
				Variant:              "default",
				OperatingSystem:      "incusos",
				Architecture:         "x86_64",
				Format:               imgsrv.ArtifactFormatRawGZ,
				PrimaryBlobDigest:    "sha256:abc123",
				PrimaryBlobSizeBytes: int64(len(artifactBody)),
				PrimaryMediaType:     "application/gzip",
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
		publishCatalog.EXPECT().
			PublishVersion(mock.Anything, "published-image", "v1.0.0").
			Return(imgsrv.PublishJob{
				ID:        "publish-job-1",
				ImageName: "published-image",
				Version:   "v1.0.0",
				State:     imgsrv.PublishJobStateQueued,
			}, nil).
			Once()
		publishCatalog.EXPECT().
			GetPublishJob(mock.Anything, "publish-job-1").
			Return(imgsrv.PublishJob{
				ID:        "publish-job-1",
				ImageName: "published-image",
				Version:   "v1.0.0",
				State:     imgsrv.PublishJobStateSucceeded,
			}, nil).
			Once()
		publishCatalog.EXPECT().
			PutAlias(mock.Anything, "published-image", "latest", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
			Return(imgsrv.Alias{Alias: "latest", Version: "v1.0.0"}, nil).
			Once()
		publishCatalog.EXPECT().
			PutAlias(mock.Anything, "published-image", "prod", imgsrv.PutAliasRequest{Version: "v1.0.0"}).
			Return(imgsrv.Alias{Alias: "prod", Version: "v1.0.0"}, nil).
			Once()

		result := executeCommand(t, Options{
			IncusOSCatalog:       catalog,
			IncusOSDownloader:    downloader,
			IncusOSSeedBuilder:   seedBuilder,
			IncusOSImageInjector: injector,
			ImgsrvUploadsClient:  uploads,
			ImgsrvCatalogClient:  publishCatalog,
		},
			"publish",
			configPath,
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--imgsrv-token",
			"test-token",
			"--release-version",
			"v1.0.0",
			"--alias",
			"latest",
			"--alias",
			"prod",
		)

		require.NoError(t, result.err)
		assert.Empty(t, result.stderr)
		var manifest publish.ReleaseResult
		require.NoError(t, json.Unmarshal([]byte(result.stdout), &manifest))
		assert.Equal(t, publish.ReleaseResult{
			Image:   "published-image",
			Version: "v1.0.0",
			State:   imgsrv.ImageVersionStatePublished,
			Aliases: []string{"latest", "prod"},
			Artifacts: []publish.PublishedReleaseArtifact{
				{
					ArtifactKey:      "default",
					Variant:          "default",
					LocalPath:        wantOutputPath,
					ServerArtifactID: "artifact-1",
					OperatingSystem:  "incusos",
					Architecture:     "x86_64",
					Format:           imgsrv.ArtifactFormatRawGZ,
					Digest:           "sha256:abc123",
					Size:             int64(len(artifactBody)),
					MediaType:        "application/gzip",
				},
			},
		}, manifest)
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
			"publish",
			"image.cue",
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--imgsrv-token",
			"test-token",
			"--release-version",
			"v1.0.0",
			"--publish-part-size",
			"1MB",
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
			"publish",
			"image.cue",
			"--imgsrv-url",
			"https://imgsrv.example.invalid",
			"--imgsrv-token",
			"test-token",
			"--release-version",
			"v1.0.0",
			"--publish-timeout",
			"soon",
		)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `invalid publish.timeout "soon"`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})
}

type buildCommandRun struct {
	result                 commandResult
	artifactBody           []byte
	sha256                 string
	cacheDir               string
	defaultPath            string
	defaultMetadataPath    string
	secureBootPath         string
	secureBootMetadataPath string
	catalog                *testCatalog
	downloader             *testDownloader
	seedBuilder            *testSeedBuilder
	injector               *testImageInjector
}

func runSuccessfulBuildCommand(t *testing.T, buildArgs ...string) buildCommandRun {
	t.Helper()

	clearIMGCLIEnv(t)
	outputDir := filepath.Join(t.TempDir(), "out")
	configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
output: dir: "`+outputDir+`"
incusos: {
	defaults: source: {
		channel: "testing"
		version: "202604261712"
	}
	seed: install: {}
	variants: {
		secureboot: artifact: {
			architecture: "amd64"
			format:       "raw.gz"
		}
		default: artifact: {
			architecture: "amd64"
			format:       "raw.gz"
			labels: tier: "smoke"
			annotations: note: "built"
		}
	}
}
`)
	artifactBody := []byte("artifact")
	artifactSHA256 := strings.Repeat("abcdef0123456789", 4)
	defaultPath := filepath.Join(outputDir, "test-image-default-amd64.raw.gz")
	secureBootPath := filepath.Join(outputDir, "test-image-secureboot-amd64.raw.gz")
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
		sha256: artifactSHA256,
	}
	cacheDir := filepath.Join(t.TempDir(), "cache")

	args := []string{"--cache-dir", cacheDir, "build"}
	args = append(args, buildArgs...)
	args = append(args, configPath)

	result := executeCommand(t, Options{
		IncusOSCatalog:       catalog,
		IncusOSDownloader:    downloader,
		IncusOSSeedBuilder:   seedBuilder,
		IncusOSImageInjector: injector,
	}, args...)

	return buildCommandRun{
		result:                 result,
		artifactBody:           artifactBody,
		sha256:                 artifactSHA256,
		cacheDir:               cacheDir,
		defaultPath:            defaultPath,
		defaultMetadataPath:    buildArtifactMetadataPath(defaultPath),
		secureBootPath:         secureBootPath,
		secureBootMetadataPath: buildArtifactMetadataPath(secureBootPath),
		catalog:                catalog,
		downloader:             downloader,
		seedBuilder:            seedBuilder,
		injector:               injector,
	}
}

func assertBuildMetadata(t *testing.T, path string, want core.ResolvedArtifact) {
	t.Helper()

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var metadata imgschemas.ArtifactMetadata
	require.NoError(t, json.Unmarshal(data, &metadata))
	assert.Equal(t, artifactMetadataAPIVersion, metadata.ApiVersion)
	assert.Equal(t, artifactMetadataKind, metadata.Kind)
	assert.Equal(t, want, metadata.Artifact)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(artifactMetadataFileMode), info.Mode().Perm())
}

func assertSuccessfulBuildAdapters(t *testing.T, run buildCommandRun) {
	t.Helper()

	assert.NoDirExists(t, run.cacheDir)
	require.Len(t, run.catalog.queries, 2)
	assert.Equal(t, []incusos.ImageQuery{
		{
			Channel:      incusos.ChannelTesting,
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
		},
		{
			Channel:      incusos.ChannelTesting,
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
		},
	}, run.catalog.queries)
	assert.Equal(t, []incusos.ImageAsset{run.catalog.asset, run.catalog.asset}, run.downloader.assets)
	assert.Len(t, run.seedBuilder.configs, 1)
	require.Len(t, run.injector.calls, 2)
	assert.Equal(t, run.defaultPath, run.injector.calls[0].outputPath)
	assert.Equal(t, run.secureBootPath, run.injector.calls[1].outputPath)
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
		"IMGCLI_PUBLISH_VERSION",
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
