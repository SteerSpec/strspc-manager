package coverage

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
)

const stdID = "dev.test.std"

// linkedASVS links the Standard Realm stdID with its whole clause list.
var linkedASVS = entity.StandardRef{ID: "owasp.asvs", Version: "5.0.0", Relation: entity.RelationImplements, Realm: stdID}

func clauseRule(id, state, clause string, profiles ...string) entity.Rule {
	return entity.Rule{ID: id, State: state, Body: "MUST " + id + ".", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: clause, Profiles: profiles}
}

// writeStandardRealm writes a Standard Realm whose live clauses are V1 (L1,
// L2), V2 (L2), V3 (L1, split across two rules) and V5 (no profile); V4's
// only rule is Terminated.
func writeStandardRealm(t *testing.T, dir string) *entity.RealmFile {
	t.Helper()
	rf := &entity.RealmFile{Realm: entity.RealmMeta{ID: stdID, Title: "S", Version: "1.0.0", Kind: entity.KindStandard, Edition: "5.0.0", Profiles: []string{"L1", "L2"}}}
	writeJSON(t, filepath.Join(dir, "realm.json"), rf)
	writeJSON(t, filepath.Join(dir, "V.json"), &entity.File{
		Entity: entity.Entity{ID: "V", Title: "V"},
		Rules: []entity.Rule{
			clauseRule("V-001", entity.StatePublished, "V1", "L1", "L2"),
			clauseRule("V-002", entity.StatePublished, "V2", "L2"),
			clauseRule("V-003", entity.StatePublished, "V3", "L1"),
			clauseRule("V-004", entity.StatePublished, "V3", "L1"),
			clauseRule("V-005", entity.StateTerminated, "V4", "L1"),
			clauseRule("V-006", entity.StatePublished, "V5"),
		},
	})
	return rf
}

func exclusion(n int, ref string) entity.RealmNote {
	return entity.RealmNote{ID: "REALM/00" + string(rune('0'+n)), Type: entity.NoteTypeExclusion, Ref: ref, Content: "Not applicable.", AddedBy: "github:a", AddedAt: "2026-01-01"}
}

// linkedTree is a Realm tree app that depends on the Standard Realm, with an
// entity APP whose rules map V1 (Implemented), V2 (Draft) and V3
// (Implemented). The root manifest excludes V3.
type linkedTree struct {
	dir   string
	rf    *entity.RealmFile
	res   *realmresolve.ResolveResult
	app   *entity.File
	scope []entity.StandardRef
}

func newLinkedTree(t *testing.T) *linkedTree {
	t.Helper()
	base := t.TempDir()
	stdDir := filepath.Join(base, "std")
	stdRF := writeStandardRealm(t, stdDir)
	dir := filepath.Join(base, "app")
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test.app", Title: "A", Version: "1.0.0", Standards: []entity.StandardRef{linkedASVS}},
		Notes: []entity.RealmNote{exclusion(1, "owasp.asvs:V3")},
	}
	return &linkedTree{
		dir: dir,
		rf:  rf,
		res: &realmresolve.ResolveResult{Dependencies: []*realmresolve.ResolvedRealm{{Realm: stdRF, Dir: stdDir}}},
		app: file("APP", nil,
			[]entity.Rule{rule("APP-001", entity.StateImplemented), rule("APP-002", entity.StateDraft), rule("APP-003", entity.StateImplemented)},
			map[string][]string{"APP-001": {"owasp.asvs:V1"}, "APP-002": {"owasp.asvs:V2"}, "APP-003": {"owasp.asvs:V3"}}),
		scope: rf.Realm.Standards,
	}
}

// entityIn returns f as an entity of the Realm at dir, resolving against
// scopes: its Realm's, then for a sub-realm its parent's.
func entityIn(dir string, f *entity.File, scopes ...[]entity.StandardRef) *realmresolve.Inheritance {
	return &realmresolve.Inheritance{
		Entity: inherit.Hop{RealmID: "dev.test.app", Entity: f, Scopes: scopes},
		Path:   filepath.Join(dir, f.Entity.ID+".json"),
	}
}

func (lt *linkedTree) compute(t *testing.T, entities ...*realmresolve.Inheritance) *Report {
	t.Helper()
	links := NewLinks()
	if err := links.AddTree(lt.dir, lt.rf, lt.res); err != nil {
		t.Fatal(err)
	}
	if entities == nil {
		entities = []*realmresolve.Inheritance{entityIn(lt.dir, lt.app, lt.scope)}
	}
	return Compute(entities, WithLinks(links))
}

// figures renders the figures as `entity standard/profile covered/total`,
// "-" for the Realm figure and "undefined" for a zero divisor.
func figures(r *Report) []string {
	out := []string{}
	for _, f := range r.Figures {
		e := f.Entity
		if e == "" {
			e = "-"
		}
		s := e + " " + f.Standard + "/" + f.Profile + " "
		if !f.Defined {
			s += "undefined"
		} else {
			s += string(rune('0'+f.Covered)) + "/" + string(rune('0'+f.Total))
		}
		out = append(out, s)
	}
	return out
}

func checkFigures(t *testing.T, r *Report, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := figures(r); !reflect.DeepEqual(got, want) {
		t.Errorf("figures: got  %v\nwant %v", got, want)
	}
}

func TestLinked_Statuses(t *testing.T) {
	lt := newLinkedTree(t)
	r := lt.compute(t)
	// V3 is covered but excluded; V4 has left the list; V5 nothing maps.
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 mapped",
		"APP owasp.asvs@5.0.0:V3 excluded",
		"APP owasp.asvs@5.0.0:V5 unmapped",
	)
	for _, c := range r.Clauses {
		if c.Clause == "V3" && len(c.Rules) != 1 {
			t.Errorf("excluded V3 keeps its mapping: got %v", c.Rules)
		}
	}
	// The list holds V1, V2, V3 and V5; V3 is excluded, so 1 of 3.
	checkFigures(t, r, "- owasp.asvs/ 1/3", "APP owasp.asvs/ 1/3")
	if f := r.Figures[1]; f.Excluded != 1 || f.Tree != "dev.test.app" || f.Realm != stdID {
		t.Errorf("figure = %+v", f)
	}
}

// A profile narrows the list to the clauses whose rules declare it
// (STDREF-020); a mapping above it keeps its row but doesn't count.
func TestLinked_Profile(t *testing.T) {
	lt := newLinkedTree(t)
	lt.rf.Realm.Standards[0].Profile = "L1"
	r := lt.compute(t)
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 mapped",
		"APP owasp.asvs@5.0.0:V3 excluded",
	)
	// L1 holds V1 and V3; V3 is excluded.
	checkFigures(t, r, "- owasp.asvs/L1 1/1", "APP owasp.asvs/L1 1/1")
}

// A figure whose every clause is excluded is undefined (STDREF-028).
func TestLinked_ZeroDivisor(t *testing.T) {
	lt := newLinkedTree(t)
	lt.rf.Realm.Standards[0].Profile = "L1"
	lt.rf.Notes = append(lt.rf.Notes, exclusion(2, "owasp.asvs:V1"))
	checkFigures(t, lt.compute(t), "- owasp.asvs/L1 undefined", "APP owasp.asvs/L1 undefined")
}

// A `derived_from` reference gets no figure (STDREF-026), and a reference
// without a realm none either.
func TestLinked_NoFigure(t *testing.T) {
	for name, ref := range map[string]entity.StandardRef{
		"derived_from":       {ID: "owasp.asvs", Version: "5.0.0", Relation: entity.RelationDerivedFrom, Realm: stdID},
		"no realm":           {ID: "owasp.asvs", Version: "5.0.0", Relation: entity.RelationImplements},
		"no such dependency": {ID: "owasp.asvs", Version: "5.0.0", Relation: entity.RelationImplements, Realm: "dev.test.none"},
	} {
		t.Run(name, func(t *testing.T) {
			lt := newLinkedTree(t)
			lt.scope = []entity.StandardRef{ref}
			r := lt.compute(t)
			checkFigures(t, r)
			for _, c := range r.Clauses {
				if c.Status == StatusUnmapped || c.Status == StatusExcluded {
					t.Errorf("unexpected %s", c.Status)
				}
			}
		})
	}
}

// The Realm figure counts a clause covered in any entity of the tree
// (STDREF-025), and each entity's figure only its own.
func TestLinked_RealmFigure(t *testing.T) {
	lt := newLinkedTree(t)
	other := file("OTHER", nil, []entity.Rule{rule("OTHER-001", entity.StateImplemented)},
		map[string][]string{"OTHER-001": {"owasp.asvs:V5"}})
	r := lt.compute(t, entityIn(lt.dir, lt.app, lt.scope), entityIn(lt.dir, other, lt.scope))
	checkFigures(t, r, "- owasp.asvs/ 2/3", "APP owasp.asvs/ 1/3", "OTHER owasp.asvs/ 1/3")
}

// A clause is excluded for a reference when the manifest declaring the
// reference excludes it (STDREF-022): an entity of a sub-realm measured
// against the root's reference takes the root's exclusions, and a reference
// the sub-realm declares itself, with the same id, is a reference of its own
// with its own exclusions and figure, as strspc-rules#177 clarifies STDREF-025.
func TestLinked_SubRealm(t *testing.T) {
	lt := newLinkedTree(t)
	subDir := filepath.Join(lt.dir, "team")
	sub := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test.app.team", Title: "T", Version: "1.0.0"},
		Notes: []entity.RealmNote{exclusion(1, "owasp.asvs:V2")},
	}
	lt.res.SubRealms = []*realmresolve.ResolvedRealm{{Realm: sub, Dir: subDir, Dependencies: lt.res.Dependencies}}

	// The sub-realm declares no reference: the root's, and its exclusion
	// of V3, apply.
	r := lt.compute(t, entityIn(subDir, lt.app, sub.Realm.Standards, lt.rf.Realm.Standards))
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 mapped",
		"APP owasp.asvs@5.0.0:V3 excluded",
		"APP owasp.asvs@5.0.0:V5 unmapped",
	)
	checkFigures(t, r, "- owasp.asvs/ 1/3", "APP owasp.asvs/ 1/3")

	// The sub-realm declares its own: its exclusion of V2 applies, not the
	// root's of V3.
	sub.Realm.Standards = []entity.StandardRef{linkedASVS}
	r = lt.compute(t, entityIn(subDir, lt.app, sub.Realm.Standards, lt.rf.Realm.Standards))
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 excluded",
		"APP owasp.asvs@5.0.0:V3 covered",
		"APP owasp.asvs@5.0.0:V5 unmapped",
	)
	checkFigures(t, r, "- owasp.asvs/ 2/3", "APP owasp.asvs/ 2/3")

	// The sub-realm pins another transcription, whose list is V1 alone.
	pinned := filepath.Join(filepath.Dir(lt.dir), "std-next")
	nextRF := &entity.RealmFile{Realm: entity.RealmMeta{ID: stdID, Title: "S", Version: "2.0.0", Kind: entity.KindStandard, Edition: "5.0.0"}}
	writeJSON(t, filepath.Join(pinned, "realm.json"), nextRF)
	writeJSON(t, filepath.Join(pinned, "V.json"), &entity.File{Entity: entity.Entity{ID: "V", Title: "V"}, Rules: []entity.Rule{clauseRule("V-001", entity.StatePublished, "V1")}})
	lt.res.SubRealms[0].Dependencies = []*realmresolve.ResolvedRealm{{Realm: nextRF, Dir: pinned}}
	checkFigures(t, lt.compute(t, entityIn(subDir, lt.app, sub.Realm.Standards, lt.rf.Realm.Standards)), "- owasp.asvs/ 1/1", "APP owasp.asvs/ 1/1")
}

// A reference an entity declares excludes nothing: only a manifest holds
// Realm Notes.
func TestLinked_EntityReference(t *testing.T) {
	lt := newLinkedTree(t)
	lt.app.Entity.Standards = []entity.StandardRef{linkedASVS}
	r := lt.compute(t)
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 mapped",
		"APP owasp.asvs@5.0.0:V3 covered",
		"APP owasp.asvs@5.0.0:V5 unmapped",
	)
	checkFigures(t, r, "- owasp.asvs/ 2/4", "APP owasp.asvs/ 2/4")
}

// A Standard Realm whose clause list can't be read gives a warning and no
// figure.
func TestLinked_UnreadableStandardRealm(t *testing.T) {
	lt := newLinkedTree(t)
	if err := os.WriteFile(filepath.Join(lt.res.Dependencies[0].Dir, "BAD.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := lt.compute(t)
	checkFigures(t, r)
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], stdID) {
		t.Errorf("warnings = %v, want one naming %s", r.Warnings, stdID)
	}
}

// An entity outside every tree added, and a dependency that isn't a
// Standard Realm, are measured as before.
func TestLinked_OutsideTheLinks(t *testing.T) {
	lt := newLinkedTree(t)
	r := lt.compute(t, entityIn(t.TempDir(), lt.app, lt.scope))
	checkFigures(t, r)
	check(t, r,
		"APP owasp.asvs@5.0.0:V1 covered",
		"APP owasp.asvs@5.0.0:V2 mapped",
		"APP owasp.asvs@5.0.0:V3 covered",
	)
	lt.res.Dependencies[0].Realm.Realm.Kind = ""
	checkFigures(t, lt.compute(t))
}

// Links work on what realm-resolve gives: its entity paths, sub-realm
// directories and scopes.
func TestLinked_Resolved(t *testing.T) {
	base := t.TempDir()
	writeStandardRealm(t, filepath.Join(base, "std"))
	dir := filepath.Join(base, "app")
	rf := &entity.RealmFile{
		Realm:        entity.RealmMeta{ID: "dev.test.app", Title: "A", Version: "1.0.0", Standards: []entity.StandardRef{linkedASVS}},
		Dependencies: []entity.RealmDep{{RealmID: stdID, Version: "1.0.0", Source: "../std/"}},
		SubRealms:    []string{"team"},
		Notes:        []entity.RealmNote{exclusion(1, "owasp.asvs:V3")},
	}
	writeJSON(t, filepath.Join(dir, "realm.json"), rf)
	writeJSON(t, filepath.Join(dir, "team", "realm.json"), &entity.RealmFile{Realm: entity.RealmMeta{ID: "dev.test.app.team", Title: "T", Version: "1.0.0"}})
	app := file("APP", nil, []entity.Rule{rule("APP-001", entity.StateImplemented)}, map[string][]string{"APP-001": {"owasp.asvs:V1"}})
	team := file("TEAM", nil, []entity.Rule{rule("TEAM-001", entity.StateImplemented)}, map[string][]string{"TEAM-001": {"owasp.asvs:V5"}})
	writeJSON(t, filepath.Join(dir, "APP.json"), app)
	writeJSON(t, filepath.Join(dir, "team", "TEAM.json"), team)

	res, diags := realmresolve.New().Resolve(t.Context(), rf, dir)
	if !diags.OK() {
		t.Fatalf("resolve: %v", diags.Diagnostics)
	}
	links := NewLinks()
	if err := links.AddTree(dir, rf, res); err != nil {
		t.Fatal(err)
	}
	r := Compute(res.Entities, WithLinks(links))
	// Both entities measure against the root's reference and its exclusion
	// of V3; the tree covers V1 and V5 of V1, V2, V5.
	checkFigures(t, r, "- owasp.asvs/ 2/3", "APP owasp.asvs/ 1/3", "TEAM owasp.asvs/ 1/3")
	if len(r.Warnings) != 0 {
		t.Errorf("warnings: %v", r.Warnings)
	}

	// The sub-realm resolved on its own still measures against its
	// parent's reference with the parent's exclusion of V3, through the
	// dependency it declares itself.
	teamDir := filepath.Join(dir, "team")
	teamRF := &entity.RealmFile{
		Realm:        entity.RealmMeta{ID: "dev.test.app.team", Title: "T", Version: "1.0.0"},
		Dependencies: []entity.RealmDep{{RealmID: stdID, Version: "1.0.0", Source: "../../std/"}},
	}
	writeJSON(t, filepath.Join(teamDir, "realm.json"), teamRF)
	res, diags = realmresolve.New().Resolve(t.Context(), teamRF, teamDir)
	if !diags.OK() {
		t.Fatalf("resolve team: %v", diags.Diagnostics)
	}
	links = NewLinks()
	if err := links.AddTree(teamDir, teamRF, res); err != nil {
		t.Fatal(err)
	}
	checkFigures(t, Compute(res.Entities, WithLinks(links)), "- owasp.asvs/ 1/3", "TEAM owasp.asvs/ 1/3")
}
