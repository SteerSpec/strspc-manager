// Package realmresolve resolves Realm dependencies declared in realm.json.
// It fetches remote Realms (e.g. github:// URIs), resolves local paths,
// caches results, and detects EUID collisions across dependent Realms.
package realmresolve

import (
	"context"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Config holds options for the RealmResolver.
type Config struct {
	CacheTTL time.Duration // cache time-to-live (default: 24h)
	CacheDir string        // path to cache directory
}

// Option configures the RealmResolver.
type Option func(*Config)

// WithCacheTTL sets the cache time-to-live.
func WithCacheTTL(ttl time.Duration) Option {
	return func(c *Config) { c.CacheTTL = ttl }
}

// WithCacheDir sets the cache directory path.
func WithCacheDir(dir string) Option {
	return func(c *Config) { c.CacheDir = dir }
}

// RealmResolver resolves Realm dependencies.
type RealmResolver struct {
	cfg Config
}

// New creates a RealmResolver with the given options.
func New(opts ...Option) *RealmResolver {
	cfg := Config{CacheTTL: 24 * time.Hour}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &RealmResolver{cfg: cfg}
}

// Resolve fetches and resolves all dependencies declared in a RealmFile.
func (r *RealmResolver) Resolve(_ context.Context, _ *entity.RealmFile) ([]*entity.RealmFile, *result.Result) {
	// TODO: implement dependency resolution
	// - Parse github:// URIs
	// - Resolve relative paths
	// - Fetch and cache remote Realms
	// - Detect EUID collisions
	// - Handle transitive dependencies
	return nil, &result.Result{}
}
