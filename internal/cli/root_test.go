package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
