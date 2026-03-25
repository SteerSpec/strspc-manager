package realmresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// resolveLocalDep resolves a single local dependency from the filesystem.
// Returns nil if the dependency cannot be resolved (diagnostics added to res).
func resolveLocalDep(ctx context.Context, dep entity.RealmDep, resolvedDir string, res *result.Result) *ResolvedRealm {
	// Check directory exists and is a directory.
	info, err := os.Stat(resolvedDir)
	if err != nil || !info.IsDir() {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR002",
			Severity: result.Error,
			Message:  fmt.Sprintf("dependency %q: resolved directory %q does not exist or is not a directory", dep.RealmID, resolvedDir),
			Path:     resolvedDir,
		})
		return nil
	}

	// Load realm.json from resolved directory.
	realmPath := filepath.Join(resolvedDir, "realm.json")
	loaded, err := entity.LoadRealm(realmPath)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR002",
			Severity: result.Error,
			Message:  fmt.Sprintf("dependency %q: cannot load realm.json from %q: %v", dep.RealmID, realmPath, err),
			Path:     realmPath,
		})
		return nil
	}

	// RR003: realm ID mismatch.
	if loaded.Realm.ID != dep.RealmID {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR003",
			Severity: result.Error,
			Message:  fmt.Sprintf("dependency realm ID mismatch: declared %q but found %q in %s", dep.RealmID, loaded.Realm.ID, realmPath),
			Path:     realmPath,
		})
		return nil
	}

	// RR004: version mismatch (exact string match per RLMMNFST-003).
	if loaded.Realm.Version != dep.Version {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR004",
			Severity: result.Error,
			Message:  fmt.Sprintf("dependency %q version mismatch: declared %q but found %q in %s", dep.RealmID, dep.Version, loaded.Realm.Version, realmPath),
			Path:     realmPath,
		})
		return nil
	}

	// Walk resolved directory to collect EUIDs (including sub-entities).
	euids := collectEUIDs(ctx, resolvedDir, res)

	return &ResolvedRealm{
		Realm: loaded,
		Dir:   resolvedDir,
		EUIDs: euids,
	}
}
