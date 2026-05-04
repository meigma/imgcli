package cli

import (
	"fmt"
	"io"
	"log/slog"

	charmLog "charm.land/log/v2"
	"github.com/charmbracelet/colorprofile"
)

func newLogger(cfg Config, stderr io.Writer, environ []string) (*slog.Logger, error) {
	level, err := parseLogLevel(cfg.LogLevel)
	if err != nil {
		return nil, err
	}

	formatter, err := parseLogFormat(cfg.LogFormat)
	if err != nil {
		return nil, err
	}

	handler := charmLog.NewWithOptions(stderr, charmLog.Options{
		Level:           level,
		Formatter:       formatter,
		ReportTimestamp: false,
	})
	if cfg.NoColor {
		handler.SetColorProfile(colorprofile.NoTTY)
	} else {
		handler.SetColorProfile(colorprofile.Detect(stderr, environ))
	}

	return slog.New(handler), nil
}

func parseLogLevel(value string) (charmLog.Level, error) {
	switch value {
	case "debug":
		return charmLog.DebugLevel, nil
	case "info":
		return charmLog.InfoLevel, nil
	case "warn":
		return charmLog.WarnLevel, nil
	case "error":
		return charmLog.ErrorLevel, nil
	default:
		return 0, fmt.Errorf("invalid log level %q: expected debug, info, warn, or error", value)
	}
}

func parseLogFormat(value string) (charmLog.Formatter, error) {
	switch value {
	case "text":
		return charmLog.TextFormatter, nil
	case "json":
		return charmLog.JSONFormatter, nil
	case "logfmt":
		return charmLog.LogfmtFormatter, nil
	default:
		return 0, fmt.Errorf("invalid log format %q: expected text, json, or logfmt", value)
	}
}
