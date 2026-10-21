// Package realmresolve resolves Realm dependencies declared in realm.json.
// It fetches remote Realms (e.g. github:// URIs), resolves local paths,
// caches results, and detects EUID collisions across dependent Realms.
package realmresolve

import (
	"context"
	"fmt"
	"os"
	"path"
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
	// Dependencies holds the Realms declared in the manifest's dependencies, in
	// declaration order.
	Dependencies []*ResolvedRealm
	// SubRealms holds the child Realms named in the manifest's sub_realms, each
	// with its effective dependencies.
	SubRealms []*ResolvedRealm
}

// ResolvedRealm represents a single resolved dependency or sub-realm.
type ResolvedRealm struct {
	// Realm is the parsed realm.json manifest of the resolved Realm.
	Realm *entity.RealmFile
	// Dir is the absolute path to the resolved realm directory.
	Dir string
	// EUIDs maps each EUID to its absolute file path.
	EUIDs map[string]string

	// Dependencies holds the resolved effective dependencies of a
	// sub-realm (inherited from the parent plus its own). Nil for
	// dependencies themselves.
	Dependencies []*ResolvedRealm
}

// Config holds options for the RealmResolver.
type Config struct {
	// CacheTTL is the cache time-to-live for mutable remote refs such as
	// @latest (default: 24h).
	CacheTTL time.Duration
	// CacheDir is the path to the cache directory; remote dependencies land
	// under <CacheDir>/github/.
	CacheDir string

	// Fetcher downloads github:// dependency sources. When nil and CacheDir
	// is set, New builds one from CacheDir, CacheTTL and the
	// GITHUB_TOKEN/GH_TOKEN environment variables. When nil and no cache
	// dir is configured, remote dependencies fail with RR012.
	Fetcher *remote.Fetcher

	// Origin is the github:// reference the resolved realm was fetched
	// from, when it is not a local realm. The fetcher extracts only the
	// requested subtree, so a relative dependency that leaves that subtree
	// (`../core/` next to `rules/cli`) does not exist on disk although it
	// does in the repository; with Origin set such sources are rewritten
	// to github:// references on the same repository and ref, for the
	// realm and its sub-realms alike.
	Origin *remote.Ref
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

// WithOrigin declares that the realm passed to Resolve is the subtree
// ref.Path of a remote repository, materialised at baseDir. Relative
// dependency sources that leave the subtree are resolved against the
// repository at that ref instead of the local cache layout.
func WithOrigin(ref remote.Ref) Option {
	return func(c *Config) { c.Origin = &ref }
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

	// Sub-realm dependencies are resolved relative to their own directory;
	// rootDir anchors them to the realm root for origin rebasing.
	rootDir, err := filepath.Abs(baseDir)
	if err != nil {
		rootDir = baseDir
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
	// Resolved parent dependencies by realm_id, reused by sub-realms.
	parentResolved := make(map[string]*ResolvedRealm)

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

		resolved := r.resolveDep(ctx, dep, baseDir, rootDir, res)
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

		parentResolved[dep.RealmID] = resolved
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

		// Dependency inheritance: merge parent deps with sub-realm's own,
		// then resolve the effective list. Inherited deps reuse the parent's
		// resolution; the sub-realm's own deps are resolved relative to its
		// directory.
		ownIDs := make(map[string]struct{}, len(subRF.Dependencies))
		for _, d := range subRF.Dependencies {
			ownIDs[d.RealmID] = struct{}{}
		}
		subRF.Dependencies = mergeDepLists(rf.Dependencies, subRF.Dependencies)
		subDeps := r.resolveSubRealmDeps(ctx, subRF, subDir, rootDir, ownIDs, parentResolved, res)

		// RR011: EUID collision — sub-realm vs its effective dependencies.
		for _, dep := range subDeps {
			for euid, depPath := range dep.EUIDs {
				if subPath, ok := subEUIDs[euid]; ok {
					res.Add(result.Diagnostic{
						Module:   module,
						Code:     "RR011",
						Severity: result.Error,
						Message:  fmt.Sprintf("EUID collision: %q exists in sub-realm %q (%s) and dependency %q (%s)", euid, subRealmName, subPath, dep.Realm.Realm.ID, depPath),
						Path:     subDir,
					})
				}
			}
		}

		out.SubRealms = append(out.SubRealms, &ResolvedRealm{
			Realm:        subRF,
			Dir:          subDir,
			EUIDs:        subEUIDs,
			Dependencies: subDeps,
		})
	}

	return out, res
}

// resolveSubRealmDeps resolves a sub-realm's effective (merged) dependency
// list. Dependencies inherited from the parent (realm IDs not in ownIDs)
// reuse the parent's resolution and are skipped when the parent failed to
// resolve them, avoiding duplicate diagnostics. Dependencies the sub-realm
// declares itself (including overrides) are resolved against subDir and
// checked for EUID collisions with the dependencies already collected
// (RR005); parent-only collisions are reported once by the parent loop.
func (r *RealmResolver) resolveSubRealmDeps(ctx context.Context, subRF *entity.RealmFile, subDir, rootDir string, ownIDs map[string]struct{}, parentResolved map[string]*ResolvedRealm, res *result.Result) []*ResolvedRealm {
	var deps []*ResolvedRealm
	for _, dep := range subRF.Dependencies {
		if err := ctx.Err(); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR000",
				Severity: result.Error,
				Message:  err.Error(),
				Path:     subDir,
			})
			return deps
		}
		if _, own := ownIDs[dep.RealmID]; !own {
			if parent, ok := parentResolved[dep.RealmID]; ok {
				deps = append(deps, parent)
			}
			continue
		}
		if dep.RealmID == subRF.Realm.ID {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR006",
				Severity: result.Error,
				Message:  fmt.Sprintf("self-referencing dependency: realm_id %q matches sub-realm", dep.RealmID),
				Path:     subDir,
			})
			continue
		}
		resolved := r.resolveDep(ctx, dep, subDir, rootDir, res)
		if resolved == nil {
			continue
		}
		// RR005: EUID collisions — own dependency vs the effective
		// dependencies collected so far (inherited first, then own).
		for euid, depPath := range resolved.EUIDs {
			for _, prev := range deps {
				if _, ok := prev.EUIDs[euid]; ok {
					res.Add(result.Diagnostic{
						Module:   module,
						Code:     "RR005",
						Severity: result.Error,
						Message:  fmt.Sprintf("EUID collision: %q exists in dependency %q and dependency %q (%s) of sub-realm %q", euid, prev.Realm.Realm.ID, dep.RealmID, depPath, subRF.Realm.ID),
						Path:     resolved.Dir,
					})
					break
				}
			}
		}
		deps = append(deps, resolved)
	}
	return deps
}

// resolveDep validates a dependency's source and resolves it, locally or
// through the remote fetcher. Relative paths are taken against baseDir;
// when the resolver has an Origin, relative paths that leave the fetched
// subtree (rootDir) are rebased onto the origin repository first.
// Returns nil when the dependency cannot be resolved (diagnostics added).
func (r *RealmResolver) resolveDep(ctx context.Context, dep entity.RealmDep, baseDir, rootDir string, res *result.Result) *ResolvedRealm {
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

	// Relative source of a remote realm: rebase onto the origin repository
	// when it leaves the subtree that was fetched.
	if r.cfg.Origin != nil && !strings.Contains(dep.Source, "://") {
		rebased, escapes := rebaseOnOrigin(*r.cfg.Origin, dep.Source, baseDir, rootDir)
		if escapes {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR001",
				Severity: result.Error,
				Message:  fmt.Sprintf("dependency %q source %q escapes the repository root of %s", dep.RealmID, dep.Source, r.cfg.Origin.String()),
				Path:     baseDir,
			})
			return nil
		}
		if rebased != "" {
			dep.Source = rebased
		}
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

// rebaseOnOrigin maps a relative dependency source, declared in baseDir of
// a realm whose root rootDir holds origin.Path of the repository, to its
// repository path. It returns the github:// reference when that path lies
// outside the fetched subtree (so it is absent from the cache), "" when the
// source stays inside the subtree and local resolution applies, and
// escapes=true when the path leaves the repository root.
func rebaseOnOrigin(origin remote.Ref, source, baseDir, rootDir string) (rebased string, escapes bool) {
	rel, err := filepath.Rel(rootDir, baseDir)
	if err != nil || rel == "." {
		rel = ""
	}
	joined := path.Clean(path.Join(origin.Path, filepath.ToSlash(rel), filepath.ToSlash(source)))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", true
	}
	if joined == "." {
		joined = ""
	}
	inside := origin.Path == "" || joined == origin.Path || strings.HasPrefix(joined, origin.Path+"/")
	if inside {
		return "", false
	}
	return remote.Ref{Owner: origin.Owner, Repo: origin.Repo, Ref: origin.Ref, Path: joined}.String(), false
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
