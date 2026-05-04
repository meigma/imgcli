package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

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

func writeConfig(t *testing.T, key string, value string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "imgcli.yaml")
	content := fmt.Sprintf("%s: %q\n", key, value)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}
