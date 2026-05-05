package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogConfigPrecedence(t *testing.T) {
	tests := []struct {
		name         string
		configKey    string
		envName      string
		flagName     string
		validValue   string
		invalidValue string
		wantErr      string
	}{
		{
			name:         "log level",
			configKey:    KeyLogLevel,
			envName:      "IMGCLI_LOG_LEVEL",
			flagName:     flagLogLevel,
			validValue:   "debug",
			invalidValue: "verbose",
			wantErr:      `invalid log level "verbose"`,
		},
		{
			name:         "log format",
			configKey:    KeyLogFormat,
			envName:      "IMGCLI_LOG_FORMAT",
			flagName:     flagLogFormat,
			validValue:   "json",
			invalidValue: "yaml",
			wantErr:      `invalid log format "yaml"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" invalid config file fails", func(t *testing.T) {
			clearIMGCLIEnv(t)
			configPath := writeConfig(t, tt.configKey, tt.invalidValue)

			result := executeCommand(t, Options{}, "--config", configPath, "version")

			require.Error(t, result.err)
			assert.ErrorContains(t, result.err, tt.wantErr)
		})

		t.Run(tt.name+" env overrides config file", func(t *testing.T) {
			clearIMGCLIEnv(t)
			configPath := writeConfig(t, tt.configKey, tt.invalidValue)
			t.Setenv(tt.envName, tt.validValue)

			result := executeCommand(t, Options{}, "--config", configPath, "version")

			require.NoError(t, result.err)
			assert.Equal(t, "dev\n", result.stdout)
		})

		t.Run(tt.name+" flag overrides env", func(t *testing.T) {
			clearIMGCLIEnv(t)
			t.Setenv(tt.envName, tt.invalidValue)

			result := executeCommand(t, Options{}, "--"+tt.flagName, tt.validValue, "version")

			require.NoError(t, result.err)
			assert.Equal(t, "dev\n", result.stdout)
		})
	}
}

func TestConfigFileCanComeFromEnvironment(t *testing.T) {
	clearIMGCLIEnv(t)
	configPath := writeConfig(t, KeyLogFormat, "yaml")
	t.Setenv("IMGCLI_CONFIG", configPath)

	result := executeCommand(t, Options{}, "version")

	require.Error(t, result.err)
	assert.ErrorContains(t, result.err, `invalid log format "yaml"`)
}

func TestConfigFlagOverridesConfigEnvironment(t *testing.T) {
	clearIMGCLIEnv(t)
	envConfigPath := writeConfig(t, KeyLogFormat, "yaml")
	flagConfigPath := writeConfig(t, KeyLogFormat, "logfmt")
	t.Setenv("IMGCLI_CONFIG", envConfigPath)

	result := executeCommand(t, Options{}, "--config", flagConfigPath, "version")

	require.NoError(t, result.err)
	assert.Equal(t, "dev\n", result.stdout)
}

func TestDefaultConfigFileLoadsFromXDGConfigHome(t *testing.T) {
	clearIMGCLIEnv(t)
	xdgConfigHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	configPath := filepath.Join(xdgConfigHome, "imgcli", "config.yaml")
	writeConfigContent(t, configPath, fmt.Sprintf("%s: %q\n", KeyLogFormat, "yaml"))

	result := executeCommand(t, Options{}, "version")

	require.Error(t, result.err)
	assert.ErrorContains(t, result.err, `invalid log format "yaml"`)
}

func TestConfigFlagOverridesDefaultXDGConfigFile(t *testing.T) {
	clearIMGCLIEnv(t)
	xdgConfigHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	defaultConfigPath := filepath.Join(xdgConfigHome, "imgcli", "config.yaml")
	writeConfigContent(t, defaultConfigPath, fmt.Sprintf("%s: %q\n", KeyLogFormat, "yaml"))
	flagConfigPath := writeConfig(t, KeyLogFormat, "logfmt")

	result := executeCommand(t, Options{}, "--config", flagConfigPath, "version")

	require.NoError(t, result.err)
	assert.Equal(t, "dev\n", result.stdout)
}

func TestCacheConfigDefaults(t *testing.T) {
	clearIMGCLIEnv(t)
	cfg, err := loadConfig(newConfigViper())

	require.NoError(t, err)
	assert.Empty(t, cfg.CacheDir)
	assert.Equal(t, int64(10*(1<<30)), cfg.CacheMaxSizeBytes)
}

func TestCacheConfigFileValues(t *testing.T) {
	clearIMGCLIEnv(t)
	cacheDir := filepath.Join(t.TempDir(), "cache")
	configPath := filepath.Join(t.TempDir(), "imgcli.yaml")
	writeConfigContent(t, configPath, fmt.Sprintf(`
cache:
  dir: %q
  max-size: "0"
`, cacheDir))
	vp := newConfigViper()
	vp.Set(KeyConfig, configPath)

	cfg, err := loadConfig(vp)

	require.NoError(t, err)
	assert.Equal(t, configPath, cfg.ConfigFile)
	assert.Equal(t, cacheDir, cfg.CacheDir)
	assert.Equal(t, int64(0), cfg.CacheMaxSizeBytes)
}

func TestCacheConfigPrecedence(t *testing.T) {
	t.Run("invalid config file fails", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeConfig(t, KeyCacheMaxSize, "nope")

		result := executeCommand(t, Options{}, "--config", configPath, "version")

		require.Error(t, result.err)
		assert.ErrorContains(t, result.err, `invalid cache.max-size "nope"`)
	})

	t.Run("env overrides config file", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeConfig(t, KeyCacheMaxSize, "nope")
		t.Setenv("IMGCLI_CACHE_MAX_SIZE", "1GB")

		result := executeCommand(t, Options{}, "--config", configPath, "version")

		require.NoError(t, result.err)
		assert.Equal(t, "dev\n", result.stdout)
	})

	t.Run("flag overrides env", func(t *testing.T) {
		clearIMGCLIEnv(t)
		t.Setenv("IMGCLI_CACHE_MAX_SIZE", "nope")

		result := executeCommand(t, Options{}, "--cache-max-size", "1GB", "version")

		require.NoError(t, result.err)
		assert.Equal(t, "dev\n", result.stdout)
	})
}

func TestCacheConfigRejectsUnsupportedSizeUnits(t *testing.T) {
	clearIMGCLIEnv(t)
	configPath := writeConfig(t, KeyCacheMaxSize, "10GiB")

	result := executeCommand(t, Options{}, "--config", configPath, "version")

	require.Error(t, result.err)
	assert.ErrorContains(t, result.err, `invalid cache.max-size "10GiB"`)
}

func writeConfig(t *testing.T, key string, value string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "imgcli.yaml")
	writeConfigContent(t, path, fmt.Sprintf("%s: %q\n", key, value))
	return path
}

func writeConfigContent(t *testing.T, path string, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func newConfigViper() *viper.Viper {
	vp := viper.New()
	configureViper(vp)
	return vp
}
