package imagefile

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers/incusos"
)

func TestInjectSeedWritesCustomizedImage(t *testing.T) {
	sourceContent := []byte("aaaabbbbcccc")
	seed := incusos.SeedArchive{Data: []byte("SEED")}
	wantContent := []byte("aaaaSEEDcccc")

	tests := []struct {
		name       string
		sourceGzip bool
		outputGzip bool
	}{
		{
			name: "plain source to plain output",
		},
		{
			name:       "gzip source to gzip output",
			sourceGzip: true,
			outputGzip: true,
		},
		{
			name:       "gzip source to plain output",
			sourceGzip: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sourcePath := filepath.Join(dir, "source.img")
			if tt.sourceGzip {
				sourcePath += ".gz"
			}
			outputPath := filepath.Join(dir, "out.img")
			if tt.outputGzip {
				outputPath += ".gz"
			}
			writeImage(t, sourcePath, sourceContent, tt.sourceGzip)
			sourceImage := downloadedImage(t, sourcePath)

			got, err := (Injector{seedOffset: 4}).InjectSeed(context.Background(), sourceImage, seed, outputPath)

			require.NoError(t, err)
			assert.Equal(t, sourceImage, got.Source)
			assert.Equal(t, outputPath, got.Path)

			size, digest := fileStats(t, outputPath)
			assert.Equal(t, size, got.Size)
			assert.Equal(t, digest, got.SHA256)

			if tt.outputGzip {
				assert.Equal(t, wantContent, readGzip(t, outputPath))
			} else {
				assert.Equal(t, wantContent, readFile(t, outputPath))
			}
		})
	}
}

func TestInjectSeedValidatesInputs(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.img")
	writeImage(t, sourcePath, []byte("aaaabbbbcccc"), false)
	validImage := downloadedImage(t, sourcePath)
	validSeed := incusos.SeedArchive{Data: []byte("SEED")}
	outputPath := filepath.Join(dir, "out.img")

	tests := []struct {
		name       string
		image      incusos.DownloadedImage
		seed       incusos.SeedArchive
		outputPath string
		wantErr    string
	}{
		{
			name:       "missing source path",
			image:      incusos.DownloadedImage{},
			seed:       validSeed,
			outputPath: outputPath,
			wantErr:    "incusos source image path is required",
		},
		{
			name:       "empty seed",
			image:      validImage,
			seed:       incusos.SeedArchive{},
			outputPath: outputPath,
			wantErr:    "incusos seed archive is empty",
		},
		{
			name:       "missing output path",
			image:      validImage,
			seed:       validSeed,
			outputPath: "",
			wantErr:    "incusos output image path is required",
		},
		{
			name:       "source equals output",
			image:      validImage,
			seed:       validSeed,
			outputPath: sourcePath,
			wantErr:    "incusos source and output image paths must differ",
		},
		{
			name:       "source symlink equals output target",
			image:      downloadedImage(t, sourceSymlink(t, sourcePath)),
			seed:       validSeed,
			outputPath: sourcePath,
			wantErr:    "incusos source and output image paths must differ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (Injector{seedOffset: 4}).InjectSeed(context.Background(), tt.image, tt.seed, tt.outputPath)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, got)
		})
	}
}

func TestInjectSeedValidatesSeedPartitionBoundary(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.img")
	outputPath := filepath.Join(dir, "out.img")
	writeImage(t, sourcePath, []byte("aaaabbbbcccc"), false)
	image := downloadedImage(t, sourcePath)

	_, err := (Injector{seedOffset: 4, seedPartitionSize: 4}).InjectSeed(
		context.Background(),
		image,
		incusos.SeedArchive{Data: []byte("SEED")},
		outputPath,
	)
	require.NoError(t, err)

	got, err := (Injector{seedOffset: 4, seedPartitionSize: 4}).InjectSeed(
		context.Background(),
		image,
		incusos.SeedArchive{Data: []byte("SEEDS")},
		filepath.Join(dir, "oversized.img"),
	)
	require.Error(t, err)
	require.ErrorContains(t, err, "incusos seed archive exceeds seed partition size")
	assert.Empty(t, got)
}

func TestInjectSeedReturnsErrorWhenSourceIsTooShort(t *testing.T) {
	tests := []struct {
		name          string
		sourceContent []byte
		seed          incusos.SeedArchive
		wantErr       string
	}{
		{
			name:          "source shorter than seed offset",
			sourceContent: []byte("abc"),
			seed:          incusos.SeedArchive{Data: []byte("S")},
			wantErr:       "source image shorter than seed offset",
		},
		{
			name:          "source shorter than seed span",
			sourceContent: []byte("abcdX"),
			seed:          incusos.SeedArchive{Data: []byte("SEED")},
			wantErr:       "source image shorter than seed span",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			sourcePath := filepath.Join(dir, "source.img")
			outputPath := filepath.Join(dir, "out.img")
			writeImage(t, sourcePath, tt.sourceContent, false)

			got, err := (Injector{seedOffset: 4}).InjectSeed(
				context.Background(),
				downloadedImage(t, sourcePath),
				tt.seed,
				outputPath,
			)

			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
			assert.Empty(t, got)
			assert.NoFileExists(t, outputPath)
			assertNoTempOutputs(t, dir, outputPath)
		})
	}
}

func TestInjectSeedReturnsContextError(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.img")
	outputPath := filepath.Join(dir, "out.img")
	writeImage(t, sourcePath, []byte("aaaabbbbcccc"), false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := (Injector{seedOffset: 4}).InjectSeed(
		ctx,
		downloadedImage(t, sourcePath),
		incusos.SeedArchive{Data: []byte("SEED")},
		outputPath,
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, got)
	assert.NoFileExists(t, outputPath)
}

func sourceSymlink(t *testing.T, target string) string {
	t.Helper()

	linkPath := filepath.Join(t.TempDir(), "source-link.img")
	require.NoError(t, os.Symlink(target, linkPath))

	return linkPath
}

func downloadedImage(t *testing.T, path string) incusos.DownloadedImage {
	t.Helper()

	size, digest := fileStats(t, path)
	return incusos.DownloadedImage{
		Path:   path,
		Size:   size,
		SHA256: digest,
	}
}

func writeImage(t *testing.T, path string, content []byte, gzipOutput bool) {
	t.Helper()

	if !gzipOutput {
		require.NoError(t, os.WriteFile(path, content, 0o600))
		return
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	defer file.Close()

	writer := gzip.NewWriter(file)
	_, err = writer.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	require.NoError(t, err)

	return content
}

func readGzip(t *testing.T, path string) []byte {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer file.Close()

	reader, err := gzip.NewReader(file)
	require.NoError(t, err)
	defer reader.Close()

	content, err := io.ReadAll(reader)
	require.NoError(t, err)

	return content
}

func fileStats(t *testing.T, path string) (int64, string) {
	t.Helper()

	content := readFile(t, path)
	sum := sha256.Sum256(content)

	return int64(len(content)), hex.EncodeToString(sum[:])
}

func assertNoTempOutputs(t *testing.T, dir string, outputPath string) {
	t.Helper()

	matches, err := filepath.Glob(filepath.Join(dir, "."+filepath.Base(outputPath)+".*.tmp"))
	require.NoError(t, err)
	assert.Empty(t, matches)
}
