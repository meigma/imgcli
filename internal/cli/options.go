package cli

import (
	"io"
	"os"
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
