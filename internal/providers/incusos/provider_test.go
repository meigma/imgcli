package incusos

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/meigma/imgcli/internal/providers"
	"github.com/meigma/imgcli/schemas/core"
	incusosschema "github.com/meigma/imgcli/schemas/providers/incusos"
)

func TestProviderName(t *testing.T) {
	provider := New(Config{}, Options{})

	assert.Equal(t, core.ProviderName("incusos"), provider.Name())
}

func TestProviderPlanPlaceholderOperation(t *testing.T) {
	provider := New(Config{}, Options{})

	plan, err := provider.Plan(context.Background(), providers.PlanRequest{})
	require.ErrorIs(t, err, ErrNotImplemented)
	assert.Empty(t, plan)
}

func TestProviderBuildResolvesImageURL(t *testing.T) {
	tests := []struct {
		name      string
		config    Config
		wantQuery ImageQuery
	}{
		{
			name: "uses stable by default",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {
						Artifact: core.ArtifactIntent{
							Architecture: core.Architecture("amd64"),
							Format:       core.ArtifactFormat("raw"),
						},
					},
				},
			},
			wantQuery: ImageQuery{
				Channel:      ChannelStable,
				Architecture: core.Architecture("amd64"),
				Type:         ImageTypeRaw,
			},
		},
		{
			name: "uses variant source over defaults",
			config: Config{
				Defaults: &incusosschema.Defaults{
					Source: &incusosschema.Source{
						Channel: ChannelStable,
						Version: Version("202604202240"),
					},
				},
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {
						Source: &incusosschema.Source{
							Channel: ChannelTesting,
							Version: Version("202604282312"),
						},
						Artifact: core.ArtifactIntent{
							Architecture: core.Architecture("arm64"),
							Format:       core.ArtifactFormat("iso"),
						},
					},
				},
			},
			wantQuery: ImageQuery{
				Channel:      ChannelTesting,
				Version:      Version("202604282312"),
				Architecture: core.Architecture("arm64"),
				Type:         ImageTypeISO,
			},
		},
		{
			name: "maps raw gzip artifact to raw image",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {
						Artifact: core.ArtifactIntent{
							Architecture: core.Architecture("amd64"),
							Format:       core.ArtifactFormat("raw.gz"),
						},
					},
				},
			},
			wantQuery: ImageQuery{
				Channel:      ChannelStable,
				Architecture: core.Architecture("amd64"),
				Type:         ImageTypeRaw,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const assetURL = "https://example.invalid/incusos.img.gz"

			catalog := &recordingCatalog{
				asset: ImageAsset{URL: assetURL},
			}
			var output bytes.Buffer
			provider := New(tt.config, Options{
				Catalog: catalog,
				Output:  &output,
			})

			result, err := provider.Build(context.Background(), providers.BuildRequest{})

			require.NoError(t, err)
			assert.Empty(t, result)
			require.Len(t, catalog.queries, 1)
			assert.Equal(t, tt.wantQuery, catalog.queries[0])
			assert.Equal(t, assetURL+"\n", output.String())
		})
	}
}

func TestProviderBuildErrors(t *testing.T) {
	catalogErr := errors.New("catalog failed")

	tests := []struct {
		name       string
		config     Config
		catalog    *recordingCatalog
		wantErr    string
		wantErrIs  error
		wantOutput string
	}{
		{
			name:    "missing catalog",
			config:  configWithVariant(core.ArtifactFormat("raw")),
			wantErr: "incusos catalog is required",
		},
		{
			name: "zero variants",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{},
			},
			catalog: &recordingCatalog{},
			wantErr: "incusos build requires exactly one variant, got 0",
		},
		{
			name: "multiple variants",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {},
					"other":   {},
				},
			},
			catalog: &recordingCatalog{},
			wantErr: "incusos build requires exactly one variant, got 2",
		},
		{
			name:    "unsupported format",
			config:  configWithVariant(core.ArtifactFormat("qcow2")),
			catalog: &recordingCatalog{},
			wantErr: `unsupported incusos artifact format "qcow2"`,
		},
		{
			name: "catalog error",
			config: Config{
				Variants: map[core.VariantName]incusosschema.Variant{
					"default": {
						Artifact: core.ArtifactIntent{
							Architecture: core.Architecture("amd64"),
							Format:       core.ArtifactFormat("raw"),
						},
					},
				},
			},
			catalog:   &recordingCatalog{err: catalogErr},
			wantErrIs: catalogErr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var output bytes.Buffer
			options := Options{Output: &output}
			if tt.catalog != nil {
				options.Catalog = tt.catalog
			}
			provider := New(tt.config, options)

			result, err := provider.Build(context.Background(), providers.BuildRequest{})

			require.Error(t, err)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			}
			if tt.wantErrIs != nil {
				require.ErrorIs(t, err, tt.wantErrIs)
			}
			assert.Empty(t, result)
			assert.Equal(t, tt.wantOutput, output.String())
		})
	}
}

func configWithVariant(format core.ArtifactFormat) Config {
	return Config{
		Variants: map[core.VariantName]incusosschema.Variant{
			"default": {
				Artifact: core.ArtifactIntent{
					Architecture: core.Architecture("amd64"),
					Format:       format,
				},
			},
		},
	}
}

type recordingCatalog struct {
	asset   ImageAsset
	err     error
	queries []ImageQuery
}

func (c *recordingCatalog) ResolveImage(_ context.Context, query ImageQuery) (ImageAsset, error) {
	c.queries = append(c.queries, query)
	if c.err != nil {
		return ImageAsset{}, c.err
	}

	return c.asset, nil
}
