package schemas

import (
	"bytes"
	"io/fs"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModuleFS(t *testing.T) {
	moduleFile, err := fs.ReadFile(ModuleFS(), "cue.mod/module.cue")
	if err != nil {
		t.Fatalf("read module file: %v", err)
	}
	if !bytes.Contains(moduleFile, []byte(ModulePath)) {
		t.Fatalf("module file does not contain module path %q", ModulePath)
	}
}

func TestConfigSchema(t *testing.T) {
	ctx := cuecontext.New()

	schema, err := ConfigSchema(ctx)
	if err != nil {
		t.Fatalf("load config schema: %v", err)
	}
	if got := schema.IncompleteKind(); got != cue.StructKind {
		t.Fatalf("#Config kind = %v, want %v", got, cue.StructKind)
	}

	input := ctx.CompileString(`
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: {
	name: "test-image"
}
incusos: variants: default: artifact: {
	architecture: "amd64"
	format:       "raw"
}
`)
	if err := input.Err(); err != nil {
		t.Fatalf("compile input: %v", err)
	}

	value := schema.Unify(input)
	if err := value.Validate(cue.Concrete(false)); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	provider, err := value.LookupPath(cue.ParsePath("incusos.variants.default.artifact.provider")).String()
	if err != nil {
		t.Fatalf("lookup provider: %v", err)
	}
	if provider != "incusos" {
		t.Fatalf("provider = %q, want %q", provider, "incusos")
	}
}

func TestIncusOSSourceSchema(t *testing.T) {
	ctx := cuecontext.New()

	schema, err := ConfigSchema(ctx)
	if err != nil {
		t.Fatalf("load config schema: %v", err)
	}

	input := ctx.CompileString(`
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: {
	name: "test-image"
}
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
	if err := input.Err(); err != nil {
		t.Fatalf("compile input: %v", err)
	}

	value := schema.Unify(input)
	if err := value.Validate(cue.Concrete(false)); err != nil {
		t.Fatalf("validate config: %v", err)
	}

	channel, err := value.LookupPath(cue.ParsePath("incusos.defaults.source.channel")).String()
	if err != nil {
		t.Fatalf("lookup channel: %v", err)
	}
	if channel != "testing" {
		t.Fatalf("default source channel = %q, want %q", channel, "testing")
	}

	version, err := value.LookupPath(cue.ParsePath("incusos.variants.default.source.version")).String()
	if err != nil {
		t.Fatalf("lookup version: %v", err)
	}
	if version != "202604261712" {
		t.Fatalf("variant source version = %q, want %q", version, "202604261712")
	}
}

func TestIncusOSSourceValidation(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name: "invalid channel",
			input: `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: variants: default: {
	source: channel: "edge"
	artifact: {
		architecture: "amd64"
		format:       "raw"
	}
}
`,
		},
		{
			name: "invalid version",
			input: `
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: variants: default: {
	source: version: "latest"
	artifact: {
		architecture: "amd64"
		format:       "raw"
	}
}
`,
		},
	}

	ctx := cuecontext.New()

	schema, err := ConfigSchema(ctx)
	if err != nil {
		t.Fatalf("load config schema: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ctx.CompileString(tt.input)
			if err := input.Err(); err != nil {
				t.Fatalf("compile input: %v", err)
			}

			value := schema.Unify(input)
			if err := value.Validate(cue.Concrete(false)); err == nil {
				t.Fatal("validate config succeeded, want error")
			}
		})
	}
}

func TestIncusOSSeedSchema(t *testing.T) {
	ctx := cuecontext.New()

	schema, err := ConfigSchema(ctx)
	require.NoError(t, err)

	input := ctx.CompileString(`
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: {
	seed: {
		install: {}
		applications: applications: [
			{name: "incus"},
			{name: "incus-ceph"},
			{name: "incus-linstor"},
			{name: "migration-manager"},
			{name: "operations-center"},
		]
	}
	variants: default: artifact: {
		architecture: "amd64"
		format:       "raw"
	}
}
`)
	require.NoError(t, input.Err())

	value := schema.Unify(input)
	require.NoError(t, value.Validate(cue.Concrete(false)))

	var config Config
	require.NoError(t, value.Decode(&config))
	require.NotNil(t, config.Incusos)
	require.NotNil(t, config.Incusos.Seed)
	require.NotNil(t, config.Incusos.Seed.Install)
	require.NotNil(t, config.Incusos.Seed.Applications)

	assert.Equal(t, "1", config.Incusos.Seed.Install.Version)
	assert.False(t, config.Incusos.Seed.Install.ForceInstall)
	assert.False(t, config.Incusos.Seed.Install.ForceReboot)
	assert.Equal(t, "1", config.Incusos.Seed.Applications.Version)
	assert.Len(t, config.Incusos.Seed.Applications.Applications, 5)
}

func TestIncusOSSeedValidation(t *testing.T) {
	tests := []struct {
		name string
		seed string
	}{
		{
			name: "invalid application name",
			seed: `
applications: applications: [{name: "debug"}]
`,
		},
		{
			name: "invalid install target sort order",
			seed: `
install: target: sort_order: "middle"
`,
		},
		{
			name: "invalid update channel",
			seed: `
update: {
	channel:         "edge"
	check_frequency: "6h"
}
`,
		},
		{
			name: "install security degraded modes are mutually exclusive",
			seed: `
install: security: {
	missing_tpm:         true
	missing_secure_boot: true
}
`,
		},
		{
			name: "invalid incus preseed field",
			seed: `
incus: preseed: unknown_field: true
`,
		},
		{
			name: "invalid migration manager preseed field",
			seed: `
"migration-manager": preseed: unknown_field: true
`,
		},
		{
			name: "invalid operations center preseed field",
			seed: `
"operations-center": preseed: unknown_field: true
`,
		},
	}

	ctx := cuecontext.New()

	schema, err := ConfigSchema(ctx)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := ctx.CompileString(`
apiVersion: "imgcli.meigma.io/v0alpha1"
kind:       "ImagePlan"
image: name: "test-image"
incusos: {
	seed: {
` + tt.seed + `
	}
	variants: default: artifact: {
		architecture: "amd64"
		format:       "raw"
	}
}
`)
			require.NoError(t, input.Err())

			value := schema.Unify(input)
			require.Error(t, value.Validate(cue.Concrete(false)))
		})
	}
}
