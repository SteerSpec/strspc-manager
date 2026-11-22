package rulediff

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// tree maps a slash path to the content of a file.
type tree map[string]string

func (tr tree) write(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range slices.Sorted(maps.Keys(tr)) {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(tr[name]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func (tr tree) with(more tree) tree {
	out := maps.Clone(tr)
	maps.Copy(out, more)
	return out
}

func compareTrees(t *testing.T, base, head tree, opts ...Option) *result.Result {
	t.Helper()
	return CompareRealms(base.write(t), head.write(t), opts...)
}

const (
	codeRealm     = `{"realm":{"id":"a.b"}}`
	codeParent    = `{"realm":{"id":"a.b"},"sub_realms":["app"]}`
	stdRealm      = `{"realm":{"id":"a.b","kind":"standard"}}`
	stdParent     = `{"realm":{"id":"a.b","kind":"standard"},"sub_realms":["app"]}`
	codeChild     = `{"realm":{"id":"a.b.app"}}`
	stdChild      = `{"realm":{"id":"a.b.app","kind":"standard"}}`
	someEntity    = `{"entity":{"id":"APP"},"rules":[]}`
	unknownKind   = `{"realm":{"id":"a.b","kind":"code"}}`
	wrongChildID  = `{"realm":{"id":"a.b.x","kind":"standard"}}`
	brokenJSON    = `{"realm":`
	entityInApp   = "app/APP.json"
	childManifest = "app/realm.json"
)

// A change of kind turns evaluation off or on (STDRLM-011), so it is an Error
// however it is made: by the field, or by moving the manifests around files
// that were already there.
func TestCompareRealms_KindChange(t *testing.T) {
	file := tree{entityInApp: someEntity}
	for _, tc := range []struct {
		name       string
		base, head tree
	}{
		{"field, code to standard", tree{"realm.json": codeRealm}, tree{"realm.json": stdRealm}},
		{"field, standard to code", tree{"realm.json": stdRealm}, tree{"realm.json": codeRealm}},
		{"field, to an invalid kind", tree{"realm.json": codeRealm}, tree{"realm.json": unknownKind}},
		{"field of a sub-realm", tree{"realm.json": codeParent, childManifest: codeChild}, tree{"realm.json": codeParent, childManifest: stdChild}},
		{"field, over existing files", file.with(tree{"realm.json": codeRealm}), file.with(tree{"realm.json": stdRealm})},
		{"code child undeclared and deleted", file.with(tree{"realm.json": stdParent, childManifest: codeChild}), file.with(tree{"realm.json": stdRealm})},
		{"standard child added over existing files", file.with(tree{"realm.json": codeRealm}), file.with(tree{"realm.json": codeParent, childManifest: stdChild})},
		{"existing standard child declared", file.with(tree{"realm.json": codeRealm, childManifest: stdChild}), file.with(tree{"realm.json": codeParent, childManifest: stdChild})},
		{"manifest above a standard child deleted", file.with(tree{"realm.json": codeRealm, childManifest: stdChild}), file.with(tree{childManifest: stdChild})},
		{"manifest added over files that had none", file, file.with(tree{"realm.json": stdRealm})},
		{"child ID made valid", file.with(tree{"realm.json": codeParent, childManifest: wrongChildID}), file.with(tree{"realm.json": codeParent, childManifest: stdChild})},
		// From the root the files stay code, but from a source rooted at app/
		// they become standard.
		{"standard manifest planted for a source rooted below", file.with(tree{"realm.json": codeRealm}), file.with(tree{"realm.json": codeRealm, childManifest: stdChild})},
		{"code child undeclared and deleted, file edited", file.with(tree{"realm.json": stdParent, childManifest: codeChild}), tree{"realm.json": stdRealm, entityInApp: `{"entity":{"id":"APP"}}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := compareTrees(t, tc.base, tc.head)
			assertCodeSeverity(t, res, "RD028", result.Error)
		})
	}
}

func TestCompareRealms_KindUnchanged(t *testing.T) {
	file := tree{entityInApp: someEntity}
	for _, tc := range []struct {
		name       string
		base, head tree
	}{
		{"nothing", file.with(tree{"realm.json": stdRealm}), file.with(tree{"realm.json": stdRealm})},
		{"new Standard Realm", tree{}, file.with(tree{"realm.json": stdRealm})},
		{"new standard sub-realm", tree{"realm.json": codeRealm, "TOP.json": someEntity}, file.with(tree{"realm.json": codeParent, "TOP.json": someEntity, childManifest: stdChild})},
		{"new code sub-realm", tree{"realm.json": stdRealm, "STD.json": someEntity}, file.with(tree{"realm.json": stdParent, "STD.json": someEntity, childManifest: codeChild})},
		{"another field", file.with(tree{"realm.json": codeRealm}), file.with(tree{"realm.json": `{"realm":{"id":"a.b","version":"2.0.0"}}`})},
		{"schema files", tree{"_schema/entity.json": "{}"}, tree{"_schema/entity.json": "{}", "realm.json": stdRealm}},
		{"deleted with its files", file.with(tree{"realm.json": stdRealm}), tree{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if res := compareTrees(t, tc.base, tc.head); len(res.Diagnostics) != 0 {
				t.Errorf("got %v", res.Diagnostics)
			}
		})
	}
}

func TestCompareRealms_FailsClosedOnWhatItCantRead(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base, head tree
	}{
		{"head manifest", tree{"realm.json": codeRealm}, tree{"realm.json": brokenJSON}},
		{"base manifest", tree{"realm.json": brokenJSON}, tree{"realm.json": codeRealm}},
		{"new manifest", tree{}, tree{"realm.json": brokenJSON}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertHasCode(t, compareTrees(t, tc.base, tc.head), "RD000")
		})
	}

	dir, missing := t.TempDir(), filepath.Join(t.TempDir(), "missing")
	assertHasCode(t, CompareRealms(missing, dir), "RD000")
	assertHasCode(t, CompareRealms(dir, missing), "RD000")

	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	assertHasCode(t, CompareRealms(file, dir), "RD000")

	t.Run("dangling manifest link", func(t *testing.T) {
		head := t.TempDir()
		if err := os.Symlink("nowhere", filepath.Join(head, "realm.json")); err != nil {
			t.Skip(err)
		}
		assertHasCode(t, CompareRealms(dir, head), "RD000")
	})

	t.Run("linked root", func(t *testing.T) {
		base := tree{"realm.json": codeRealm}.write(t)
		link := filepath.Join(t.TempDir(), "rules")
		if err := os.Symlink(tree{"realm.json": stdRealm}.write(t), link); err != nil {
			t.Skip(err)
		}
		assertHasCode(t, CompareRealms(base, link), "RD028")
	})
}

func manifestWith(t *testing.T, notes ...entity.RealmNote) string {
	t.Helper()
	data, err := json.Marshal(map[string]any{"realm": map[string]string{"id": "a.b"}, "notes": notes})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestCompareRealms_RealmNotes(t *testing.T) {
	exclusion := entity.RealmNote{ID: "REALM/001", Type: "exclusion", Ref: "x.y:1", Content: "Not applicable.", AddedBy: "github:a"}
	rationale := entity.RealmNote{ID: "REALM/001", Type: "rationale", Content: "Because.", AddedBy: "github:a"}
	withdrawn := exclusion
	withdrawn.Type, withdrawn.Revision = "reference", 1
	edit := func(n entity.RealmNote, f func(*entity.RealmNote)) []entity.RealmNote {
		f(&n)
		return []entity.RealmNote{n}
	}
	one := func(n entity.RealmNote) []entity.RealmNote { return []entity.RealmNote{n} }

	for _, tc := range []struct {
		name       string
		base, head []entity.RealmNote
		want       string
	}{
		{"withdrawn", one(exclusion), one(withdrawn), ""},
		{"revised", one(exclusion), edit(exclusion, func(n *entity.RealmNote) { n.Content, n.Revision = "Still not.", 1 }), ""},
		{"withdrawn note revised", one(withdrawn), edit(withdrawn, func(n *entity.RealmNote) { n.Content, n.Revision = "See x.", 2 }), ""},
		{"added", nil, one(exclusion), ""},
		{"withdrawn with new content", one(exclusion), edit(withdrawn, func(n *entity.RealmNote) { n.Content = "Other." }), "RD029"},
		{"withdrawn without its ref", one(exclusion), edit(withdrawn, func(n *entity.RealmNote) { n.Ref = "" }), "RD029"},
		{"withdrawn to rationale", one(exclusion), edit(withdrawn, func(n *entity.RealmNote) { n.Type = "rationale" }), "RD029"},
		{"withdrawal undone", one(withdrawn), edit(exclusion, func(n *entity.RealmNote) { n.Revision = 2 }), "RD029"},
		{"retyped into exclusion", one(rationale), edit(exclusion, func(n *entity.RealmNote) { n.Content, n.Revision = rationale.Content, 1 }), "RD029"},
		{"other retype", one(rationale), edit(rationale, func(n *entity.RealmNote) { n.Type, n.Revision = "clarification", 1 }), "RD029"},
		{"ref retargeted", one(exclusion), edit(exclusion, func(n *entity.RealmNote) { n.Ref, n.Revision = "x.y:2", 1 }), "RD029"},
		{"reference with a ref, never an exclusion", nil, edit(withdrawn, func(n *entity.RealmNote) { n.Revision = 0 }), "RD029"},
		{"removed", one(exclusion), nil, "RD014"},
		{"renumbered", one(exclusion), edit(exclusion, func(n *entity.RealmNote) { n.ID = "REALM/002" }), "RD014"},
		{"revised, no revision", one(exclusion), edit(exclusion, func(n *entity.RealmNote) { n.Content = "Still not." }), "RD013"},
		{"withdrawn, no revision", one(exclusion), edit(withdrawn, func(n *entity.RealmNote) { n.Revision = 0 }), "RD013"},
		{"revision alone", one(exclusion), edit(exclusion, func(n *entity.RealmNote) { n.Revision = 1 }), "RD013"},
		{"added at revision 1", nil, edit(exclusion, func(n *entity.RealmNote) { n.Revision = 1 }), "RD015"},
		{"added by nobody", nil, edit(exclusion, func(n *entity.RealmNote) { n.AddedBy = " " }), "RD011"},
		{"duplicate that differs", one(exclusion), []entity.RealmNote{{ID: "REALM/001", Type: "rationale", Content: exclusion.Content}, exclusion}, "RD029"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := compareTrees(t, tree{"realm.json": manifestWith(t, tc.base...)}, tree{"realm.json": manifestWith(t, tc.head...)})
			if tc.want != "" {
				assertCodeSeverity(t, res, tc.want, result.Error)
			} else if len(res.Diagnostics) != 0 {
				t.Errorf("got %v", res.Diagnostics)
			}
		})
	}

	t.Run("manifest deleted", func(t *testing.T) {
		assertHasCode(t, compareTrees(t, tree{"realm.json": manifestWith(t, exclusion)}, tree{}), "RD014")
	})
	t.Run("new manifest", func(t *testing.T) {
		assertHasCode(t, compareTrees(t, tree{}, tree{"realm.json": manifestWith(t, withdrawn)}), "RD029")
	})
}

// STDRLM-017 when a fix is drafted: only the history shows whether its target
// was live then.
func TestCompareRealms_SupersedesOnlyLiveRules(t *testing.T) {
	rule := func(id, state, supersedes string) entity.Rule {
		r := entity.Rule{ID: id, State: state, Clause: "1.1"}
		if supersedes != "" {
			r.Supersedes = &supersedes
		}
		return r
	}
	dead, live := rule("STD-001", "T", ""), rule("STD-001", "P", "")
	for _, tc := range []struct {
		name       string
		base, head []entity.Rule
		want       bool
	}{
		{"new fix of a Terminated rule", []entity.Rule{dead}, []entity.Rule{dead, rule("STD-002", "D", "STD-001")}, true},
		{"retargeted to a dead rule while publishing", []entity.Rule{dead, rule("STD-002", "D", "")}, []entity.Rule{dead, rule("STD-002", "P", "STD-001")}, true},
		{"published over a rule already dead", []entity.Rule{dead, rule("STD-002", "D", "STD-001")}, []entity.Rule{dead, rule("STD-002", "P", "STD-001")}, true},
		{"new fix of a Draft fix", []entity.Rule{live, rule("STD-002", "D", "STD-001")}, []entity.Rule{live, rule("STD-002", "D", "STD-001"), rule("STD-003", "D", "STD-002")}, true},
		{"new fix of a live rule", []entity.Rule{live}, []entity.Rule{live, rule("STD-002", "D", "STD-001")}, false},
		{"fix published, target terminated with it", []entity.Rule{live, rule("STD-002", "D", "STD-001")}, []entity.Rule{dead, rule("STD-002", "P", "STD-001")}, false},
		{"fix of a fix published in this change", []entity.Rule{live, rule("STD-002", "D", "STD-001")}, []entity.Rule{dead, rule("STD-002", "P", "STD-001"), rule("STD-003", "D", "STD-002")}, false},
		{"published long ago", []entity.Rule{dead, rule("STD-002", "P", "STD-001")}, []entity.Rule{dead, rule("STD-002", "P", "STD-001"), rule("STD-003", "D", "")}, false},
		{"fix abandoned", []entity.Rule{dead, rule("STD-002", "D", "STD-001")}, []entity.Rule{dead, rule("STD-002", "A", "STD-001")}, false},
		{"target missing", nil, []entity.Rule{rule("STD-002", "D", "STD-001")}, false},
	} {
		for _, kind := range []string{"standard", ""} {
			for _, nested := range []bool{false, true} {
				name, manifest := tc.name+"/code", codeRealm
				if kind != "" {
					name, manifest = tc.name+"/standard", stdRealm
				}
				if nested {
					name += "/sub-entity"
				}
				t.Run(name, func(t *testing.T) {
					file := func(rules []entity.Rule) string {
						f := entity.File{Entity: entity.Entity{ID: "STD"}, Rules: rules}
						if nested {
							f = entity.File{Entity: entity.Entity{ID: "TOP"}, SubEntities: []entity.File{f}}
						}
						data, err := json.Marshal(f)
						if err != nil {
							t.Fatal(err)
						}
						return string(data)
					}
					res := compareTrees(t,
						tree{"realm.json": manifest, "STD.json": file(tc.base)},
						tree{"realm.json": manifest, "STD.json": file(tc.head)})
					if tc.want && kind != "" {
						assertCodeSeverity(t, res, "RD030", result.Error)
					} else {
						assertNoCode(t, res, "RD030")
					}
				})
			}
		}
	}
}

func TestCompareDir_ComparesRealms(t *testing.T) {
	base := tree{"realm.json": codeRealm}.write(t)
	head := tree{"realm.json": stdRealm}.write(t)
	assertHasCode(t, CompareDir(base, head), "RD028")

	// CompareDir still takes a base that doesn't exist for a new directory.
	res := CompareDir(filepath.Join(t.TempDir(), "missing"), tree{"realm.json": stdRealm}.write(t))
	if len(res.Diagnostics) != 0 {
		t.Errorf("a new directory drew %v", res.Diagnostics)
	}
}

// A head that can't be read never passes as nothing to compare.
func TestCompareDir_HeadMustBeADirectory(t *testing.T) {
	file := filepath.Join(tree{"realm.json": stdRealm}.write(t), "realm.json")
	for name, head := range map[string]string{
		"missing":         filepath.Join(t.TempDir(), "missing"),
		"not a directory": file,
		"empty path":      "",
	} {
		t.Run(name, func(t *testing.T) {
			assertCodeSeverity(t, CompareDir(tree{"realm.json": stdRealm}.write(t), head), "RD000", result.Error)
			assertCodeSeverity(t, CompareDir(filepath.Join(t.TempDir(), "no-base"), head), "RD000", result.Error)
			assertCodeSeverity(t, CompareRealms(tree{"realm.json": stdRealm}.write(t), head), "RD000", result.Error)
		})
	}
}

// Only a base that is absent is an empty one, not a broken path to one.
func TestCompareDir_BrokenBaseIsNotEmpty(t *testing.T) {
	head := tree{"realm.json": stdRealm}.write(t)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Join(t.TempDir(), "gone"), link); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	for name, base := range map[string]string{
		"dangling link":         link,
		"below a dangling link": filepath.Join(link, "a", "rules"),
		"empty path":            "",
	} {
		t.Run(name, func(t *testing.T) {
			assertCodeSeverity(t, CompareDir(base, head), "RD000", result.Error)
		})
	}

	absent := filepath.Join(t.TempDir(), "a", "rules")
	if res := CompareDir(absent, head); len(res.Diagnostics) != 0 {
		t.Errorf("a new directory below new ones drew %v", res.Diagnostics)
	}
}
