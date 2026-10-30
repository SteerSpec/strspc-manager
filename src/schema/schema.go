// Package schema fetches and caches JSON schemas from steerspec.dev.
// Schemas are the source of truth for entity and Realm validation.
// The latest are published at https://steerspec.dev/schemas/ and those of each
// core release at https://steerspec.dev/rules/vX.Y.Z/_schema/. A realm is
// validated against its own _schema/, then the schemas of the core version it
// pins (see ForRealm), so a schema change reaches it through its pin. Published
// schemas are cached locally to avoid repeated network calls: a release's
// schemas never change, so its copy is kept for good, while a latest copy
// expires after latestTTL.
package schema

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// BaseURL is the default base URL for schema fetches.
const BaseURL = "https://steerspec.dev/schemas"

// maxSchemaSize is the maximum allowed schema size (1 MB).
const maxSchemaSize int64 = 1 << 20

// defaultTimeout is the HTTP client timeout when no custom client is provided.
const defaultTimeout = 30 * time.Second

// latestTTL is how long a cached copy of a latest schema is used before it is
// fetched again.
const latestTTL = time.Hour

// CoreRealmID identifies the realm that defines the schemas.
const CoreRealmID = "dev.steerspec.core"

// errNotFound marks a schema the server does not have.
var errNotFound = errors.New("not found")

// releaseVersion matches the versions core is released under. A version comes
// from realm.json and ends up in a URL and a cache path.
var releaseVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Well-known schema paths relative to BaseURL.
const (
	// EntityV1Path is the path of the entity.v1 schema below BaseURL.
	EntityV1Path = "entity/v1.json"
	// RealmV1Path is the path of the realm.v1 schema below BaseURL.
	RealmV1Path = "realm/v1.json"
	// BootstrapPath is the path of the minimal structural entity schema (the
	// bootstrap axiom) below BaseURL.
	BootstrapPath = "entity/bootstrap.json"
)

// Config holds options for the Fetcher.
type Config struct {
	// CacheDir is the local cache directory (default:
	// os.UserCacheDir()/strspc/schemas).
	CacheDir string
	// BaseURL is the base URL for schema fetches (default: BaseURL).
	BaseURL string
	// Client is the HTTP client used for fetches; nil selects one with a
	// 30-second timeout.
	Client *http.Client
	// CoreVersion selects the schemas of a core release (X.Y.Z). Empty uses
	// the latest schemas.
	CoreVersion string
	// LocalDir is a realm directory whose _schema/ takes precedence.
	LocalDir string
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

// WithClient sets a custom HTTP client. Nil values are ignored.
func WithClient(client *http.Client) Option {
	return func(c *Config) {
		if client != nil {
			c.Client = client
		}
	}
}

// WithCoreVersion selects the schemas of a core release. Anything but an
// X.Y.Z version is ignored, leaving the latest schemas in use.
func WithCoreVersion(version string) Option {
	return func(c *Config) {
		if releaseVersion.MatchString(version) {
			c.CoreVersion = version
		}
	}
}

// WithLocalDir prefers the schemas in dir/_schema over any published ones.
func WithLocalDir(dir string) Option {
	return func(c *Config) { c.LocalDir = dir }
}

// ForRealm returns the options selecting the schemas for the realm in dir: its
// own _schema/, which every realm carries (RM003, RM004), then those of the
// core version it depends on, then the latest. RM015 reports drift between
// _schema/ and the pin, only where the linter has a dependency fetcher. It
// returns none when dir has no readable realm.json, leaving the latest schemas
// in use; reporting a broken manifest is the linters' job.
func ForRealm(dir string) []Option {
	data, err := os.ReadFile(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	var manifest struct {
		Dependencies []struct {
			RealmID string `json:"realm_id"`
			Version string `json:"version"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil
	}
	opts := []Option{WithLocalDir(dir)}
	for _, dep := range manifest.Dependencies {
		if dep.RealmID == CoreRealmID {
			return append(opts, WithCoreVersion(dep.Version))
		}
	}
	return opts
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
	// Ensure Client is non-nil even if a caller passed nil via WithClient
	// after the default was set (e.g. via a custom Option).
	if cfg.Client == nil {
		cfg.Client = &http.Client{Timeout: defaultTimeout}
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

// Fetch retrieves a schema by path (relative to BaseURL). A local schema wins,
// then the pinned core release, then the latest schema; a release without that
// schema falls through to latest, any other failure is returned.
func (f *Fetcher) Fetch(ctx context.Context, schemaPath string) ([]byte, error) {
	// Reject backslashes to prevent Windows path traversal bypasses.
	if strings.ContainsRune(schemaPath, '\\') {
		return nil, fmt.Errorf("invalid schema path: %q", schemaPath)
	}

	clean := path.Clean(schemaPath)
	if clean == "." || strings.HasPrefix(clean, "..") || path.IsAbs(clean) {
		return nil, fmt.Errorf("invalid schema path: %q", schemaPath)
	}

	// Verify the resolved cache path stays within CacheDir, resolving
	// symlinks where possible so the check is not purely lexical.
	base := filepath.Clean(f.cfg.CacheDir)
	if phys, err := filepath.EvalSymlinks(base); err == nil {
		base = phys
	}
	resolved := filepath.Clean(filepath.Join(base, filepath.FromSlash(clean)))
	if parent := filepath.Dir(resolved); parent != base {
		if phys, err := filepath.EvalSymlinks(parent); err == nil {
			resolved = filepath.Join(phys, filepath.Base(resolved))
		}
	}
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("invalid schema path: %q", schemaPath)
	}

	if f.cfg.LocalDir != "" {
		local := filepath.Join(f.cfg.LocalDir, "_schema", filepath.FromSlash(releaseName(clean)))
		if data, err := readSchemaFile(local); err == nil {
			return data, nil
		}
	}
	if f.cfg.CoreVersion != "" {
		data, err := f.fetchRelease(ctx, clean)
		if !errors.Is(err, errNotFound) {
			return data, err
		}
	}
	return f.fetchLatest(ctx, clean)
}

// fetchRelease retrieves a schema of the pinned core release. Releases are
// immutable, so a cached copy is always valid.
func (f *Fetcher) fetchRelease(ctx context.Context, clean string) ([]byte, error) {
	key := path.Join("versions", f.cfg.CoreVersion, clean)
	if data, err := f.readCache(key); err == nil {
		return data, nil
	}

	site := strings.TrimSuffix(strings.TrimRight(f.cfg.BaseURL, "/"), "/schemas")
	data, err := f.download(ctx, site+"/rules/v"+f.cfg.CoreVersion+"/_schema/"+releaseName(clean))
	if err != nil {
		return nil, err
	}

	// Cache for next time (best-effort, don't fail if caching fails).
	_ = f.writeCache(key, data)

	return data, nil
}

// fetchLatest retrieves a latest schema, refreshing a cached copy older than
// latestTTL. An expired copy still serves when the refresh fails, so validation
// keeps working offline.
func (f *Fetcher) fetchLatest(ctx context.Context, clean string) ([]byte, error) {
	cached, cacheErr := f.readCache(clean)
	if cacheErr == nil && f.fresh(clean) {
		return cached, nil
	}

	// Fetch from remote using the canonicalized path.
	data, err := f.download(ctx, strings.TrimRight(f.cfg.BaseURL, "/")+"/"+clean)
	if err != nil {
		if cacheErr == nil {
			return cached, nil
		}
		return nil, err
	}

	// Cache for next time (best-effort, don't fail if caching fails).
	_ = f.writeCache(clean, data)

	return data, nil
}

func (f *Fetcher) download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request for %s: %w", url, err)
	}

	resp, err := f.cfg.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching schema %s: %w", url, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("fetching schema %s: HTTP %d: %w", url, resp.StatusCode, errNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetching schema %s: HTTP %d", url, resp.StatusCode)
	}

	// Read up to maxSchemaSize+1 to detect oversized responses.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxSchemaSize+1))
	if err != nil {
		return nil, fmt.Errorf("reading schema %s: %w", url, err)
	}
	if int64(len(data)) > maxSchemaSize {
		return nil, fmt.Errorf("schema %s exceeds max allowed size of %d bytes", url, maxSchemaSize)
	}
	return data, nil
}

// releaseName maps a path below BaseURL to its file name in _schema/:
// entity/v1.json is entity.v1.schema.json, entity/bootstrap.json is
// bootstrap.schema.json.
func releaseName(clean string) string {
	dir, file := path.Split(clean)
	stem := strings.TrimSuffix(file, ".json")
	if stem == "bootstrap" {
		return "bootstrap.schema.json"
	}
	return strings.TrimSuffix(dir, "/") + "." + stem + ".schema.json"
}

func (f *Fetcher) localPath(clean string) string {
	return filepath.Join(f.cfg.CacheDir, filepath.FromSlash(clean))
}

func (f *Fetcher) readCache(clean string) ([]byte, error) {
	return readSchemaFile(f.localPath(clean))
}

func (f *Fetcher) fresh(clean string) bool {
	fi, err := os.Lstat(f.localPath(clean))
	return err == nil && time.Since(fi.ModTime()) < latestTTL
}

func readSchemaFile(cp string) ([]byte, error) {
	fi, err := os.Lstat(cp)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("cached schema is not a regular file: %s", cp)
	}
	if fi.Size() > maxSchemaSize {
		return nil, fmt.Errorf("cached schema too large: %d bytes", fi.Size())
	}

	return os.ReadFile(cp)
}

func (f *Fetcher) writeCache(clean string, data []byte) error {
	cp := f.localPath(clean)
	dir := filepath.Dir(cp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	// Write to a temp file then atomically rename to avoid partial reads.
	tmp, err := os.CreateTemp(dir, ".schema-cache-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	// On Windows, os.Rename fails if the destination already exists.
	// Try a direct rename first, then remove and retry on failure.
	if err := os.Rename(tmpName, cp); err != nil {
		if removeErr := os.Remove(cp); removeErr != nil && !os.IsNotExist(removeErr) {
			_ = os.Remove(tmpName)
			return err
		}
		if err2 := os.Rename(tmpName, cp); err2 != nil {
			_ = os.Remove(tmpName)
			return err2
		}
	}
	return nil
}
