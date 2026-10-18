// Package realmresolve resolves Realm dependencies declared in realm.json.
// It fetches remote Realms (e.g. github:// URIs), resolves local paths,
// caches results, and detects EUID collisions across dependent Realms.
package realmresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "realm-resolve"

// ResolveResult holds the resolved dependencies and sub-realms for a realm.
type ResolveResult struct {
	Dependencies []*ResolvedRealm
	SubRealms    []*ResolvedRealm
}

// ResolvedRealm represents a single resolved dependency.
type ResolvedRealm struct {
	Realm *entity.RealmFile
	Dir   string            // absolute path to resolved realm directory
	EUIDs map[string]string // EUID → absolute file path
}

// Config holds options for the RealmResolver.
type Config struct {
	CacheTTL time.Duration // cache time-to-live for mutable remote refs such as @latest (default: 24h)
	CacheDir string        // path to cache directory; remote dependencies land under <CacheDir>/github/

	// Fetcher downloads github:// dependency sources. When nil and CacheDir
	// is set, New builds one from CacheDir, CacheTTL and the
	// GITHUB_TOKEN/GH_TOKEN environment variables. When nil and no cache
	// dir is configured, remote dependencies fail with RR012.
	Fetcher *remote.Fetcher
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

// WithRemoteFetcher supplies the fetcher used for github:// dependency
// sources, overriding the one New would build from CacheDir/CacheTTL.
func WithRemoteFetcher(f *remote.Fetcher) Option {
	return func(c *Config) { c.Fetcher = f }
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
	if cfg.Fetcher == nil && cfg.CacheDir != "" {
		cfg.Fetcher = &remote.Fetcher{
			Token:    remote.TokenFromEnv(),
			CacheDir: cfg.CacheDir,
			TTL:      cfg.CacheTTL,
		}
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

	// Collect parent realm EUIDs. When sub-realms are declared, exclude
	// their directories so their entities don't appear in the parent's
	// EUID set (they are checked separately).
	var walkOpts []entity.WalkOption
	if len(rf.SubRealms) > 0 {
		walkOpts = append(walkOpts, entity.WithExcludeDirs(rf.SubRealms))
	}
	parentEUIDs := collectEUIDs(ctx, baseDir, res, walkOpts...)

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

		resolved := r.resolveDep(ctx, dep, baseDir, res)
		if resolved == nil {
			continue
		}
		resolvedDir := resolved.Dir

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

	// --- Sub-realm resolution ---
	allSubRealmEUIDs := make(map[string]string) // EUID → "sub-realm-name (file-path)"

	for _, subRealmName := range rf.SubRealms {
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

		// Validate sub-realm name is a clean directory name (no path traversal).
		if subRealmName == "" || subRealmName == "." || subRealmName == ".." ||
			strings.ContainsAny(subRealmName, "/\\") || filepath.IsAbs(subRealmName) ||
			filepath.VolumeName(subRealmName) != "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR007",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q: invalid directory name", subRealmName),
				Path:     baseDir,
			})
			continue
		}

		subDir := filepath.Join(baseDir, subRealmName)
		subDir, err := filepath.Abs(subDir)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR007",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q: cannot resolve absolute path: %v", subRealmName, err),
				Path:     baseDir,
			})
			continue
		}

		// RR007: sub-realm directory must exist and be a directory.
		info, err := os.Stat(subDir)
		if err != nil || !info.IsDir() {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR007",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q: directory %q does not exist or is not a directory", subRealmName, subDir),
				Path:     subDir,
			})
			continue
		}

		// RR008: load realm.json from sub-realm directory.
		subRealmPath := filepath.Join(subDir, "realm.json")
		subRF, err := entity.LoadRealm(subRealmPath)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR008",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q: cannot load realm.json from %q: %v", subRealmName, subRealmPath, err),
				Path:     subRealmPath,
			})
			continue
		}

		// RR009: sub-realm ID must match parent.id + "." + dirname.
		expectedID := rf.Realm.ID + "." + subRealmName
		if subRF.Realm.ID != expectedID {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR009",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q: expected ID %q but found %q", subRealmName, expectedID, subRF.Realm.ID),
				Path:     subRealmPath,
			})
			continue
		}

		// RR010: sub-realm must not declare its own sub_realms.
		if len(subRF.SubRealms) > 0 {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR010",
				Severity: result.Error,
				Message:  fmt.Sprintf("sub-realm %q declares nested sub_realms; only one level of nesting is allowed", subRealmName),
				Path:     subRealmPath,
			})
			continue
		}

		// Collect EUIDs from the sub-realm directory.
		subEUIDs := collectEUIDs(ctx, subDir, res)

		// RR011: EUID collision — sub-realm vs parent.
		for euid, subPath := range subEUIDs {
			if parentPath, ok := parentEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR011",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in parent (%s) and sub-realm %q (%s)", euid, parentPath, subRealmName, subPath),
					Path:     subDir,
				})
			}
		}

		// RR011: EUID collision — sub-realm vs sibling sub-realm.
		for euid, subPath := range subEUIDs {
			if prevSub, ok := allSubRealmEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR011",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in %s and sub-realm %q (%s)", euid, prevSub, subRealmName, subPath),
					Path:     subDir,
				})
			} else {
				allSubRealmEUIDs[euid] = fmt.Sprintf("sub-realm %q (%s)", subRealmName, subPath)
			}
		}

		// RR011: EUID collision — sub-realm vs dependency.
		// TODO: Once sub-realm dep resolution is implemented (Phase 2-4),
		// this should check against the sub-realm's effective dep EUIDs
		// (after mergeDepLists), not the parent's dep EUIDs.
		for euid, subPath := range subEUIDs {
			if depRealm, ok := allDepEUIDs[euid]; ok {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RR011",
					Severity: result.Error,
					Message:  fmt.Sprintf("EUID collision: %q exists in sub-realm %q (%s) and dependency %q", euid, subRealmName, subPath, depRealm),
					Path:     subDir,
				})
			}
		}

		// Dependency inheritance: merge parent deps with sub-realm's own.
		effectiveDeps := mergeDepLists(rf.Dependencies, subRF.Dependencies)
		subRF.Dependencies = effectiveDeps

		out.SubRealms = append(out.SubRealms, &ResolvedRealm{
			Realm: subRF,
			Dir:   subDir,
			EUIDs: subEUIDs,
		})
	}

	return out, res
}

// resolveDep validates a dependency's source and resolves it, locally or
// through the remote fetcher. Relative paths are taken against baseDir.
// Returns nil when the dependency cannot be resolved (diagnostics added).
func (r *RealmResolver) resolveDep(ctx context.Context, dep entity.RealmDep, baseDir string, res *result.Result) *ResolvedRealm {
	// RR001: empty source.
	if dep.Source == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR001",
			Severity: result.Error,
			Message:  fmt.Sprintf("dependency %q has empty source", dep.RealmID),
			Path:     baseDir,
		})
		return nil
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
		return nil
	}

	// Remote source: fetch into the cache, then treat as local.
	if strings.Contains(dep.Source, "://") {
		dir, err := r.fetchRemote(ctx, dep)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR012",
				Severity: result.Error,
				Message:  fmt.Sprintf("dependency %q: %v", dep.RealmID, err),
				Path:     dep.Source,
			})
			return nil
		}
		return resolveLocalDep(ctx, dep, dir, res)
	}

	// Resolve local path.
	resolvedDir, err := filepath.Abs(filepath.Join(baseDir, dep.Source))
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR002",
			Severity: result.Error,
			Message:  fmt.Sprintf("cannot resolve path for dependency %q: %v", dep.RealmID, err),
			Path:     baseDir,
		})
		return nil
	}
	return resolveLocalDep(ctx, dep, resolvedDir, res)
}

// fetchRemote materialises a remote dependency source in the cache and
// returns the local directory holding it.
func (r *RealmResolver) fetchRemote(ctx context.Context, dep entity.RealmDep) (string, error) {
	if !strings.HasPrefix(dep.Source, remote.Scheme) {
		return "", fmt.Errorf("unsupported remote source scheme in %q (only %s is supported)", dep.Source, remote.Scheme)
	}
	ref, err := remote.ParseGitHub(dep.Source)
	if err != nil {
		return "", err
	}
	if r.cfg.Fetcher == nil {
		return "", fmt.Errorf("remote source %s requires a cache directory (WithCacheDir) or fetcher (WithRemoteFetcher)", ref.String())
	}
	dir, err := r.cfg.Fetcher.EnsureDir(ctx, ref)
	if err != nil {
		return "", fmt.Errorf("fetching %s: %w", ref.String(), err)
	}
	return dir, nil
}

// mergeDepLists merges parent and child dependency lists.
// Parent deps come first; if a child declares a dep with the same realm_id
// as a parent dep, the child's version takes precedence (override).
func mergeDepLists(parent, child []entity.RealmDep) []entity.RealmDep {
	// Build set of child realm IDs for override detection.
	childIDs := make(map[string]struct{}, len(child))
	for _, d := range child {
		childIDs[d.RealmID] = struct{}{}
	}

	merged := make([]entity.RealmDep, 0, len(parent)+len(child))
	// Add parent deps that are not overridden by child.
	for _, d := range parent {
		if _, overridden := childIDs[d.RealmID]; !overridden {
			merged = append(merged, d)
		}
	}
	// Add all child deps (including overrides).
	merged = append(merged, child...)
	return merged
}

// collectEUIDs walks a directory and returns a map of EUID → file path,
// including EUIDs from sub-entities.
func collectEUIDs(ctx context.Context, dir string, res *result.Result, walkOpts ...entity.WalkOption) map[string]string {
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
	}, walkOpts...)
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
