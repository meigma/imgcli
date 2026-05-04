package schemas

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/load"
)

const (
	// ModulePath is the CUE module path embedded in this Go package.
	ModulePath = "github.com/meigma/imgcli/schemas@v0"

	embeddedModuleDir = "imgcli-schemas-embedded"
)

//go:embed cue.mod/module.cue *.cue core/*.cue providers/*/*.cue
var embeddedModule embed.FS

// ModuleFS returns the embedded CUE module files.
func ModuleFS() fs.FS {
	return embeddedModule
}

// LoadSchema loads the embedded CUE module into ctx and returns the root package value.
func LoadSchema(ctx *cue.Context) (cue.Value, error) {
	if ctx == nil {
		return cue.Value{}, errors.New("schemas: nil CUE context")
	}

	overlay, err := embeddedOverlay()
	if err != nil {
		return cue.Value{}, err
	}

	moduleRoot := embeddedModuleRoot()
	insts := load.Instances([]string{"."}, &load.Config{
		Dir:        moduleRoot,
		ModuleRoot: moduleRoot,
		Module:     ModulePath,
		Overlay:    overlay,
		Env:        []string{},
	})
	if len(insts) != 1 {
		return cue.Value{}, fmt.Errorf("schemas: expected one CUE instance, got %d", len(insts))
	}
	if err := insts[0].Err; err != nil {
		return cue.Value{}, fmt.Errorf("schemas: load embedded CUE module: %w", err)
	}

	value := ctx.BuildInstance(insts[0])
	if err := value.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("schemas: build embedded CUE module: %w", err)
	}
	return value, nil
}

// ConfigSchema returns the embedded schemas.#Config definition.
func ConfigSchema(ctx *cue.Context) (cue.Value, error) {
	value, err := LoadSchema(ctx)
	if err != nil {
		return cue.Value{}, err
	}

	config := value.LookupPath(cue.ParsePath("#Config"))
	if err := config.Err(); err != nil {
		return cue.Value{}, fmt.Errorf("schemas: lookup #Config: %w", err)
	}
	return config, nil
}

func embeddedOverlay() (map[string]load.Source, error) {
	moduleRoot := embeddedModuleRoot()
	overlay := map[string]load.Source{}

	err := fs.WalkDir(embeddedModule, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".cue") {
			return nil
		}

		data, err := fs.ReadFile(embeddedModule, path)
		if err != nil {
			return err
		}
		overlay[filepath.Join(moduleRoot, filepath.FromSlash(path))] = load.FromBytes(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("schemas: read embedded CUE module: %w", err)
	}
	return overlay, nil
}

func embeddedModuleRoot() string {
	return filepath.Join(os.TempDir(), embeddedModuleDir)
}
