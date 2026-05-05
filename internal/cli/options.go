package cli

import (
	"io"
	"os"

	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/internal/publish"
)

// Options configures the root imgcli command.
type Options struct {
	// Version is the build version printed by the version command.
	Version string

	// Stdin receives interactive input. Nil selects os.Stdin.
	Stdin io.Reader

	// Stdout receives command results. Nil selects os.Stdout.
	Stdout io.Writer

	// Stderr receives logs and diagnostics. Nil selects os.Stderr.
	Stderr io.Writer

	// Environ provides terminal environment values for output adapters. Nil selects os.Environ().
	Environ []string

	// IncusOSCatalog resolves IncusOS source images. Nil selects the default CDN catalog.
	IncusOSCatalog incusos.Catalog

	// IncusOSDownloader retrieves IncusOS source images. Nil selects the default CDN downloader.
	IncusOSDownloader incusos.Downloader

	// IncusOSSeedBuilder creates IncusOS seed archives. Nil selects the default seed builder.
	IncusOSSeedBuilder incusos.SeedBuilder

	// IncusOSImageInjector writes IncusOS seed archives into source images. Nil selects the default image injector.
	IncusOSImageInjector incusos.ImageInjector

	// ImgsrvUploadsClient uploads artifacts to imgsrv. Nil selects the HTTP SDK client.
	ImgsrvUploadsClient publish.UploadsClient
}

func (o Options) version() string {
	if o.Version == "" {
		return defaultVersion
	}
	return o.Version
}

func (o Options) stdin() io.Reader {
	if o.Stdin == nil {
		return os.Stdin
	}
	return o.Stdin
}

func (o Options) stdout() io.Writer {
	if o.Stdout == nil {
		return os.Stdout
	}
	return o.Stdout
}

func (o Options) stderr() io.Writer {
	if o.Stderr == nil {
		return os.Stderr
	}
	return o.Stderr
}

func (o Options) environ() []string {
	if o.Environ == nil {
		return os.Environ()
	}
	return o.Environ
}
