package rulelint

import (
	"fmt"
	"path/filepath"
	"slices"

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

// CheckRealmStandards runs RL031 over a Realm and its sub-realms: the id of a
// `standard` note resolves in the tree of declarations around its rule
// (NTE-016). LintRealm calls it, and realm-lint when it lints entity files
// itself. A source with no manifest has no Realm to resolve against, so neither
// reaches it for one.
func CheckRealmStandards(dir string, rf *entity.RealmFile, res *result.Result) {
	var opts []entity.WalkOption
	if len(rf.SubRealms) > 0 {
		opts = append(opts, entity.WithExcludeDirs(rf.SubRealms))
	}
	checkDirStandards(dir, rf.Realm.Standards, parentStandards(dir), res, opts...)
	for _, name := range rf.SubRealms {
		subDir := filepath.Join(dir, name)
		sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json"))
		if err != nil {
			continue // realm-lint reports it (RM009).
		}
		checkDirStandards(subDir, sub.Realm.Standards, rf.Realm.Standards, res)
	}
}

// parentStandards returns the references of the Realm whose manifest lists dir
// as a sub-realm, for a sub-realm linted on its own.
func parentStandards(dir string) []entity.StandardRef {
	if parent := entity.ParentRealm(dir); parent != nil {
		return parent.Realm.Standards
	}
	return nil
}

// checkDirStandards runs RL031 on the entity files of one Realm directory.
func checkDirStandards(dir string, realm, parent []entity.StandardRef, res *result.Result, opts ...entity.WalkOption) {
	// A file that can't be read or parsed is reported by the first pass.
	_ = entity.WalkEntityFiles(dir, func(fpath string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr == nil {
			checkNoteStandards(ef, nil, realm, parent, fpath, res)
		}
		return nil
	}, opts...)
}

// checkNoteStandards checks ef and its sub-entities. containing holds the
// references of the entities around ef, nearest first. The entity an `inherits`
// names is not among them, so it passes on nothing (STDREF-009).
func checkNoteStandards(ef *entity.File, containing [][]entity.StandardRef, realm, parent []entity.StandardRef, fpath string, res *result.Result) {
	scopes := append([][]entity.StandardRef{ef.Entity.Standards}, containing...)
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		id, _, err := entity.ParseStandardNote(n.Content)
		if err != nil {
			continue // RL029.
		}
		if entity.ResolveStandard(id, append(slices.Clone(scopes), realm, parent)...) == nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL031",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: standard %q is not declared by the entity, an entity containing it, the Realm or its parent (NTE-016)", n.ID, id),
				Path:     fpath,
			})
		}
	}
	for i := range ef.SubEntities {
		checkNoteStandards(&ef.SubEntities[i], scopes, realm, parent, fpath, res)
	}
}
