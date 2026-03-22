// Package ruleresolve fetches and caches rule sets from configured sources.
// It handles GitHub references, local paths, and URL archives as described
// in the Rule Manager Spec §8.2.
package ruleresolve

import (
	"context"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Source represents a location rules can be fetched from.
type Source interface {
	// Fetch retrieves an entity file from the source at the given reference.
	Fetch(ctx context.Context, ref string) ([]*entity.File, error)
}

// Config holds options for the Resolver.
type Config struct {
	CacheTTL  string // cache time-to-live (default: "24h")
	ForceSync bool   // bypass cache
	CacheDir  string // path to cache directory
}

// Option configures the Resolver.
type Option func(*Config)

// WithCacheTTL sets the cache time-to-live.
func WithCacheTTL(ttl string) Option {
	return func(c *Config) { c.CacheTTL = ttl }
}

// WithForceSync bypasses the cache.
func WithForceSync(b bool) Option {
	return func(c *Config) { c.ForceSync = b }
}

// WithCacheDir sets the cache directory path.
func WithCacheDir(dir string) Option {
	return func(c *Config) { c.CacheDir = dir }
}

// Resolver fetches and caches rules from configured sources.
type Resolver struct {
	sources []Source
	cfg     Config
}

// New creates a Resolver with the given sources and options.
func New(sources []Source, opts ...Option) *Resolver {
	cfg := Config{CacheTTL: "24h"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Resolver{sources: sources, cfg: cfg}
}

// Resolve fetches all rules from configured sources.
func (r *Resolver) Resolve(_ context.Context) ([]*entity.File, *result.Result) {
	// TODO: implement source resolution, validation, and caching
	return nil, &result.Result{}
}
