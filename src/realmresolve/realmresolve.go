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
	EUIDs map[string]string // EUID → absolute file path
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
func (r *RealmResolver) Resolve(ctx context.Context, rf *entity.RealmFile, baseDir string) (*ResolveResult, *result.Result) {
	res := &result.Result{}
	out := &ResolveResult{}

	if err := ctx.Err(); err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR000",
			Severity: result.Error,
			Message:  err.Error(),
			Path:     baseDir,
		})
		return out, res
	}

	// Collect parent realm EUIDs.
	parentEUIDs := collectEUIDs(ctx, baseDir, res)

	// Track all dependency EUIDs for dep-vs-dep collision detection.
	allDepEUIDs := make(map[string]string) // EUID → realm_id

	for _, dep := range rf.Dependencies {
		if err := ctx.Err(); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR000",
				Severity: result.Error,
				Message:  err.Error(),
				Path:     baseDir,
			})
			return out, res
		}

		// RR006: self-referencing dependency.
		if dep.RealmID == rf.Realm.ID {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR006",
				Severity: result.Error,
				Message:  fmt.Sprintf("self-referencing dependency: realm_id %q matches parent realm", dep.RealmID),
				Path:     baseDir,
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
				Path:     baseDir,
			})
			continue
		}

		// RR001: absolute paths not allowed.
		if filepath.IsAbs(dep.Source) {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR001",
				Severity: result.Error,
				Message:  fmt.Sprintf("dependency %q has absolute source path %q; only relative paths are allowed", dep.RealmID, dep.Source),
				Path:     baseDir,
			})
			continue
		}

		// Skip remote sources (future Phase 2).
		if strings.Contains(dep.Source, "://") {
			res.Add(result.Diagnostic{
				Module:   module,
				Severity: result.Info,
				Message:  fmt.Sprintf("skipping remote dependency %q (source %q): remote resolution not yet supported", dep.RealmID, dep.Source),
				Path:     baseDir,
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
				Path:     baseDir,
			})
			continue
		}

		resolved := resolveLocalDep(ctx, dep, resolvedDir, res)
		if resolved == nil {
			continue
		}

		// RR005: EUID collisions — parent vs dependency.
		for euid, depPath := range resolved.EUIDs {
			if parentPath, ok := parentEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR005",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in parent (%s) and dependency %q (%s)", euid, parentPath, dep.RealmID, depPath),
					Path:     resolvedDir,
				})
			}
		}

		// RR005: EUID collisions — dependency vs dependency.
		for euid, depPath := range resolved.EUIDs {
			if prevRealm, ok := allDepEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR005",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in dependency %q and dependency %q (%s)", euid, prevRealm, dep.RealmID, depPath),
					Path:     resolvedDir,
				})
			}
			allDepEUIDs[euid] = dep.RealmID
		}

		out.Dependencies = append(out.Dependencies, resolved)
	}

	return out, res
}

// collectEUIDs walks a directory and returns a map of EUID → file path,
// including EUIDs from sub-entities.
func collectEUIDs(ctx context.Context, dir string, res *result.Result) map[string]string {
	euids := make(map[string]string)
	err := entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if parseErr != nil {
			msg := fmt.Sprintf("accessing path: %s", parseErr)
			if data != nil {
				msg = fmt.Sprintf("parsing entity: %s", parseErr)
			}
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR002",
				Severity: result.Error,
				Message:  msg,
				Path:     path,
			})
			return nil
		}
		addEUIDs(ef, path, euids)
		return nil
	})
	if err != nil && ctx.Err() == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR002",
			Severity: result.Error,
			Message:  fmt.Sprintf("failed to walk directory %q: %v", dir, err),
			Path:     dir,
		})
	}
	return euids
}

// addEUIDs recursively collects EUIDs from an entity file and its sub-entities.
func addEUIDs(ef *entity.File, path string, euids map[string]string) {
	if id := ef.Entity.ID; id != "" {
		euids[id] = path
	}
	for i := range ef.SubEntities {
		addEUIDs(&ef.SubEntities[i], path, euids)
	}
}
