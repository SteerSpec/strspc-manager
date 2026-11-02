package rulelint

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkStandards runs RL028 on the entity's `standards` array, and RL029 and
// RL030 on its `standard` notes (STDREF-002 to STDREF-004, STDREF-008, NTE-014,
// NTE-015). Whether a note's id is declared needs the manifests, so that is
// RL031, in LintRealm.
func checkStandards(ef *entity.File, res *result.Result, path string) {
	for _, err := range entity.ValidateStandardRefs(ef.Entity.Standards) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL028",
			Severity: result.Error,
			Message:  err.Error(),
			Path:     path,
		})
	}
	seen := make(map[string]bool)
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		if _, _, err := entity.ParseStandardNote(n.Content); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL029",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: %s", n.ID, err),
				Path:     path,
			})
		}
		key := n.RuleRef + "\x00" + n.Content
		if seen[key] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL030",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: rule %s already maps %q (NTE-015)", n.ID, n.RuleRef, n.Content),
				Path:     path,
			})
		}
		seen[key] = true
	}
}

// UnresolvedNote is a `standard` note whose id resolves to no declaration in the
// tree around its rule (NTE-016): an RL031 finding as data.
type UnresolvedNote struct {
	// Path is the entity file holding the note.
	Path string `json:"path"`
	// NoteID is the note's id.
	NoteID string `json:"note_id"`
	// StandardID is the standard id the note names.
	StandardID string `json:"standard_id"`
}

// CheckRealmStandards runs RL031 over a Realm and its sub-realms: the id of a
// `standard` note resolves in the tree of declarations around its rule
// (NTE-016). LintRealm calls it, and realm-lint when it lints entity files
// itself. A source with no manifest has no Realm to resolve against, so neither
// reaches it for one.
func CheckRealmStandards(dir string, rf *entity.RealmFile, res *result.Result) {
	for _, u := range UnresolvedStandardNotes(dir, rf) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL031",
			Severity: result.Error,
			Message:  fmt.Sprintf("note %s: standard %q is not declared by the entity, an entity containing it, the Realm or its parent (NTE-016)", u.NoteID, u.StandardID),
			Path:     u.Path,
		})
	}
}

// UnresolvedStandardNotes returns the `standard` notes of the Realm at dir and
// of its sub-realms whose id resolves to no declaration (NTE-016), in walk
// order. rf is used as given and dir/realm.json is not read again, so a caller
// can pass a manifest it has changed in memory. A sub-realm whose manifest can't
// be loaded is skipped: realm-lint reports it (RM009). A note whose content
// doesn't parse is left to RL029.
func UnresolvedStandardNotes(dir string, rf *entity.RealmFile) []UnresolvedNote {
	var opts []entity.WalkOption
	if len(rf.SubRealms) > 0 {
		opts = append(opts, entity.WithExcludeDirs(rf.SubRealms))
	}
	out := dirUnresolvedNotes(dir, rootScopes(dir, rf), opts...)
	for _, name := range rf.SubRealms {
		subDir := filepath.Join(dir, name)
		sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json"))
		if err != nil {
			continue // realm-lint reports it (RM009).
		}
		out = append(out, dirUnresolvedNotes(subDir, subRealmScopes(sub, rf))...)
	}
	return out
}

// RealmStandardScopes returns the Realm declarations a `standard` note in the
// entity file at path resolves against after its entities (NTE-016), as
// UnresolvedStandardNotes uses them: the Realm at dir then its parent, or, for a
// file inside one of dir's sub-realms, that sub-realm then dir's Realm. Without
// a manifest at dir there are none. When the sub-realm's manifest can't be
// loaded, dir's Realm alone is returned, so a removal guard still checks
// against it.
func RealmStandardScopes(dir, path string) [][]entity.StandardRef {
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil {
		return nil
	}
	if rel, err := filepath.Rel(dir, path); err == nil {
		if first, _, nested := strings.Cut(filepath.ToSlash(rel), "/"); nested && slices.Contains(rf.SubRealms, first) {
			sub, err := entity.LoadRealm(filepath.Join(dir, first, "realm.json"))
			if err != nil {
				return [][]entity.StandardRef{rf.Realm.Standards}
			}
			return subRealmScopes(sub, rf)
		}
	}
	return rootScopes(dir, rf)
}

// rootScopes returns the declarations around the entities of the Realm rf at
// dir: rf, then the Realm whose manifest lists dir as a sub-realm, for a
// sub-realm linted on its own.
func rootScopes(dir string, rf *entity.RealmFile) [][]entity.StandardRef {
	var parent []entity.StandardRef
	if p := entity.ParentRealm(dir, rf.Realm.ID); p != nil {
		parent = p.Realm.Standards
	}
	return [][]entity.StandardRef{rf.Realm.Standards, parent}
}

// subRealmScopes returns the declarations around the entities of sub, a
// sub-realm of rf: sub, then rf.
func subRealmScopes(sub, rf *entity.RealmFile) [][]entity.StandardRef {
	return [][]entity.StandardRef{sub.Realm.Standards, rf.Realm.Standards}
}

// dirUnresolvedNotes returns the unresolved notes of the entity files of one
// Realm directory. outer holds the Realm scopes.
func dirUnresolvedNotes(dir string, outer [][]entity.StandardRef, opts ...entity.WalkOption) []UnresolvedNote {
	var out []UnresolvedNote
	// A file that can't be read or parsed is reported by the first pass.
	_ = entity.WalkEntityFiles(dir, func(fpath string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr == nil {
			out = append(out, UnresolvedEntityNotes(ef, outer, fpath)...)
		}
		return nil
	}, opts...)
	return out
}

// UnresolvedEntityNotes returns the `standard` notes of ef and its sub-entities
// whose id resolves to no declaration (NTE-016); path is copied into each
// result. outer holds the declarations around ef, nearest first: those of the
// entities containing it, then the Realm scopes RealmStandardScopes returns.
// The entity an `inherits` names is not among them, so it passes on nothing
// (STDREF-009). A note whose content doesn't parse is left to RL029.
func UnresolvedEntityNotes(ef *entity.File, outer [][]entity.StandardRef, path string) []UnresolvedNote {
	scopes := append([][]entity.StandardRef{ef.Entity.Standards}, outer...)
	var out []UnresolvedNote
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		id, _, err := entity.ParseStandardNote(n.Content)
		if err != nil {
			continue // RL029.
		}
		if entity.ResolveStandard(id, scopes...) == nil {
			out = append(out, UnresolvedNote{Path: path, NoteID: n.ID, StandardID: id})
		}
	}
	for i := range ef.SubEntities {
		out = append(out, UnresolvedEntityNotes(&ef.SubEntities[i], scopes, path)...)
	}
	return out
}
