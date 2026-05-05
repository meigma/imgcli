package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/schemas/core"
)

type commandResult struct {
	stdout string
	stderr string
	err    error
}

func TestVersionOutput(t *testing.T) {
	tests := []struct {
		name    string
		version string
		args    []string
		want    string
	}{
		{
			name: "version command uses dev by default",
			args: []string{"version"},
			want: "dev\n",
		},
		{
			name: "root version flag uses dev by default",
			args: []string{"--version"},
			want: "dev\n",
		},
		{
			name:    "version command prints injected version",
			version: "1.2.3",
			args:    []string{"version"},
			want:    "1.2.3\n",
		},
		{
			name:    "root version flag prints injected version",
			version: "1.2.3",
			args:    []string{"--version"},
			want:    "1.2.3\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{Version: tt.version}, tt.args...)

			require.NoError(t, result.err)
			assert.Equal(t, tt.want, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestInvalidLogSettings(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid log level",
			args:    []string{"--log-level", "verbose", "version"},
			wantErr: `invalid log level "verbose"`,
		},
		{
			name:    "invalid log format",
			args:    []string{"--log-format", "yaml", "version"},
			wantErr: `invalid log format "yaml"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.args...)

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, tt.wantErr)
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestBaseCommands(t *testing.T) {
	tests := []struct {
		name        string
		command     string
		placeholder bool
	}{
		{
			name:        "plan",
			command:     "plan",
			placeholder: true,
		},
		{
			name:    "build",
			command: "build",
		},
		{
			name:        "publish",
			command:     "publish",
			placeholder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" appears in root help", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, "--help")

			require.NoError(t, result.err)
			assert.Contains(t, result.stdout, tt.command)
			assert.Empty(t, result.stderr)
		})

		t.Run(tt.name+" requires config operand", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.command)

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, `accepts 1 arg(s), received 0`)
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})

		if !tt.placeholder {
			continue
		}

		t.Run(tt.name+" returns placeholder error", func(t *testing.T) {
			clearIMGCLIEnv(t)

			result := executeCommand(t, Options{}, tt.command, "image.cue")

			require.Error(t, result.err)
			require.ErrorContains(t, result.err, tt.command+" command is not implemented yet")
			assert.Empty(t, result.stdout)
			assert.Empty(t, result.stderr)
		})
	}
}

func TestBuildCommand(t *testing.T) {
	t.Run("missing incusos provider fails explicitly", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
`)

		result := executeCommand(t, Options{}, "build", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, "must specify provider incusos")
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("unsupported provider fails explicitly", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
talos: {}
`)

		result := executeCommand(t, Options{}, "build", configPath)

		require.Error(t, result.err)
		require.ErrorContains(t, result.err, `unsupported provider "talos": only incusos is supported`)
		assert.Empty(t, result.stdout)
		assert.Empty(t, result.stderr)
	})

	t.Run("prints resolved IncusOS image URL", func(t *testing.T) {
		clearIMGCLIEnv(t)
		configPath := writeImageConfig(t, `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: {
	defaults: source: channel: "testing"
	variants: default: {
		source: version: "202604261712"
		artifact: {
			architecture: "amd64"
			format:       "raw"
		}
	}
}
`)
		catalog := &testCatalog{
			asset: incusos.ImageAsset{
				URL: "https://example.invalid/os/202604261712/x86_64/IncusOS_202604261712.img.gz",
			},
		}

		result := executeCommand(t, Options{IncusOSCatalog: catalog}, "build", configPath)

		require.NoError(t, result.err)
		assert.Equal(t, "https://example.invalid/os/202604261712/x86_64/IncusOS_202604261712.img.gz\n", result.stdout)
		assert.Empty(t, result.stderr)
		require.Len(t, catalog.queries, 1)
		assert.Equal(t, incusos.ImageQuery{
			Channel:      incusos.ChannelTesting,
			Version:      incusos.Version("202604261712"),
			Architecture: core.Architecture("amd64"),
			Type:         incusos.ImageTypeRaw,
		}, catalog.queries[0])
	})
}

func executeCommand(t *testing.T, opts Options, args ...string) commandResult {
	t.Helper()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	opts.Stdin = strings.NewReader("")
	opts.Stdout = &stdout
	opts.Stderr = &stderr
	opts.Environ = []string{"TERM=dumb"}

	cmd, err := NewRootCommand(opts)
	require.NoError(t, err)
	cmd.SetArgs(args)
	err = cmd.ExecuteContext(context.Background())

	return commandResult{
		stdout: stdout.String(),
		stderr: stderr.String(),
		err:    err,
	}
}

func clearIMGCLIEnv(t *testing.T) {
	t.Helper()

	for _, key := range []string{
		"IMGCLI_CONFIG",
		"IMGCLI_LOG_LEVEL",
		"IMGCLI_LOG_FORMAT",
		"IMGCLI_NO_COLOR",
	} {
		t.Setenv(key, "")
	}
}

func writeImageConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "image.cue")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

type testCatalog struct {
	asset   incusos.ImageAsset
	queries []incusos.ImageQuery
}

func (c *testCatalog) ResolveImage(_ context.Context, query incusos.ImageQuery) (incusos.ImageAsset, error) {
	c.queries = append(c.queries, query)
	return c.asset, nil
}
