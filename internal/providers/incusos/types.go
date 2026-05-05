package incusos

import (
	"context"

	"github.com/meigma/imgcli/schemas/core"
	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

// Catalog resolves IncusOS image queries into source image assets.
type Catalog interface {
	// ResolveImage selects an IncusOS source image asset for the query.
	ResolveImage(ctx context.Context, query ImageQuery) (ImageAsset, error)
}

// Downloader retrieves IncusOS source image assets.
type Downloader interface {
	// DownloadImage downloads and verifies the provided image asset.
	DownloadImage(ctx context.Context, asset ImageAsset) (DownloadedImage, error)
}

// SeedBuilder creates IncusOS seed archives.
type SeedBuilder interface {
	// BuildSeed creates the seed archive for a provider configuration.
	BuildSeed(ctx context.Context, config Config) (SeedArchive, error)
}

// ImageInjector writes IncusOS seed archives into local images.
type ImageInjector interface {
	// InjectSeed writes a seed archive into a downloaded image.
	InjectSeed(ctx context.Context, image DownloadedImage, seed SeedArchive, outputPath string) (CustomizedImage, error)
}

// Channel is an IncusOS update channel.
type Channel = incusosschema.Channel

// Version is an IncusOS release version.
type Version = incusosschema.Version

// ImageType is an IncusOS source image type.
type ImageType string

const (
	// ChannelStable selects IncusOS releases promoted to stable.
	ChannelStable Channel = "stable"

	// ChannelTesting selects IncusOS releases published to testing.
	ChannelTesting Channel = "testing"
)

const (
	// ImageTypeISO selects the IncusOS ISO image.
	ImageTypeISO ImageType = "iso"

	// ImageTypeRaw selects the IncusOS raw disk image.
	ImageTypeRaw ImageType = "raw"
)

// ImageQuery describes an IncusOS source image lookup.
type ImageQuery struct {
	// Channel selects the IncusOS release channel.
	Channel Channel

	// Version selects a specific IncusOS release version. Empty means latest.
	Version Version

	// Architecture selects the source image architecture.
	Architecture core.Architecture

	// Type selects the source image type.
	Type ImageType
}

// ImageAsset describes an IncusOS source image asset.
type ImageAsset struct {
	// Version is the IncusOS release version containing this asset.
	Version Version

	// Architecture is the asset architecture.
	Architecture core.Architecture

	// Type is the source image type.
	Type ImageType

	// URL is the download URL for this asset.
	URL string

	// SHA256 is the expected SHA-256 digest in lowercase hex.
	SHA256 string

	// Size is the compressed source asset size in bytes.
	Size int64
}

// DownloadedImage describes a verified local source image.
type DownloadedImage struct {
	// Asset is the source asset that was downloaded.
	Asset ImageAsset

	// Path is the verified local image path.
	Path string

	// SHA256 is the verified SHA-256 digest in lowercase hex.
	SHA256 string

	// Size is the local image size in bytes.
	Size int64
}

// SeedArchive describes an IncusOS seed tar archive.
type SeedArchive struct {
	// Data is the tar archive content to inject into the image.
	Data []byte
}

// CustomizedImage describes an image after IncusOS seed injection.
type CustomizedImage struct {
	// Source is the verified source image that was customized.
	Source DownloadedImage

	// Path is the customized local image path.
	Path string

	// Size is the customized image size in bytes.
	Size int64

	// SHA256 is the customized image SHA-256 digest in lowercase hex.
	SHA256 string
}
