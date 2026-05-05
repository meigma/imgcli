package imagefile

import (
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/meigma/imgcli/internal/providers/incusos"
)

const (
	defaultSeedOffset        = 2148532224
	defaultSeedPartitionSize = 100 * 1024 * 1024
	gzipHeaderLength         = 2
	gzipID1                  = 0x1f
	gzipID2                  = 0x8b
	copyBufferSize           = 1024 * 1024
	outputFileMode           = 0o600
	outputDirMode            = 0o755
)

var _ incusos.ImageInjector = Injector{}

// Injector writes IncusOS seed archives into local image files.
type Injector struct {
	seedOffset        int64
	seedPartitionSize int64
}

// InjectSeed copies image to outputPath while replacing the upstream IncusOS seed region.
func (i Injector) InjectSeed(
	ctx context.Context,
	image incusos.DownloadedImage,
	seed incusos.SeedArchive,
	outputPath string,
) (incusos.CustomizedImage, error) {
	if err := ctx.Err(); err != nil {
		return incusos.CustomizedImage{}, err
	}

	if err := validateInputs(image, seed, outputPath); err != nil {
		return incusos.CustomizedImage{}, err
	}

	if err := i.validateSeedSize(seed); err != nil {
		return incusos.CustomizedImage{}, err
	}

	if err := rejectSamePath(image.Path, outputPath); err != nil {
		return incusos.CustomizedImage{}, err
	}

	source, openErr := os.Open(image.Path)
	if openErr != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("open incusos source image: %w", openErr)
	}
	defer source.Close()

	outputDir := filepath.Dir(outputPath)
	if mkdirErr := os.MkdirAll(outputDir, outputDirMode); mkdirErr != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("create incusos output directory: %w", mkdirErr)
	}

	temp, createErr := os.CreateTemp(outputDir, "."+filepath.Base(outputPath)+".*.tmp")
	if createErr != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("create temporary incusos output image: %w", createErr)
	}

	tempPath := temp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if chmodErr := temp.Chmod(outputFileMode); chmodErr != nil {
		_ = temp.Close()
		return incusos.CustomizedImage{}, fmt.Errorf("set temporary incusos output image mode: %w", chmodErr)
	}

	if injectErr := i.inject(ctx, source, temp, seed, outputPath); injectErr != nil {
		_ = temp.Close()
		return incusos.CustomizedImage{}, injectErr
	}

	if closeErr := temp.Close(); closeErr != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("close temporary incusos output image: %w", closeErr)
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return incusos.CustomizedImage{}, ctxErr
	}

	size, digest, digestErr := fileDigest(tempPath)
	if digestErr != nil {
		return incusos.CustomizedImage{}, digestErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return incusos.CustomizedImage{}, ctxErr
	}

	if renameErr := os.Rename(tempPath, outputPath); renameErr != nil {
		return incusos.CustomizedImage{}, fmt.Errorf("replace incusos output image: %w", renameErr)
	}
	keepTemp = true

	return incusos.CustomizedImage{
		Source: image,
		Path:   outputPath,
		Size:   size,
		SHA256: digest,
	}, nil
}

func (i Injector) inject(
	ctx context.Context,
	source io.Reader,
	output io.Writer,
	seed incusos.SeedArchive,
	outputPath string,
) error {
	sourceReader, closeSource, err := decompressedSource(source)
	if err != nil {
		return err
	}
	defer func() {
		_ = closeSource()
	}()

	outputWriter, closeOutput := compressedOutput(output, outputPath)
	defer func() {
		_ = closeOutput()
	}()

	buffer := make([]byte, copyBufferSize)
	offset := i.seedOffsetOrDefault()
	if err := copyExactly(ctx, outputWriter, sourceReader, offset, buffer); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("copy incusos image prefix: source image shorter than seed offset: %w", err)
		}

		return fmt.Errorf("copy incusos image prefix: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := writeAll(ctx, outputWriter, seed.Data); err != nil {
		return fmt.Errorf("write incusos seed archive: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return err
	}

	if err := discardExactly(ctx, sourceReader, int64(len(seed.Data)), buffer); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("discard incusos image seed span: source image shorter than seed span: %w", err)
		}

		return fmt.Errorf("discard incusos image seed span: %w", err)
	}

	if err := copyRemaining(ctx, outputWriter, sourceReader, buffer); err != nil {
		return fmt.Errorf("copy incusos image suffix: %w", err)
	}

	if err := closeOutput(); err != nil {
		return err
	}

	return nil
}

func (i Injector) seedOffsetOrDefault() int64 {
	if i.seedOffset > 0 {
		return i.seedOffset
	}

	return defaultSeedOffset
}

func (i Injector) seedPartitionSizeOrDefault() int64 {
	if i.seedPartitionSize > 0 {
		return i.seedPartitionSize
	}

	return defaultSeedPartitionSize
}

func (i Injector) validateSeedSize(seed incusos.SeedArchive) error {
	partitionSize := i.seedPartitionSizeOrDefault()
	if int64(len(seed.Data)) > partitionSize {
		return fmt.Errorf(
			"incusos seed archive exceeds seed partition size: seed=%d bytes partition=%d bytes",
			len(seed.Data),
			partitionSize,
		)
	}

	return nil
}

func validateInputs(image incusos.DownloadedImage, seed incusos.SeedArchive, outputPath string) error {
	if strings.TrimSpace(image.Path) == "" {
		return errors.New("incusos source image path is required")
	}
	if len(seed.Data) == 0 {
		return errors.New("incusos seed archive is empty")
	}
	if strings.TrimSpace(outputPath) == "" {
		return errors.New("incusos output image path is required")
	}

	return nil
}

func rejectSamePath(sourcePath string, outputPath string) error {
	sourceAbs, err := filepath.Abs(sourcePath)
	if err != nil {
		return fmt.Errorf("resolve incusos source image path: %w", err)
	}

	outputAbs, err := filepath.Abs(outputPath)
	if err != nil {
		return fmt.Errorf("resolve incusos output image path: %w", err)
	}

	if sourceAbs == outputAbs {
		return errors.New("incusos source and output image paths must differ")
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("stat incusos source image path: %w", err)
	}

	outputInfo, err := os.Stat(outputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat incusos output image path: %w", err)
	}

	if os.SameFile(sourceInfo, outputInfo) {
		return errors.New("incusos source and output image paths must differ")
	}

	return nil
}

func decompressedSource(source io.Reader) (io.Reader, func() error, error) {
	buffered := bufio.NewReader(source)
	header, err := buffered.Peek(gzipHeaderLength)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, bufio.ErrBufferFull) {
		return nil, nil, fmt.Errorf("detect incusos source image compression: %w", err)
	}

	if len(header) < gzipHeaderLength || header[0] != gzipID1 || header[1] != gzipID2 {
		return buffered, func() error { return nil }, nil
	}

	gzipReader, err := gzip.NewReader(buffered)
	if err != nil {
		return nil, nil, fmt.Errorf("open gzip incusos source image: %w", err)
	}

	return gzipReader, gzipReader.Close, nil
}

func compressedOutput(output io.Writer, outputPath string) (io.Writer, func() error) {
	if !strings.HasSuffix(outputPath, ".gz") {
		return output, func() error { return nil }
	}

	gzipWriter := gzip.NewWriter(output)
	closed := false
	closeWriter := func() error {
		if closed {
			return nil
		}
		closed = true
		if err := gzipWriter.Close(); err != nil {
			return fmt.Errorf("close gzip incusos output image: %w", err)
		}

		return nil
	}

	return gzipWriter, closeWriter
}

func copyExactly(ctx context.Context, dst io.Writer, src io.Reader, n int64, buffer []byte) error {
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		readSize := min(n, int64(len(buffer)))

		read, readErr := src.Read(buffer[:readSize])
		if read > 0 {
			if err := writeAll(ctx, dst, buffer[:read]); err != nil {
				return err
			}
			n -= int64(read)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if n == 0 {
					return ctx.Err()
				}

				return io.ErrUnexpectedEOF
			}

			return readErr
		}
	}

	return ctx.Err()
}

func discardExactly(ctx context.Context, src io.Reader, n int64, buffer []byte) error {
	for n > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		readSize := min(n, int64(len(buffer)))

		read, readErr := src.Read(buffer[:readSize])
		if read > 0 {
			n -= int64(read)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if n == 0 {
					return ctx.Err()
				}

				return io.ErrUnexpectedEOF
			}

			return readErr
		}
	}

	return ctx.Err()
}

func copyRemaining(ctx context.Context, dst io.Writer, src io.Reader, buffer []byte) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		read, readErr := src.Read(buffer)
		if read > 0 {
			if err := writeAll(ctx, dst, buffer[:read]); err != nil {
				return err
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}

			return readErr
		}
	}
}

func writeAll(ctx context.Context, dst io.Writer, data []byte) error {
	for len(data) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}

		written, err := dst.Write(data)
		if written > 0 {
			data = data[written:]
		}

		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}

	return ctx.Err()
}

func fileDigest(path string) (int64, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, "", fmt.Errorf("open customized incusos image for digest: %w", err)
	}
	defer file.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return 0, "", fmt.Errorf("digest customized incusos image: %w", err)
	}

	return size, hex.EncodeToString(hash.Sum(nil)), nil
}
