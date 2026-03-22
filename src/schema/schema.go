// Package schema fetches and caches JSON schemas from steerspec.dev.
// Schemas are the source of truth for entity and Realm validation.
// They are published at https://steerspec.dev/schemas/ and cached locally
// to avoid repeated network calls.
package schema

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// BaseURL is the default base URL for schema fetches.
const BaseURL = "https://steerspec.dev/schemas"

// maxSchemaSize is the maximum allowed schema size (1 MB).
const maxSchemaSize = 1 << 20

// defaultTimeout is the HTTP client timeout when no custom client is provided.
const defaultTimeout = 30 * time.Second

// Well-known schema paths relative to BaseURL.
const (
	EntityV1Path  = "entity/v1.json"
	RealmV1Path   = "realm/v1.json"
	BootstrapPath = "entity/bootstrap.json"
)

// Config holds options for the Fetcher.
type Config struct {
	CacheDir string // local cache directory (default: os.UserCacheDir()/strspc/schemas)
	BaseURL  string // base URL for schema fetches (default: BaseURL)
	Client   *http.Client
}

// Option configures the Fetcher.
type Option func(*Config)

// WithCacheDir sets the local cache directory.
func WithCacheDir(dir string) Option {
	return func(c *Config) { c.CacheDir = dir }
}

// WithBaseURL overrides the base URL (useful for testing).
func WithBaseURL(url string) Option {
	return func(c *Config) { c.BaseURL = url }
}

// WithClient sets a custom HTTP client.
func WithClient(client *http.Client) Option {
	return func(c *Config) { c.Client = client }
}

// Fetcher retrieves and caches JSON schemas from steerspec.dev.
type Fetcher struct {
	cfg Config
}

// New creates a Fetcher with the given options.
func New(opts ...Option) *Fetcher {
	cfg := Config{
		BaseURL: BaseURL,
		Client:  &http.Client{Timeout: defaultTimeout},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.CacheDir == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			dir = os.TempDir()
		}
		cfg.CacheDir = filepath.Join(dir, "strspc", "schemas")
	}
	return &Fetcher{cfg: cfg}
}

// EntityV1 fetches the entity/v1.json schema.
func (f *Fetcher) EntityV1(ctx context.Context) ([]byte, error) {
	return f.Fetch(ctx, EntityV1Path)
}

// RealmV1 fetches the realm/v1.json schema.
func (f *Fetcher) RealmV1(ctx context.Context) ([]byte, error) {
	return f.Fetch(ctx, RealmV1Path)
}

// Bootstrap fetches the entity/bootstrap.json schema.
func (f *Fetcher) Bootstrap(ctx context.Context) ([]byte, error) {
	return f.Fetch(ctx, BootstrapPath)
}

// Fetch retrieves a schema by path (relative to BaseURL). It serves from
// the local file cache if available, otherwise fetches via HTTP and caches.
func (f *Fetcher) Fetch(ctx context.Context, path string) ([]byte, error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return nil, fmt.Errorf("invalid schema path: %q", path)
	}

	// Try cache first.
	if data, err := f.readCache(clean); err == nil {
		return data, nil
	}

	// Fetch from remote.
	url := f.cfg.BaseURL + "/" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}

	resp, err := f.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching schema %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching schema %s: HTTP %d", url, resp.StatusCode)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSchemaSize))
	if err != nil {
		return nil, fmt.Errorf("reading schema %s: %w", url, err)
	}

	// Cache for next time (best-effort, don't fail if caching fails).
	_ = f.writeCache(clean, data)

	return data, nil
}

func (f *Fetcher) cachePath(clean string) string {
	return filepath.Join(f.cfg.CacheDir, clean)
}

func (f *Fetcher) readCache(clean string) ([]byte, error) {
	return os.ReadFile(f.cachePath(clean))
}

func (f *Fetcher) writeCache(clean string, data []byte) error {
	cp := f.cachePath(clean)
	if err := os.MkdirAll(filepath.Dir(cp), 0o755); err != nil {
		return err
	}
	return os.WriteFile(cp, data, 0o644)
}
