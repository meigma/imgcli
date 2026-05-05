package incusos

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"

	"go.yaml.in/yaml/v4"

	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

var _ SeedBuilder = SeedArchiveBuilder{}

const (
	seedArchiveFileMode = 0o600
	seedSectionCount    = 8
)

var errNoSeedSections = errors.New("incusos seed has no sections")

// SeedArchiveBuilder creates IncusOS seed tar archives from provider configuration.
type SeedArchiveBuilder struct{}

// BuildSeed creates the seed archive for a provider configuration.
func (SeedArchiveBuilder) BuildSeed(ctx context.Context, config Config) (SeedArchive, error) {
	if err := ctx.Err(); err != nil {
		return SeedArchive{}, err
	}

	sections := seedSections(config.Seed)
	if len(sections) == 0 {
		return SeedArchive{}, errNoSeedSections
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)

	for _, section := range sections {
		if err := ctx.Err(); err != nil {
			return SeedArchive{}, err
		}

		data, err := yaml.Dump(section.value, yaml.V2)
		if err != nil {
			return SeedArchive{}, fmt.Errorf("encode incusos seed %s: %w", section.name, err)
		}

		header := &tar.Header{
			Name: section.name + ".yaml",
			Mode: seedArchiveFileMode,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(header); err != nil {
			return SeedArchive{}, fmt.Errorf("write incusos seed %s header: %w", section.name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return SeedArchive{}, fmt.Errorf("write incusos seed %s content: %w", section.name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return SeedArchive{}, fmt.Errorf("close incusos seed archive: %w", err)
	}

	return SeedArchive{Data: buf.Bytes()}, nil
}

type seedSection struct {
	name  string
	value any
}

func seedSections(seed *incusosschema.Seed) []seedSection {
	if seed == nil {
		return nil
	}

	sections := make([]seedSection, 0, seedSectionCount)
	if seed.Install != nil {
		sections = append(sections, seedSection{name: "install", value: seed.Install})
	}
	if seed.Applications != nil {
		sections = append(sections, seedSection{name: "applications", value: seed.Applications})
	}
	if seed.Incus != nil {
		sections = append(sections, seedSection{name: "incus", value: seed.Incus})
	}
	if seed.Network != nil {
		sections = append(sections, seedSection{name: "network", value: seed.Network})
	}
	if seed.MigrationManager != nil {
		sections = append(sections, seedSection{name: "migration-manager", value: seed.MigrationManager})
	}
	if seed.OperationsCenter != nil {
		sections = append(sections, seedSection{name: "operations-center", value: seed.OperationsCenter})
	}
	if seed.Provider != nil {
		sections = append(sections, seedSection{name: "provider", value: seed.Provider})
	}
	if seed.Update != nil {
		sections = append(sections, seedSection{name: "update", value: seed.Update})
	}

	return sections
}
