package incusos

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	incusosapi "github.com/lxc/incus-os/incus-osd/api"
	apiseed "github.com/lxc/incus-os/incus-osd/api/seed"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.yaml.in/yaml/v4"

	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

func TestSeedArchiveBuilderRequiresSeedSections(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "nil seed",
			config: Config{},
		},
		{
			name: "empty seed",
			config: Config{
				Seed: &incusosschema.Seed{},
			},
		},
	}

	builder := SeedArchiveBuilder{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := builder.BuildSeed(context.Background(), tt.config)

			require.ErrorIs(t, err, errNoSeedSections)
		})
	}
}

func TestSeedArchiveBuilderReturnsContextError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := SeedArchiveBuilder{}.BuildSeed(ctx, Config{
		Seed: &incusosschema.Seed{
			Install: &apiseed.Install{Version: "1"},
		},
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestSeedArchiveBuilderWritesSeedSectionsInOrder(t *testing.T) {
	builder := SeedArchiveBuilder{}
	archive, err := builder.BuildSeed(context.Background(), Config{
		Seed: &incusosschema.Seed{
			Install:          &apiseed.Install{Version: "1", ForceReboot: true},
			Applications:     &apiseed.Applications{Version: "1", Applications: []apiseed.Application{{Name: "incus"}}},
			Incus:            &apiseed.Incus{Version: "1", ApplyDefaults: true},
			Network:          &apiseed.Network{Version: "1"},
			MigrationManager: &apiseed.MigrationManager{Version: "1"},
			OperationsCenter: &apiseed.OperationsCenter{Version: "1"},
			Provider: &apiseed.Provider{
				SystemProviderConfig: incusosapi.SystemProviderConfig{Name: "images"},
				Version:              "1",
			},
			Update: &apiseed.Update{
				SystemUpdateConfig: incusosapi.SystemUpdateConfig{
					Channel:        "stable",
					CheckFrequency: "6h",
				},
				Version: "1",
			},
		},
	})

	require.NoError(t, err)
	entries := readSeedArchive(t, archive)

	assert.Equal(t, []string{
		"install.yaml",
		"applications.yaml",
		"incus.yaml",
		"network.yaml",
		"migration-manager.yaml",
		"operations-center.yaml",
		"provider.yaml",
		"update.yaml",
	}, entries.names)
	for _, mode := range entries.modes {
		assert.Equal(t, int64(0o600), mode)
	}

	var install apiseed.Install
	require.NoError(t, yaml.Load(entries.contents["install.yaml"], &install))
	assert.Equal(t, "1", install.Version)
	assert.True(t, install.ForceReboot)

	var applications apiseed.Applications
	require.NoError(t, yaml.Load(entries.contents["applications.yaml"], &applications))
	require.Len(t, applications.Applications, 1)
	assert.Equal(t, "incus", applications.Applications[0].Name)
}

type seedArchiveEntries struct {
	names    []string
	modes    map[string]int64
	contents map[string][]byte
}

func readSeedArchive(t *testing.T, archive SeedArchive) seedArchiveEntries {
	t.Helper()

	tr := tar.NewReader(bytes.NewReader(archive.Data))
	entries := seedArchiveEntries{
		modes:    map[string]int64{},
		contents: map[string][]byte{},
	}

	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		content, err := io.ReadAll(tr)
		require.NoError(t, err)

		entries.names = append(entries.names, header.Name)
		entries.modes[header.Name] = header.Mode
		entries.contents[header.Name] = content
	}

	return entries
}
