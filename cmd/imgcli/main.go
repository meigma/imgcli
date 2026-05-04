package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/meigma/imgcli/internal/cli"
)

const exitFailure = 1

var version = "dev"

func main() {
	if err := run(); err != nil {
		if _, writeErr := fmt.Fprintln(os.Stderr, err); writeErr != nil {
			os.Exit(exitFailure)
		}
		os.Exit(exitFailure)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	return cli.ExecuteContext(ctx, cli.Options{Version: version})
}
