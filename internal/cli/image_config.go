package cli

import (
	"fmt"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"

	imgschemas "github.com/meigma/imgcli/schemas"
)

func loadImageConfig(path string) (imgschemas.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return imgschemas.Config{}, fmt.Errorf("read image config %q: %w", path, err)
	}

	ctx := cuecontext.New()
	input := ctx.CompileBytes(data, cue.Filename(path))
	if inputErr := input.Err(); inputErr != nil {
		return imgschemas.Config{}, fmt.Errorf("parse image config %q: %w", path, inputErr)
	}

	if providerErr := rejectUnsupportedProviderFields(input); providerErr != nil {
		return imgschemas.Config{}, providerErr
	}

	schema, err := imgschemas.ConfigSchema(ctx)
	if err != nil {
		return imgschemas.Config{}, fmt.Errorf("load image config schema: %w", err)
	}

	value := schema.Unify(input)
	if err := value.Validate(cue.Concrete(false)); err != nil {
		return imgschemas.Config{}, fmt.Errorf("validate image config %q: %w", path, err)
	}

	var config imgschemas.Config
	if err := value.Decode(&config); err != nil {
		return imgschemas.Config{}, fmt.Errorf("decode image config %q: %w", path, err)
	}

	if config.Incusos == nil {
		return imgschemas.Config{}, fmt.Errorf("image config %q must specify provider incusos", path)
	}

	return config, nil
}

func rejectUnsupportedProviderFields(value cue.Value) error {
	fields, err := value.Fields()
	if err != nil {
		return fmt.Errorf("inspect image config fields: %w", err)
	}

	for fields.Next() {
		selector := fields.Selector()
		if selector.LabelType() != cue.StringLabel {
			return fmt.Errorf("unsupported image config selector %q", selector)
		}

		label := selector.Unquoted()
		if isAllowedImageConfigField(label) {
			continue
		}

		return fmt.Errorf("unsupported provider %q: only incusos is supported", label)
	}

	return nil
}

func isAllowedImageConfigField(label string) bool {
	switch label {
	case "apiVersion", "kind", "image", "output", "publish", "incusos":
		return true
	default:
		return false
	}
}
