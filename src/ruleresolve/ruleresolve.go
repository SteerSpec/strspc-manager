// Package ruleresolve fetches and caches rule sets from configured sources.
// It handles GitHub references, local paths, and URL archives as described
// in the Rule Manager Spec §8.2.
package ruleresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "rule-resolve"

// Scope represents the applicability of a rule source.
type Scope string

// Rule source scope values.
const (
	ScopeGlobal Scope = "global"
	ScopeLocal  Scope = "local"
)

// SourceEntry represents a single rule source from config.yaml.
type SourceEntry struct {
	Source string // URI: "./rules/", "github://...", etc.
	Scope  Scope
}

// ResolvedFile wraps an entity.File with metadata from resolution.
type ResolvedFile struct {
	File           *entity.File
	Origin         SourceEntry
	Path           string // filesystem path (for diagnostics)
	ResolvedSource string // absolute path or normalized URI of the source
}

// SourceFile pairs a parsed entity file with the path it was loaded from.
type SourceFile struct {
	File *entity.File
	Path string // filesystem path or URL
}

// Source represents a location rules can be fetched from.
type Source interface {
	// Fetch retrieves entity files from the source at the given reference.
	Fetch(ctx context.Context, ref string) ([]SourceFile, *result.Result)
}

// sourceBinding pairs a concrete Source with the SourceEntry it was parsed from.
type sourceBinding struct {
	source Source
	entry  SourceEntry
}

// Config holds options for the Resolver.
type Config struct {
	CacheTTL  time.Duration // cache time-to-live (default: 24h)
	ForceSync bool          // bypass cache
	CacheDir  string        // path to cache directory
	BaseDir   string        // base directory for resolving relative source paths
}

// Option configures the Resolver.
type Option func(*Config)

// WithCacheTTL sets the cache time-to-live.
func WithCacheTTL(ttl time.Duration) Option {
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

// WithBaseDir sets the base directory for resolving relative source paths.
// Defaults to the current working directory.
func WithBaseDir(dir string) Option {
	return func(c *Config) { c.BaseDir = dir }
}

// Resolver fetches and caches rules from configured sources.
type Resolver struct {
	bindings []sourceBinding
	cfg      Config
}

// New creates a Resolver from the given source entries and options.
// It returns an error if any entry uses an unsupported URI scheme.
func New(entries []SourceEntry, opts ...Option) (*Resolver, error) {
	cfg := Config{CacheTTL: 24 * time.Hour}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.BaseDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolving base directory: %w", err)
		}
		cfg.BaseDir = wd
	}

	bindings := make([]sourceBinding, 0, len(entries))
	for _, e := range entries {
		src, err := parseSource(e)
		if err != nil {
			return nil, err
		}
		bindings = append(bindings, sourceBinding{source: src, entry: e})
	}

	return &Resolver{bindings: bindings, cfg: cfg}, nil
}

// Resolve fetches all rules from configured sources and checks for
// EUID collisions across sources.
func (r *Resolver) Resolve(ctx context.Context) ([]*ResolvedFile, *result.Result) {
	res := &result.Result{}

	if err := ctx.Err(); err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV000",
			Severity: result.Error,
			Message:  err.Error(),
		})
		return nil, res
	}

	var all []*ResolvedFile
	for _, b := range r.bindings {
		if err := ctx.Err(); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV000",
				Severity: result.Error,
				Message:  err.Error(),
			})
			return nil, res
		}

		ref := b.entry.Source
		// Resolve relative paths against BaseDir.
		if !filepath.IsAbs(ref) && !strings.Contains(ref, "://") {
			ref = filepath.Join(r.cfg.BaseDir, ref)
		}
		// Canonicalize for consistent collision comparison.
		if absRef, err := filepath.Abs(ref); err == nil {
			ref = filepath.Clean(absRef)
		}

		files, fetchRes := b.source.Fetch(ctx, ref)
		if fetchRes != nil {
			res.Diagnostics = append(res.Diagnostics, fetchRes.Diagnostics...)
		}
		for _, sf := range files {
			all = append(all, &ResolvedFile{
				File:           sf.File,
				Origin:         b.entry,
				Path:           sf.Path,
				ResolvedSource: ref,
			})
		}
	}

	checkCollisions(all, res)
	return all, res
}

// parseSource dispatches a SourceEntry to a concrete Source implementation.
func parseSource(e SourceEntry) (Source, error) {
	s := e.Source
	switch {
	case strings.HasPrefix(s, "github://"):
		return nil, fmt.Errorf("github source not yet implemented: %s", s)
	case strings.Contains(s, "://"):
		return nil, fmt.Errorf("unsupported source scheme: %s", s)
	default:
		// Local path (relative or absolute).
		return &LocalSource{}, nil
	}
}
