package schemas

import (
	"bytes"
	"io/fs"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
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
