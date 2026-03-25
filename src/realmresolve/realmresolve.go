// Package realmresolve resolves Realm dependencies declared in realm.json.
// It fetches remote Realms (e.g. github:// URIs), resolves local paths,
// caches results, and detects EUID collisions across dependent Realms.
package realmresolve

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "realm-resolve"

// ResolveResult holds the resolved dependencies for a realm.
type ResolveResult struct {
	Dependencies []*ResolvedRealm
}

// ResolvedRealm represents a single resolved dependency.
type ResolvedRealm struct {
	Realm *entity.RealmFile
	Dir   string            // absolute path to resolved realm directory
	EUIDs map[string]string // EUID → file path within this realm
}

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
// baseDir is the directory containing the parent realm.json.
func (r *RealmResolver) Resolve(_ context.Context, rf *entity.RealmFile, baseDir string) (*ResolveResult, *result.Result) {
	res := &result.Result{}
	out := &ResolveResult{}

	// Collect parent realm EUIDs.
	parentEUIDs := collectEUIDs(baseDir, res)

	for _, dep := range rf.Dependencies {
		// RR006: self-referencing dependency.
		if dep.RealmID == rf.Realm.ID {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR006",
				Severity: result.Error,
				Message:  fmt.Sprintf("self-referencing dependency: realm_id %q matches parent realm", dep.RealmID),
			})
			continue
		}

		// RR001: empty source.
		if dep.Source == "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR001",
				Severity: result.Error,
				Message:  fmt.Sprintf("dependency %q has empty source", dep.RealmID),
			})
			continue
		}

		// Skip remote sources (future Phase 2).
		if strings.Contains(dep.Source, "://") {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR001",
				Severity: result.Info,
				Message:  fmt.Sprintf("skipping remote dependency %q (source %q): remote resolution not yet supported", dep.RealmID, dep.Source),
			})
			continue
		}

		// Resolve local path.
		resolvedDir := filepath.Join(baseDir, dep.Source)
		resolvedDir, err := filepath.Abs(resolvedDir)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR002",
				Severity: result.Error,
				Message:  fmt.Sprintf("cannot resolve path for dependency %q: %v", dep.RealmID, err),
			})
			continue
		}

		resolved := resolveLocalDep(dep, resolvedDir, res)
		if resolved == nil {
			continue
		}

		// RR005: EUID collisions between parent and dependency.
		for euid, depPath := range resolved.EUIDs {
			if parentPath, ok := parentEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR005",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in parent (%s) and dependency %q (%s)", euid, parentPath, dep.RealmID, depPath),
				})
			}
		}

		out.Dependencies = append(out.Dependencies, resolved)
	}

	return out, res
}

// collectEUIDs walks a directory and returns a map of EUID → file path.
func collectEUIDs(dir string, res *result.Result) map[string]string {
	euids := make(map[string]string)
	err := entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			return nil // skip unparseable files
		}
		euids[ef.Entity.ID] = path
		return nil
	})
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR002",
			Severity: result.Error,
			Message:  fmt.Sprintf("failed to walk directory %q: %v", dir, err),
		})
	}
	return euids
}
