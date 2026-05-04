package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const (
	envPrefix = "IMGCLI"

	// KeyConfig is the Viper key for the optional config file path.
	KeyConfig = "config"
	// KeyLogLevel is the Viper key for the minimum log level.
	KeyLogLevel = "log-level"
	// KeyLogFormat is the Viper key for the log formatter.
	KeyLogFormat = "log-format"
	// KeyNoColor is the Viper key for disabling styled terminal output.
	KeyNoColor = "no-color"
)

const (
	flagConfig    = "config"
	flagLogLevel  = "log-level"
	flagLogFormat = "log-format"
	flagNoColor   = "no-color"
)

const (
	defaultLogLevel  = "info"
	defaultLogFormat = "text"
)

// Config is the CLI edge configuration resolved from flags, environment, config file, and defaults.
type Config struct {
	// ConfigFile is the optional config file path used for this invocation.
	ConfigFile string

	// LogLevel is the minimum log level emitted to stderr.
	LogLevel string

	// LogFormat is the log formatter emitted to stderr.
	LogFormat string

	// NoColor disables styled terminal output when true.
	NoColor bool
}

func configureViper(vp *viper.Viper) {
	vp.SetEnvPrefix(envPrefix)
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	vp.SetDefault(KeyLogLevel, defaultLogLevel)
	vp.SetDefault(KeyLogFormat, defaultLogFormat)
	vp.SetDefault(KeyNoColor, false)
}

func (rt *runtime) registerGlobalFlags(root *cobra.Command) error {
	flags := root.PersistentFlags()
	flags.String(flagConfig, "", "Path to an imgcli config file")
	flags.String(flagLogLevel, defaultLogLevel, "Minimum log level: debug, info, warn, or error")
	flags.String(flagLogFormat, defaultLogFormat, "Log format: text, json, or logfmt")
	flags.Bool(flagNoColor, false, "Disable styled terminal output")

	if err := bindConfigFlag(rt.viper, flags, KeyConfig, flagConfig); err != nil {
		return err
	}
	if err := bindConfigFlag(rt.viper, flags, KeyLogLevel, flagLogLevel); err != nil {
		return err
	}
	if err := bindConfigFlag(rt.viper, flags, KeyLogFormat, flagLogFormat); err != nil {
		return err
	}
	if err := bindConfigFlag(rt.viper, flags, KeyNoColor, flagNoColor); err != nil {
		return err
	}

	return nil
}

func bindConfigFlag(vp *viper.Viper, flags *pflag.FlagSet, key string, flagName string) error {
	flag := flags.Lookup(flagName)
	if flag == nil {
		return fmt.Errorf("bind config flag %q: flag not found", flagName)
	}
	if err := vp.BindPFlag(key, flag); err != nil {
		return fmt.Errorf("bind flag %q to key %q: %w", flagName, key, err)
	}
	if err := vp.BindEnv(key); err != nil {
		return fmt.Errorf("bind env for key %q: %w", key, err)
	}
	return nil
}

func loadConfig(vp *viper.Viper) (Config, error) {
	if configFile := vp.GetString(KeyConfig); configFile != "" {
		vp.SetConfigFile(configFile)
		if err := vp.ReadInConfig(); err != nil {
			return Config{}, fmt.Errorf("read config file %q: %w", configFile, err)
		}
	}

	cfg := Config{
		ConfigFile: vp.GetString(KeyConfig),
		LogLevel:   strings.ToLower(strings.TrimSpace(vp.GetString(KeyLogLevel))),
		LogFormat:  strings.ToLower(strings.TrimSpace(vp.GetString(KeyLogFormat))),
		NoColor:    vp.GetBool(KeyNoColor),
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validateConfig(cfg Config) error {
	if _, err := parseLogLevel(cfg.LogLevel); err != nil {
		return err
	}
	if _, err := parseLogFormat(cfg.LogFormat); err != nil {
		return err
	}
	return nil
}
