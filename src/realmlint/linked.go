package realmlint

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// fetchedDep is a dependency of the Realm tree that was fetched and whose
// manifest declares the realm_id and version it was pinned with (RM014).
type fetchedDep struct {
	dir string
	rf  *entity.RealmFile
	// declaredAt is the manifest that declares the dependency first.
	declaredAt string
}

// fetchedDeps maps the realm_id of each such dependency to it. checkDependencies
// fills it, so a dependency is fetched once for RM014 to RM016 and RM026 to
// RM029.
type fetchedDeps map[string]fetchedDep

// linkedStandards holds what the checks on Standard References with a `realm`
// read of the Realm tree and the Realms it links.
type linkedStandards struct {
	// tree holds the realm_id of each dependency a manifest of the tree
	// declares: the Realm, its sub-realms and, for a sub-realm linted on its
	// own, its parent.
	tree    map[string]bool
	fetched fetchedDeps
	// loaded caches the Standard Realm of each fetched realm_id, nil when it
	// is not one; a Standard Realm whose entity files can't all be read has
	// no Clauses.
	loaded map[string]*entity.StandardRealm
	res    *result.Result
}

// checkLinkedStandards runs RM026 to RM029 on the Standard References of the
// Realm at dir and its sub-realms, those of their manifests and of their
// entities, and on the `standard` notes that resolve to one (NTE-016):
//
//   - RM026: a `realm` names no dependency of the Realm tree, or one whose
//     manifest doesn't declare the kind `standard` (STDREF-017).
//   - RM027: a reference with a `realm` has no `version`, or one other than
//     the `edition` of that Realm (STDREF-018).
//   - RM028: a `profile` names none of the `profiles` of that Realm
//     (STDREF-027).
//   - RM029: a note maps a clause that isn't in that Realm's clause list
//     (STDREF-021).
//
// Without a dependency fetcher only what the manifests of the tree say is
// checked: whether a `realm` is a declared dependency, and whether a
// `version` is given. A dependency that couldn't be fetched or isn't what was
// pinned is reported by RM014 or RM016 and gets only those checks too.
func checkLinkedStandards(dir string, rf *entity.RealmFile, realmPath string, fetched fetchedDeps, res *result.Result) {
	ls := &linkedStandards{
		tree:    make(map[string]bool),
		fetched: fetched,
		loaded:  make(map[string]*entity.StandardRealm),
		res:     res,
	}
	ls.addTree(rf)
	if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
		ls.addTree(parent)
	}
	type manifest struct {
		path string
		rf   *entity.RealmFile
	}
	manifests := []manifest{{realmPath, rf}}
	for _, name := range rf.SubRealms {
		if !isSubRealmName(name) {
			continue // RM008.
		}
		path := filepath.Join(dir, name, "realm.json")
		sub, err := entity.LoadRealm(path)
		if err != nil {
			continue // RM009.
		}
		ls.addTree(sub)
		manifests = append(manifests, manifest{path, sub})
	}
	for _, m := range manifests {
		ls.checkRefs(m.rf.Realm.Standards, m.path)
	}
	rulelint.WalkRealmStandards(dir, rf, func(path string, ef *entity.File, scopes [][]entity.StandardRef) {
		ls.checkRefs(ef.Entity.Standards, path)
		ls.checkNotes(ef, scopes, path)
	})
}

// isSubRealmName reports whether a sub_realms entry names a directory
// directly inside the Realm, as RM008 requires.
func isSubRealmName(name string) bool {
	return name != "" && name != "." && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`)
}

// addTree records the dependencies rf declares as dependencies of the tree.
func (ls *linkedStandards) addTree(rf *entity.RealmFile) {
	for _, dep := range rf.Dependencies {
		ls.tree[dep.RealmID] = true
	}
}

// standardRealm returns the Standard Realm a `realm` links, or nil when it
// wasn't fetched or isn't a Standard Realm.
func (ls *linkedStandards) standardRealm(id string) *entity.StandardRealm {
	if sr, ok := ls.loaded[id]; ok {
		return sr
	}
	var sr *entity.StandardRealm
	if fd, ok := ls.fetched[id]; ok && fd.rf.Realm.Kind == entity.KindStandard {
		var err error
		if sr, err = entity.LoadStandardRealm(fd.dir, fd.rf); err != nil {
			ls.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM016",
				Severity: result.Warning,
				Message:  fmt.Sprintf("dependency %q: clause list not read, so RM029 doesn't check notes against it: %v", id, err),
				Path:     fd.declaredAt,
			})
			sr = &entity.StandardRealm{ID: id, Edition: fd.rf.Realm.Edition, Profiles: fd.rf.Realm.Profiles}
		}
	}
	ls.loaded[id] = sr
	return sr
}

// checkRefs runs RM026 to RM028 on the Standard References declared at path.
func (ls *linkedStandards) checkRefs(refs []entity.StandardRef, path string) {
	for _, ref := range refs {
		if ref.Realm == "" {
			continue
		}
		ls.checkRef(ref, path)
	}
}

func (ls *linkedStandards) checkRef(ref entity.StandardRef, path string) {
	var notDep string
	if !ls.tree[ref.Realm] {
		notDep = "is not a dependency of the Realm tree"
	} else if fd, ok := ls.fetched[ref.Realm]; ok && fd.rf.Realm.Kind != entity.KindStandard {
		notDep = fmt.Sprintf("is a dependency whose manifest doesn't declare the kind %q", entity.KindStandard)
	}
	if notDep != "" {
		ls.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM026",
			Severity: result.Error,
			Message:  fmt.Sprintf("standard %s: realm %q %s (STDREF-017)", ref.ID, ref.Realm, notDep),
			Path:     path,
		})
	}
	sr := ls.standardRealm(ref.Realm)
	var badVersion string
	switch {
	case ref.Version == "":
		badVersion = fmt.Sprintf("has a realm but no version; it is the edition of %s", ref.Realm)
	case sr != nil && ref.Version != sr.Edition:
		badVersion = fmt.Sprintf("version %q is not %q, the edition of %s", ref.Version, sr.Edition, ref.Realm)
	}
	if badVersion != "" {
		ls.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM027",
			Severity: result.Error,
			Message:  fmt.Sprintf("standard %s: %s (STDREF-018)", ref.ID, badVersion),
			Path:     path,
		})
	}
	if ref.Profile != "" && sr != nil && !slices.Contains(sr.Profiles, ref.Profile) {
		ls.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM028",
			Severity: result.Error,
			Message:  fmt.Sprintf("standard %s: profile %q is not one of the profiles %v of %s (STDREF-027)", ref.ID, ref.Profile, sr.Profiles, ref.Realm),
			Path:     path,
		})
	}
}

// checkNotes runs RM029 on the `standard` notes of ef whose Standard
// Reference, resolved in scopes (NTE-016), has a `realm`: the clause is in the
// whole clause list of that Realm, whatever the profile, matched exactly
// (STDREF-021). A note whose content doesn't parse is RL029's, and one whose
// id resolves nowhere RL031's.
func (ls *linkedStandards) checkNotes(ef *entity.File, scopes [][]entity.StandardRef, path string) {
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		id, clause, err := entity.ParseStandardNote(n.Content)
		if err != nil {
			continue // RL029.
		}
		ref := entity.ResolveStandard(id, scopes...)
		if ref == nil || ref.Realm == "" {
			continue // RL031, or a standard without a Standard Realm.
		}
		sr := ls.standardRealm(ref.Realm)
		if sr == nil || sr.Clauses == nil || sr.HasClause(clause) {
			continue
		}
		ls.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM029",
			Severity: result.Error,
			Message:  fmt.Sprintf("note %s maps clause %q, which is not in the clause list of %s, the clauses of its live rules (STDREF-021)", n.ID, clause, ref.Realm),
			Path:     path,
		})
	}
}
