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

// fetchedDep is a dependency that was fetched and passed both halves of RM014:
// its source tag and its manifest match the pin.
type fetchedDep struct {
	dir string
	rf  *entity.RealmFile
	// declaredAt is the manifest that declares the dependency.
	declaredAt string
}

// fetchedDeps maps the directory of each Realm of the tree to the dependencies
// its manifest declares that were fetched, by realm_id. checkDependencies
// fills it as it checks each manifest's pins, and RM026 to RM029 read it, so
// they fetch nothing themselves. It is kept per Realm because a sub-realm's
// own declaration of a dependency is the one its references are judged
// against (RLM-014).
type fetchedDeps map[string]map[string]fetchedDep

// add records fd as the dependency id of the Realm at realmDir, unless that
// manifest declared id before.
func (f fetchedDeps) add(realmDir, id string, fd fetchedDep) {
	if f[realmDir] == nil {
		f[realmDir] = make(map[string]fetchedDep)
	}
	if _, ok := f[realmDir][id]; !ok {
		f[realmDir][id] = fd
	}
}

// declaringRealm is a Realm of the tree whose dependencies a reference may
// name.
type declaringRealm struct {
	dir string
	rf  *entity.RealmFile
}

// linkedStandards holds what the checks on Standard References with a `realm`
// read of the Realm tree and the Realms it links.
type linkedStandards struct {
	fetched fetchedDeps
	// loaded caches the Standard Realm read from each fetched directory, nil
	// when it is not one; a Standard Realm whose entity files can't all be
	// read has no Clauses.
	loaded map[string]*entity.StandardRealm
	res    *result.Result
}

// realmScope is where a Standard Reference of one Realm finds the dependency
// its `realm` names: the Realms of the tree, that Realm first, then the one it
// inherits from (RLM-014), then the others.
type realmScope []declaringRealm

// resolve returns the nearest declaration of the dependency id in s and, when
// that declaration was fetched, the fetched dependency.
func (ls *linkedStandards) resolve(s realmScope, id string) (declared bool, fd *fetchedDep) {
	for _, r := range s {
		if !slices.ContainsFunc(r.rf.Dependencies, func(d entity.RealmDep) bool { return d.RealmID == id }) {
			continue
		}
		if got, ok := ls.fetched[r.dir][id]; ok {
			return true, &got
		}
		return true, nil
	}
	return false, nil
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
// A reference resolves its `realm` against its own Realm's dependencies, then
// those the Realm inherits, a sub-realm's parent's (RLM-014), then those of
// the rest of the tree (STDREF-017), so a sub-realm's own pin of a dependency
// is the one its references are judged against. The parent of a
// sub-realm linted on its own is not fetched, so its declarations get only the
// checks that need no fetch. Without a dependency fetcher those are all that
// run: whether a `realm` is a declared dependency, and whether a `version` is
// given. A dependency that couldn't be fetched or isn't what was pinned is
// reported by RM014 or RM016 and gets only those checks too.
func checkLinkedStandards(dir string, rf *entity.RealmFile, realmPath string, fetched fetchedDeps, res *result.Result) {
	ls := &linkedStandards{
		fetched: fetched,
		loaded:  make(map[string]*entity.StandardRealm),
		res:     res,
	}
	// The Realms of the tree, nearest first for the Realm at dir.
	tree := realmScope{{dir, rf}}
	if parent := entity.ParentRealm(dir, rf.Realm.ID); parent != nil {
		tree = append(tree, declaringRealm{filepath.Dir(dir), parent})
	}
	type subRealm struct {
		name, path string
		realm      declaringRealm
	}
	var subRealms []subRealm
	for _, name := range rf.SubRealms {
		if !isSubRealmName(name) {
			continue // RM008.
		}
		subDir := filepath.Join(dir, name)
		path := filepath.Join(subDir, "realm.json")
		sub, err := entity.LoadRealm(path)
		if err != nil {
			continue // RM009.
		}
		r := declaringRealm{subDir, sub}
		subRealms = append(subRealms, subRealm{name, path, r})
		tree = append(tree, r)
	}
	// A sub-realm looks at its own declarations, then its parent's, then the
	// rest of the tree.
	root := tree
	ls.checkRefs(rf.Realm.Standards, realmPath, root)
	subs := make(map[string]realmScope, len(subRealms))
	for _, sr := range subRealms {
		s := realmScope{sr.realm}
		for _, r := range tree {
			if r.dir != sr.realm.dir {
				s = append(s, r)
			}
		}
		subs[sr.name] = s
		ls.checkRefs(sr.realm.rf.Realm.Standards, sr.path, s)
	}
	rulelint.WalkRealmStandards(dir, rf, func(path string, ef *entity.File, scopes [][]entity.StandardRef) {
		s := root
		if rel, err := filepath.Rel(dir, path); err == nil {
			if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && subs[first] != nil {
				s = subs[first]
			}
		}
		ls.checkRefs(ef.Entity.Standards, path, s)
		ls.checkNotes(ef, scopes, path, s)
	})
}

// isSubRealmName reports whether a sub_realms entry names a directory
// directly inside the Realm, as RM008 requires.
func isSubRealmName(name string) bool {
	return name != "" && name != "." && filepath.IsLocal(name) && !strings.ContainsAny(name, `/\`)
}

// standardRealm returns the Standard Realm fd holds, or nil when fd is nil or
// isn't a Standard Realm. Each fetched directory is read once.
func (ls *linkedStandards) standardRealm(fd *fetchedDep) *entity.StandardRealm {
	if fd == nil {
		return nil
	}
	if sr, ok := ls.loaded[fd.dir]; ok {
		return sr
	}
	var sr *entity.StandardRealm
	if fd.rf.Realm.Kind == entity.KindStandard {
		var err error
		if sr, err = entity.LoadStandardRealm(fd.dir, fd.rf); err != nil {
			ls.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM016",
				Severity: result.Warning,
				Message:  fmt.Sprintf("dependency %q: clause list not read, so RM029 doesn't check notes against it: %v", fd.rf.Realm.ID, err),
				Path:     fd.declaredAt,
			})
			sr = &entity.StandardRealm{ID: fd.rf.Realm.ID, Edition: fd.rf.Realm.Edition, Profiles: fd.rf.Realm.Profiles}
		}
	}
	ls.loaded[fd.dir] = sr
	return sr
}

// checkRefs runs RM026 to RM028 on the Standard References declared at path,
// in a Realm whose dependencies are found in s.
func (ls *linkedStandards) checkRefs(refs []entity.StandardRef, path string, s realmScope) {
	for _, ref := range refs {
		if ref.Realm == "" {
			continue
		}
		ls.checkRef(ref, path, s)
	}
}

func (ls *linkedStandards) checkRef(ref entity.StandardRef, path string, s realmScope) {
	declared, fd := ls.resolve(s, ref.Realm)
	var notDep string
	if !declared {
		notDep = "is not a dependency of the Realm tree"
	} else if fd != nil && fd.rf.Realm.Kind != entity.KindStandard {
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
	sr := ls.standardRealm(fd)
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
// id resolves nowhere RL031's. The `realm` is resolved in s, the Realm
// holding ef.
func (ls *linkedStandards) checkNotes(ef *entity.File, scopes [][]entity.StandardRef, path string, s realmScope) {
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
		_, fd := ls.resolve(s, ref.Realm)
		sr := ls.standardRealm(fd)
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
