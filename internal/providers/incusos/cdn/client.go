package cdn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/meigma/imgcli/internal/providers/incusos"
	"github.com/meigma/imgcli/schemas/core"
)

const (
	defaultBaseURL = "https://images.linuxcontainers.org/os/"
	indexPath      = "index.json"
	maxIndexBytes  = 8 << 20
)

var (
	_ incusos.Catalog    = (*Client)(nil)
	_ incusos.Downloader = (*Client)(nil)
)

// Option configures a CDN client.
type Option func(*Client)

// Client resolves and downloads IncusOS images from the Linux Containers CDN.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// WithBaseURL configures the CDN base URL containing index.json.
func WithBaseURL(baseURL string) Option {
	return func(client *Client) {
		client.baseURL = baseURL
	}
}

// WithHTTPClient configures the HTTP client used for CDN requests.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		if httpClient != nil {
			client.httpClient = httpClient
		}
	}
}

// NewClient constructs a CDN client.
func NewClient(options ...Option) *Client {
	client := &Client{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
	}

	for _, option := range options {
		option(client)
	}

	return client
}

// ResolveImage selects an IncusOS source image asset for the query.
func (c *Client) ResolveImage(ctx context.Context, query incusos.ImageQuery) (incusos.ImageAsset, error) {
	normalized, err := normalizeQuery(query)
	if err != nil {
		return incusos.ImageAsset{}, err
	}

	index, err := c.fetchIndex(ctx)
	if err != nil {
		return incusos.ImageAsset{}, err
	}

	update, ok := selectUpdate(index.Updates, normalized.Channel, normalized.Version)
	if !ok {
		return incusos.ImageAsset{}, fmt.Errorf(
			"%w: channel=%q version=%q",
			incusos.ErrImageNotFound,
			normalized.Channel,
			normalized.Version,
		)
	}

	fileType, err := cdnFileType(normalized.Type)
	if err != nil {
		return incusos.ImageAsset{}, err
	}

	cdnArch, err := cdnArchitecture(normalized.Architecture)
	if err != nil {
		return incusos.ImageAsset{}, err
	}

	for _, file := range update.Files {
		if file.Component != "os" || file.Type != fileType || file.Architecture != cdnArch {
			continue
		}

		assetURL, err := c.assetURL(update, file)
		if err != nil {
			return incusos.ImageAsset{}, err
		}

		return incusos.ImageAsset{
			Version:      update.Version,
			Architecture: normalized.Architecture,
			Type:         normalized.Type,
			URL:          assetURL,
			SHA256:       file.SHA256,
			Size:         file.Size,
		}, nil
	}

	return incusos.ImageAsset{}, fmt.Errorf(
		"%w: channel=%q version=%q architecture=%q type=%q",
		incusos.ErrImageNotFound,
		normalized.Channel,
		update.Version,
		normalized.Architecture,
		normalized.Type,
	)
}

// DownloadImage downloads and verifies the provided image asset.
func (c *Client) DownloadImage(_ context.Context, _ incusos.ImageAsset, _ string) (incusos.DownloadedImage, error) {
	return incusos.DownloadedImage{}, incusos.ErrNotImplemented
}

func (c *Client) fetchIndex(ctx context.Context) (catalogIndex, error) {
	indexURL, err := url.JoinPath(c.baseURLOrDefault(), indexPath)
	if err != nil {
		return catalogIndex{}, fmt.Errorf("build incusos catalog index URL: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, indexURL, nil)
	if err != nil {
		return catalogIndex{}, fmt.Errorf("create incusos catalog index request: %w", err)
	}

	resp, err := c.httpClientOrDefault().Do(req)
	if err != nil {
		return catalogIndex{}, fmt.Errorf("fetch incusos catalog index: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return catalogIndex{}, fmt.Errorf("fetch incusos catalog index: unexpected HTTP status %s", resp.Status)
	}

	var index catalogIndex
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxIndexBytes))
	if err := decoder.Decode(&index); err != nil {
		return catalogIndex{}, fmt.Errorf("decode incusos catalog index: %w", err)
	}

	return index, nil
}

func (c *Client) assetURL(update catalogUpdate, file catalogFile) (string, error) {
	updatePath := strings.Trim(update.URL, "/")
	if updatePath == "" {
		updatePath = string(update.Version)
	}

	filename := strings.Trim(file.Filename, "/")
	assetURL, err := url.JoinPath(c.baseURLOrDefault(), updatePath, filename)
	if err != nil {
		return "", fmt.Errorf("build incusos asset URL: %w", err)
	}

	return assetURL, nil
}

func (c *Client) baseURLOrDefault() string {
	if c.baseURL == "" {
		return defaultBaseURL
	}

	return c.baseURL
}

func (c *Client) httpClientOrDefault() *http.Client {
	if c.httpClient == nil {
		return http.DefaultClient
	}

	return c.httpClient
}

func normalizeQuery(query incusos.ImageQuery) (incusos.ImageQuery, error) {
	if query.Channel == "" {
		query.Channel = incusos.ChannelStable
	}

	if _, err := cdnArchitecture(query.Architecture); err != nil {
		return incusos.ImageQuery{}, err
	}

	if _, err := cdnFileType(query.Type); err != nil {
		return incusos.ImageQuery{}, err
	}

	return query, nil
}

func selectUpdate(updates []catalogUpdate, channel incusos.Channel, version incusos.Version) (catalogUpdate, bool) {
	var selected catalogUpdate
	for _, update := range updates {
		if version != "" && update.Version != version {
			continue
		}

		if !hasChannel(update.Channels, channel) {
			continue
		}

		if version != "" {
			return update, true
		}

		if selected.Version == "" || update.Version > selected.Version {
			selected = update
		}
	}

	if selected.Version == "" {
		return catalogUpdate{}, false
	}

	return selected, true
}

func hasChannel(channels []incusos.Channel, channel incusos.Channel) bool {
	return slices.Contains(channels, channel)
}

func cdnArchitecture(architecture core.Architecture) (string, error) {
	switch architecture {
	case "amd64":
		return "x86_64", nil
	case "arm64":
		return "aarch64", nil
	default:
		return "", fmt.Errorf("unsupported incusos architecture %q", architecture)
	}
}

func cdnFileType(imageType incusos.ImageType) (string, error) {
	switch imageType {
	case incusos.ImageTypeRaw:
		return "image-raw", nil
	case incusos.ImageTypeISO:
		return "image-iso", nil
	default:
		return "", fmt.Errorf("unsupported incusos image type %q", imageType)
	}
}

type catalogIndex struct {
	Updates []catalogUpdate `json:"updates"`
}

type catalogUpdate struct {
	Channels []incusos.Channel `json:"channels"`
	Files    []catalogFile     `json:"files"`
	Version  incusos.Version   `json:"version"`
	URL      string            `json:"url"`
}

type catalogFile struct {
	Architecture string `json:"architecture"`
	Component    string `json:"component"`
	Filename     string `json:"filename"`
	SHA256       string `json:"sha256"`
	Size         int64  `json:"size"`
	Type         string `json:"type"`
}
