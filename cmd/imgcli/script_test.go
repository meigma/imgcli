//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	imgsrv "github.com/meigma/imgsrv/client"
	imgsrvtest "github.com/meigma/imgsrv/test"
	"github.com/rogpeppe/go-internal/testscript"

	"github.com/meigma/imgcli/internal/cli"
	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/publish"
)

const (
	testEnvIncusOSCDNURL   = "IMGCLI_TEST_INCUSOS_CDN_URL"
	testEnvFixtureInjector = "IMGCLI_TEST_FIXTURE_INJECTOR"

	testVersion = "2026.05.06"

	fixtureSourcePath = "/202604261712/x86_64/IncusOS_202604261712.img.gz"
	fixtureSourceBody = "fixture IncusOS source image bytes\n"
	fixtureArtifact   = "published IncusOS artifact bytes\n"
)

type testscriptEnvKey struct{}

func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"imgcli": runTestscriptImgcli,
	})
}

func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir:                 "testdata/script",
		Setup:               setupScript,
		Cmds:                map[string]func(*testscript.TestScript, bool, []string){"verify-publish": verifyPublish},
		RequireExplicitExec: true,
		RequireUniqueNames:  true,
	})
}

func runTestscriptImgcli() {
	opts := cli.Options{
		Version:           version,
		IncusOSCDNBaseURL: os.Getenv(testEnvIncusOSCDNURL),
	}
	if os.Getenv(testEnvFixtureInjector) != "" {
		opts.IncusOSImageInjector = fixtureImageInjector{}
	}

	if err := cli.ExecuteContext(context.Background(), opts); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(exitFailure)
	}
}

func setupScript(env *testscript.Env) error {
	tb, ok := env.T().(testing.TB)
	if !ok {
		return errors.New("testscript environment does not expose testing.TB")
	}
	tb.Helper()

	imgsrvEnv := imgsrvtest.Start(tb, imgsrvtest.WithCASPromotion(), imgsrvtest.WithAPIToken())
	cdnServer := newFixtureCDNServer(tb)

	env.Values[testscriptEnvKey{}] = imgsrvEnv
	env.Setenv("TERM", "dumb")
	env.Setenv("XDG_CONFIG_HOME", filepath.Join(env.WorkDir, "xdg-config"))
	env.Setenv("IMGCLI_CACHE_DIR", filepath.Join(env.WorkDir, "cache"))
	env.Setenv("IMGCLI_CACHE_MAX_SIZE", "0")
	env.Setenv("IMGSRV_URL", imgsrvEnv.BaseURL())
	env.Setenv("IMGSRV_TOKEN", imgsrvEnv.ClientOptions().BearerToken)
	env.Setenv("PUBLISH_VERSION", testVersion)
	env.Setenv(testEnvIncusOSCDNURL, cdnServer.URL)
	env.Setenv(testEnvFixtureInjector, "1")

	return nil
}

func newFixtureCDNServer(tb testing.TB) *httptest.Server {
	tb.Helper()

	source := []byte(fixtureSourceBody)
	sourceDigest := sha256Hex(source)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.json":
			writeFixtureIndex(tb, w, sourceDigest, int64(len(source)))
		case fixtureSourcePath:
			_, err := w.Write(source)
			if err != nil {
				tb.Fatalf("write fixture source: %v", err)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	tb.Cleanup(server.Close)

	return server
}

func writeFixtureIndex(tb testing.TB, w http.ResponseWriter, sourceDigest string, sourceSize int64) {
	tb.Helper()

	index := map[string]any{
		"updates": []map[string]any{
			{
				"channels": []string{"testing", "stable"},
				"version":  "202604261712",
				"url":      "/202604261712",
				"files": []map[string]any{
					{
						"architecture": "x86_64",
						"component":    "os",
						"filename":     "x86_64/IncusOS_202604261712.img.gz",
						"sha256":       sourceDigest,
						"size":         sourceSize,
						"type":         "image-raw",
					},
				},
			},
		},
	}
	if err := json.NewEncoder(w).Encode(index); err != nil {
		tb.Fatalf("write fixture index: %v", err)
	}
}

func verifyPublish(ts *testscript.TestScript, neg bool, args []string) {
	if neg {
		ts.Fatalf("verify-publish does not support negation")
	}
	if len(args) != 4 {
		ts.Fatalf("usage: verify-publish RESULT_JSON IMAGE VERSION ALIAS")
	}

	result := readPublishResult(ts, args[0])
	assertPublishResult(ts, result, args[1], args[2], args[3])
	verifyPublishedArtifacts(ts, args[1], args[2], args[3], result.Artifacts)
}

func readPublishResult(ts *testscript.TestScript, path string) publish.ReleaseResult {
	var result publish.ReleaseResult
	if err := json.Unmarshal([]byte(ts.ReadFile(path)), &result); err != nil {
		ts.Fatalf("decode publish result: %v", err)
	}

	return result
}

func assertPublishResult(
	ts *testscript.TestScript,
	result publish.ReleaseResult,
	image string,
	publishedVersion string,
	alias string,
) {
	if result.Image != image {
		ts.Fatalf("publish result image = %q, want %q", result.Image, image)
	}
	if result.Version != publishedVersion {
		ts.Fatalf("publish result version = %q, want %q", result.Version, publishedVersion)
	}
	if result.State != imgsrv.ImageVersionStatePublished {
		ts.Fatalf("publish result state = %q, want %q", result.State, imgsrv.ImageVersionStatePublished)
	}
	if !containsString(result.Aliases, alias) {
		ts.Fatalf("publish result aliases = %v, want %q", result.Aliases, alias)
	}
	if len(result.Artifacts) != 2 {
		ts.Fatalf("publish result artifacts length = %d, want 2", len(result.Artifacts))
	}

	for _, wantVariant := range []string{"default", "secureboot"} {
		artifact, ok := publishedArtifactByVariant(result.Artifacts, wantVariant)
		if !ok {
			ts.Fatalf("publish result artifacts missing variant %q: %+v", wantVariant, result.Artifacts)
		}
		if artifact.OperatingSystem != "incusos" || artifact.Architecture != "x86_64" ||
			artifact.Format != imgsrv.ArtifactFormatRawGZ {
			ts.Fatalf("publish result artifact = %+v, want incusos x86_64 raw.gz", artifact)
		}
	}
}

func publishedArtifactByVariant(
	artifacts []publish.PublishedReleaseArtifact,
	variant string,
) (publish.PublishedReleaseArtifact, bool) {
	for _, artifact := range artifacts {
		if artifact.Variant == variant {
			return artifact, true
		}
	}

	return publish.PublishedReleaseArtifact{}, false
}

func verifyPublishedArtifacts(
	ts *testscript.TestScript,
	image string,
	publishedVersion string,
	alias string,
	publishedArtifacts []publish.PublishedReleaseArtifact,
) {
	env, ok := ts.Value(testscriptEnvKey{}).(*imgsrvtest.Env)
	if !ok || env == nil {
		ts.Fatalf("imgsrv test environment is unavailable")
	}

	client, err := imgsrv.New(env.ClientOptions())
	if err != nil {
		ts.Fatalf("create imgsrv client: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	manifest, err := client.Catalog().ResolveManifest(ctx, image, alias)
	if err != nil {
		ts.Fatalf("resolve manifest: %v", err)
	}
	if len(manifest.Artifacts) != len(publishedArtifacts) {
		ts.Fatalf("manifest artifacts length = %d, want %d", len(manifest.Artifacts), len(publishedArtifacts))
	}

	publishedByVariant := map[string]publish.PublishedReleaseArtifact{}
	for _, artifact := range publishedArtifacts {
		publishedByVariant[artifact.Variant] = artifact
	}

	for _, manifestArtifact := range manifest.Artifacts {
		artifact := manifestArtifact.Artifact
		published, ok := publishedByVariant[artifact.Variant]
		if !ok {
			ts.Fatalf("manifest artifact variant %q was not in publish result", artifact.Variant)
		}
		if artifact.ID.String() != published.ServerArtifactID {
			ts.Fatalf("manifest artifact ID = %q, want %q", artifact.ID.String(), published.ServerArtifactID)
		}
		verifyArtifactDownload(ts, client, image, publishedVersion, published.ServerArtifactID)
	}
}

func verifyArtifactDownload(
	ts *testscript.TestScript,
	client *imgsrv.Client,
	image string,
	publishedVersion string,
	artifactID string,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	download, err := client.Catalog().OpenArtifactDownload(
		ctx,
		image,
		publishedVersion,
		artifactID,
		imgsrv.OpenBlobOptions{},
	)
	if err != nil {
		ts.Fatalf("open artifact download: %v", err)
	}
	defer download.Body.Close()

	got, err := io.ReadAll(download.Body)
	if err != nil {
		ts.Fatalf("read artifact download: %v", err)
	}
	if !bytes.Equal(got, []byte(fixtureArtifact)) {
		ts.Fatalf("artifact download = %q, want %q", string(got), fixtureArtifact)
	}
}

type fixtureImageInjector struct{}

func (fixtureImageInjector) InjectSeed(
	_ context.Context,
	image incusos.DownloadedImage,
	seed incusos.SeedArchive,
	outputPath string,
) (incusos.CustomizedImage, error) {
	source, err := os.ReadFile(image.Path)
	if err != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("read fixture source image: %w", err)
	}
	if !bytes.Equal(source, []byte(fixtureSourceBody)) {
		return incusos.CustomizedImage{}, errors.New("fixture source image did not pass through cache")
	}
	if len(seed.Data) == 0 {
		return incusos.CustomizedImage{}, errors.New("fixture seed archive is empty")
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("create fixture output directory: %w", err)
	}

	body := []byte(fixtureArtifact)
	if err := os.WriteFile(outputPath, body, 0o600); err != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("write fixture artifact: %w", err)
	}

	return incusos.CustomizedImage{
		Source: image,
		Path:   outputPath,
		Size:   int64(len(body)),
		SHA256: sha256Hex(body),
	}, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
