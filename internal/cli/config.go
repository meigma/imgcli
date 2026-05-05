package cli

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
	// KeyCacheDir is the Viper key for the cache root directory.
	KeyCacheDir = "cache.dir"
	// KeyCacheMaxSize is the Viper key for the maximum cache size before LRU pruning.
	KeyCacheMaxSize = "cache.max-size"
)

const (
	flagConfig       = "config"
	flagLogLevel     = "log-level"
	flagLogFormat    = "log-format"
	flagNoColor      = "no-color"
	flagCacheDir     = "cache-dir"
	flagCacheMaxSize = "cache-max-size"
)

const (
	defaultConfigDirName  = "imgcli"
	defaultConfigFileName = "config.yaml"
	defaultLogLevel       = "info"
	defaultLogFormat      = "text"
	defaultCacheMaxSize   = "10GB"

	cacheSizeKiBShift = 10
	cacheSizeMiBShift = 20
	cacheSizeGiBShift = 30
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

	// CacheDir is the optional cache root directory. Empty selects the platform cache directory.
	CacheDir string

	// CacheMaxSizeBytes is the maximum cache size used by LRU pruning. Zero disables pruning.
	CacheMaxSizeBytes int64
}

func configureViper(vp *viper.Viper) {
	vp.SetEnvPrefix(envPrefix)
	vp.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	vp.AutomaticEnv()

	vp.SetDefault(KeyLogLevel, defaultLogLevel)
	vp.SetDefault(KeyLogFormat, defaultLogFormat)
	vp.SetDefault(KeyNoColor, false)
	vp.SetDefault(KeyCacheDir, "")
	vp.SetDefault(KeyCacheMaxSize, defaultCacheMaxSize)
}

func (rt *runtime) registerGlobalFlags(root *cobra.Command) error {
	flags := root.PersistentFlags()
	flags.String(flagConfig, "", "Path to an imgcli config file")
	flags.String(flagLogLevel, defaultLogLevel, "Minimum log level: debug, info, warn, or error")
	flags.String(flagLogFormat, defaultLogFormat, "Log format: text, json, or logfmt")
	flags.Bool(flagNoColor, false, "Disable styled terminal output")
	flags.String(flagCacheDir, "", "Cache directory")
	flags.String(
		flagCacheMaxSize,
		defaultCacheMaxSize,
		"Maximum cache size used by LRU pruning, or 0 to disable",
	)

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
	if err := bindConfigFlag(rt.viper, flags, KeyCacheDir, flagCacheDir); err != nil {
		return err
	}
	if err := bindConfigFlag(rt.viper, flags, KeyCacheMaxSize, flagCacheMaxSize); err != nil {
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
	configFile, err := readConfigFile(vp)
	if err != nil {
		return Config{}, err
	}

	cacheMaxSize, err := parseSizeConfig(vp, KeyCacheMaxSize)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		ConfigFile:        configFile,
		LogLevel:          strings.ToLower(strings.TrimSpace(vp.GetString(KeyLogLevel))),
		LogFormat:         strings.ToLower(strings.TrimSpace(vp.GetString(KeyLogFormat))),
		NoColor:           vp.GetBool(KeyNoColor),
		CacheDir:          strings.TrimSpace(vp.GetString(KeyCacheDir)),
		CacheMaxSizeBytes: cacheMaxSize,
	}

	if err := validateConfig(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func readConfigFile(vp *viper.Viper) (string, error) {
	configFile := vp.GetString(KeyConfig)
	if configFile != "" {
		return readNamedConfigFile(vp, configFile)
	}

	defaultConfig, err := defaultConfigFile()
	if err != nil {
		return "", err
	}
	discoveredConfig, err := discoverExistingConfig(defaultConfig)
	if err != nil || discoveredConfig == "" {
		return "", err
	}

	return readNamedConfigFile(vp, discoveredConfig)
}

func readNamedConfigFile(vp *viper.Viper, configFile string) (string, error) {
	vp.SetConfigFile(configFile)
	if err := vp.ReadInConfig(); err != nil {
		return "", fmt.Errorf("read config file %q: %w", configFile, err)
	}

	return configFile, nil
}

func discoverExistingConfig(path string) (string, error) {
	if path == "" {
		return "", nil
	}

	if _, err := os.Stat(path); err == nil {
		return path, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("inspect config file %q: %w", path, err)
	}

	return "", nil
}

func defaultConfigFile() (string, error) {
	if xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdgConfigHome != "" {
		if !filepath.IsAbs(xdgConfigHome) {
			return "", errors.New("resolve config directory: XDG_CONFIG_HOME must be an absolute path")
		}
		return filepath.Join(xdgConfigHome, defaultConfigDirName, defaultConfigFileName), nil
	}

	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config directory: %w", err)
	}

	return filepath.Join(userConfigDir, defaultConfigDirName, defaultConfigFileName), nil
}

func parseSizeConfig(vp *viper.Viper, key string) (int64, error) {
	raw := strings.TrimSpace(vp.GetString(key))
	if _, err := parseSizeLiteral(raw); err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}

	size := vp.GetSizeInBytes(key)
	if uint64(size) > uint64(math.MaxInt64) {
		return 0, fmt.Errorf("invalid %s %q: size is too large", key, raw)
	}

	return int64(size), nil
}

func parseSizeLiteral(raw string) (int64, error) {
	if raw == "" {
		return 0, errors.New("size is required")
	}

	lower := strings.ToLower(strings.TrimSpace(raw))
	multiplier := int64(1)
	number := lower
	for _, suffix := range []struct {
		unit       string
		multiplier int64
	}{
		{unit: "gb", multiplier: 1 << cacheSizeGiBShift},
		{unit: "mb", multiplier: 1 << cacheSizeMiBShift},
		{unit: "kb", multiplier: 1 << cacheSizeKiBShift},
		{unit: "b", multiplier: 1},
	} {
		if strings.HasSuffix(lower, suffix.unit) {
			multiplier = suffix.multiplier
			number = strings.TrimSpace(lower[:len(lower)-len(suffix.unit)])
			break
		}
	}

	value, err := strconv.ParseInt(number, 10, 64)
	if err != nil {
		return 0, errors.New("must be an integer byte size with optional B, KB, MB, or GB suffix")
	}
	if value < 0 {
		return 0, errors.New("must be non-negative")
	}
	if value > math.MaxInt64/multiplier {
		return 0, errors.New("size is too large")
	}

	return value * multiplier, nil
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
