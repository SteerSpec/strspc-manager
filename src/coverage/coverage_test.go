package coverage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
)

var (
	asvs4 = entity.StandardRef{ID: "owasp.asvs", Version: "4.0", Relation: entity.RelationImplements}
	asvs5 = entity.StandardRef{ID: "owasp.asvs", Version: "5.0", Relation: entity.RelationImplements}
	cited = entity.StandardRef{ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom}
)

func ptr(s string) *string { return &s }

// file builds an entity from rules given as ID, state and optional override,
// with a `standard` note on a rule for each entry of maps.
func file(id string, refs []entity.StandardRef, rules []entity.Rule, maps map[string][]string) *entity.File {
	f := &entity.File{Entity: entity.Entity{ID: id, Title: id, Standards: refs}, Rules: rules}
	for rule, contents := range maps {
		for i, c := range contents {
			f.Notes = append(f.Notes, entity.Note{ID: rule + "/0" + string(rune('1'+i)), RuleRef: rule, Type: entity.NoteTypeStandard, Content: c})
		}
	}
	return f
}

func rule(id, state string) entity.Rule { return entity.Rule{ID: id, State: state} }

func overriding(id, state, target string) entity.Rule {
	r := rule(id, state)
	r.Overrides = ptr(target)
	return r
}

func chain(child *inherit.Hop, ancestors ...inherit.Hop) *realmresolve.Inheritance {
	return &realmresolve.Inheritance{Entity: *child, Ancestors: ancestors}
}

func hop(realm string, f *entity.File) *inherit.Hop { return &inherit.Hop{RealmID: realm, Entity: f} }

// statuses renders the report's clauses as `entity standard@version:clause status`.
func statuses(r *Report) []string {
	out := []string{}
	for _, c := range r.Clauses {
		out = append(out, c.Entity+" "+c.Standard+"@"+c.Version+":"+c.Clause+" "+string(c.Status))
	}
	return out
}

func check(t *testing.T, r *Report, want ...string) {
	t.Helper()
	if want == nil {
		want = []string{}
	}
	if got := statuses(r); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// TestCompute checks the class of each clause over a chain given as child and
// ancestors.
func TestCompute(t *testing.T) {
	asIs := entity.StandardRef{ID: "owasp.asvs", Version: "5.0", Relation: entity.RelationDerivedFrom}
	base := func(ref entity.StandardRef, state string) inherit.Hop {
		return *hop("core", file("BASE", []entity.StandardRef{ref}, []entity.Rule{rule("BASE-001", state)},
			map[string][]string{"BASE-001": {"owasp.asvs:1"}}))
	}
	refs := []entity.StandardRef{asvs5}
	tests := []struct {
		name      string
		child     *entity.File
		ancestors []inherit.Hop
		want      []string
	}{
		{
			name: "state decides the class",
			child: file("AUTH", refs,
				[]entity.Rule{rule("AUTH-001", "I"), rule("AUTH-002", "P"), rule("AUTH-003", "D"), rule("AUTH-004", "A")},
				map[string][]string{
					"AUTH-001": {"owasp.asvs:1"}, "AUTH-002": {"owasp.asvs:2"}, "AUTH-003": {"owasp.asvs:3"}, "AUTH-004": {"owasp.asvs:4"},
				}),
			want: []string{"AUTH owasp.asvs@5.0:1 covered", "AUTH owasp.asvs@5.0:2 mapped", "AUTH owasp.asvs@5.0:3 mapped"},
		},
		{
			name: "an Implemented rule covers a clause a Published rule maps",
			child: file("AUTH", refs, []entity.Rule{rule("AUTH-001", "P"), rule("AUTH-002", "I")},
				map[string][]string{"AUTH-001": {"owasp.asvs:1"}, "AUTH-002": {"owasp.asvs:1"}}),
			want: []string{"AUTH owasp.asvs@5.0:1 covered"},
		},
		{
			name: "derived_from is cited and never covered",
			child: file("KWRD", []entity.StandardRef{cited}, []entity.Rule{rule("KWRD-001", "I")},
				map[string][]string{"KWRD-001": {"ietf.rfc2119:1"}}),
			want: []string{"KWRD ietf.rfc2119@:1 cited"},
		},
		{
			name: "a retired or terminated rule lapses",
			child: file("AUTH", refs, []entity.Rule{rule("AUTH-001", "R"), rule("AUTH-002", "T"), rule("AUTH-003", "A")},
				map[string][]string{"AUTH-001": {"owasp.asvs:1"}, "AUTH-002": {"owasp.asvs:2"}, "AUTH-003": {"owasp.asvs:3"}}),
			want: []string{"AUTH owasp.asvs@5.0:1 lapsed", "AUTH owasp.asvs@5.0:2 lapsed"},
		},
		{
			name: "a retired citation does not lapse",
			child: file("KWRD", []entity.StandardRef{cited}, []entity.Rule{rule("KWRD-001", "R")},
				map[string][]string{"KWRD-001": {"ietf.rfc2119:1"}}),
		},
		{
			name: "covered beats lapsed",
			child: file("AUTH", refs, []entity.Rule{rule("AUTH-001", "R"), rule("AUTH-002", "I")},
				map[string][]string{"AUTH-001": {"owasp.asvs:1"}, "AUTH-002": {"owasp.asvs:1"}}),
			want: []string{"AUTH owasp.asvs@5.0:1 covered"},
		},
		{
			name:      "an inherited rule covers through its owner",
			child:     file("SAV", nil, nil, nil),
			ancestors: []inherit.Hop{base(asvs5, "I")},
			want:      []string{"SAV owasp.asvs@5.0:1 covered"},
		},
		{
			name:      "an override with no mapping of its own lapses the clause",
			child:     file("SAV", nil, []entity.Rule{overriding("SAV-001", "I", "core:BASE-001")}, nil),
			ancestors: []inherit.Hop{base(asvs5, "I")},
			want:      []string{"SAV owasp.asvs@5.0:1 lapsed"},
		},
		{
			name: "an override with its own mapping covers",
			child: file("SAV", refs, []entity.Rule{overriding("SAV-001", "I", "core:BASE-001")},
				map[string][]string{"SAV-001": {"owasp.asvs:1"}}),
			ancestors: []inherit.Hop{base(asvs5, "I")},
			want:      []string{"SAV owasp.asvs@5.0:1 covered"},
		},
		{
			name: "two editions are two clauses",
			child: file("SAV", refs, []entity.Rule{overriding("SAV-001", "I", "core:BASE-001")},
				map[string][]string{"SAV-001": {"owasp.asvs:1"}}),
			ancestors: []inherit.Hop{base(asvs4, "I")},
			want:      []string{"SAV owasp.asvs@4.0:1 lapsed", "SAV owasp.asvs@5.0:1 covered"},
		},
		{
			name: "relation is read per note",
			child: file("SAV", []entity.StandardRef{asIs}, []entity.Rule{rule("SAV-001", "D")},
				map[string][]string{"SAV-001": {"owasp.asvs:1"}}),
			ancestors: []inherit.Hop{base(asvs5, "T")},
			want:      []string{"SAV owasp.asvs@5.0:1 cited", "SAV owasp.asvs@5.0:1 lapsed"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			check(t, Compute([]*realmresolve.Inheritance{chain(hop("prod", tt.child), tt.ancestors...)}), tt.want...)
		})
	}
}

// TestComputeListsEveryRuleOfAClause checks that a clause keeps all its rules.
func TestComputeListsEveryRuleOfAClause(t *testing.T) {
	f := file("AUTH", []entity.StandardRef{asvs5}, []entity.Rule{rule("AUTH-001", "P"), rule("AUTH-002", "I")},
		map[string][]string{"AUTH-001": {"owasp.asvs:1"}, "AUTH-002": {"owasp.asvs:1"}})
	r := Compute([]*realmresolve.Inheritance{chain(hop("r", f))})
	if got := len(r.Clauses[0].Rules); got != 2 {
		t.Errorf("rules = %d, want 2", got)
	}
}

// TestComputeIgnoresWithdrawnMappings checks that a note retyped to `reference`
// maps nothing.
func TestComputeIgnoresWithdrawnMappings(t *testing.T) {
	f := file("AUTH", []entity.StandardRef{asvs5}, []entity.Rule{rule("AUTH-001", "I")}, map[string][]string{"AUTH-001": {"owasp.asvs:1"}})
	f.Notes[0].Type = entity.NoteTypeReference
	check(t, Compute([]*realmresolve.Inheritance{chain(hop("r", f))}))
}

// TestComputeReportsUnresolvedNotes checks a note whose id resolves to nothing.
func TestComputeReportsUnresolvedNotes(t *testing.T) {
	f := file("AUTH", nil, []entity.Rule{rule("AUTH-001", "I")}, map[string][]string{"AUTH-001": {"owasp.asvs:1"}})
	r := Compute([]*realmresolve.Inheritance{chain(hop("r", f))})
	check(t, r)
	if len(r.Unresolved) != 1 || r.Unresolved[0].Note != "AUTH-001/01" {
		t.Errorf("Unresolved = %+v", r.Unresolved)
	}
}

// TestComputeListsDeclaredStandards checks a standard that nothing maps.
func TestComputeListsDeclaredStandards(t *testing.T) {
	h := hop("r", file("AUTH", []entity.StandardRef{asvs5}, nil, nil))
	h.Scopes = [][]entity.StandardRef{{cited}}
	r := Compute([]*realmresolve.Inheritance{chain(h)})
	if want := []entity.StandardRef{cited, asvs5}; !reflect.DeepEqual(r.Declared, want) {
		t.Errorf("Declared = %+v, want %+v", r.Declared, want)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func manifest(id string, refs []entity.StandardRef, subs []string, deps ...entity.RealmDep) entity.RealmFile {
	return entity.RealmFile{Realm: entity.RealmMeta{ID: id, Title: id, Version: "1.0.0", Standards: refs}, SubRealms: subs, Dependencies: deps}
}

// TestComputeFromResolvedRealm goes through real resolution: a rule inherited
// from a dependency resolves its note against the dependency's manifest, and a
// sub-realm's against its parent's.
func TestComputeFromResolvedRealm(t *testing.T) {
	dir := t.TempDir()
	depDir, rootDir := filepath.Join(dir, "dep"), filepath.Join(dir, "root")

	writeJSON(t, filepath.Join(depDir, "realm.json"), manifest("dev.test.dep", []entity.StandardRef{asvs4}, nil))
	writeJSON(t, filepath.Join(depDir, "BASE.json"), file("BASE", nil,
		[]entity.Rule{rule("BASE-001", "I"), rule("BASE-002", "I")},
		map[string][]string{"BASE-001": {"owasp.asvs:1"}, "BASE-002": {"owasp.asvs:2"}}))

	root := manifest("dev.test.root", []entity.StandardRef{asvs5}, []string{"team"},
		entity.RealmDep{RealmID: "dev.test.dep", Version: "1.0.0", Source: "../dep"})
	writeJSON(t, filepath.Join(rootDir, "realm.json"), root)
	child := file("SAV", nil, []entity.Rule{overriding("SAV-001", "I", "dev.test.dep:BASE-002")}, nil)
	child.Entity.Inherits = "dev.test.dep:BASE"
	writeJSON(t, filepath.Join(rootDir, "SAV.json"), child)

	writeJSON(t, filepath.Join(rootDir, "team", "realm.json"), manifest("dev.test.root.team", nil, nil))
	writeJSON(t, filepath.Join(rootDir, "team", "TEAM.json"), file("TEAM", nil, []entity.Rule{rule("TEAM-001", "P")},
		map[string][]string{"TEAM-001": {"owasp.asvs:9"}}))

	out, res := realmresolve.New().Resolve(t.Context(), &root, rootDir)
	if !res.OK() {
		t.Fatalf("Resolve: %v", res.Diagnostics)
	}
	r := Compute(out.Entities)
	check(t, r,
		"SAV owasp.asvs@4.0:1 covered",
		"SAV owasp.asvs@4.0:2 lapsed",
		"TEAM owasp.asvs@5.0:9 mapped")
}

// TestComputeDependencySubRealm checks that an entity in a dependency's
// sub-realm resolves against that sub-realm's manifest, then the dependency's.
func TestComputeDependencySubRealm(t *testing.T) {
	tests := []struct {
		name string
		sub  []entity.StandardRef
		want string
	}{
		{"nearest edition wins", []entity.StandardRef{asvs5}, "SAV owasp.asvs@5.0:1 covered"},
		{
			"nearest relation wins",
			[]entity.StandardRef{{ID: "owasp.asvs", Version: "5.0", Relation: entity.RelationDerivedFrom}},
			"SAV owasp.asvs@5.0:1 cited",
		},
		{"falls back to the parent", nil, "SAV owasp.asvs@4.0:1 covered"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			depDir, rootDir := filepath.Join(dir, "dep"), filepath.Join(dir, "root")
			writeJSON(t, filepath.Join(depDir, "realm.json"), manifest("dev.test.dep", []entity.StandardRef{asvs4}, []string{"team"}))
			writeJSON(t, filepath.Join(depDir, "team", "realm.json"), manifest("dev.test.dep.team", tt.sub, nil))
			writeJSON(t, filepath.Join(depDir, "team", "TEAM.json"), file("TEAM", nil, []entity.Rule{rule("TEAM-001", "I")},
				map[string][]string{"TEAM-001": {"owasp.asvs:1"}}))

			root := manifest("dev.test.root", nil, nil, entity.RealmDep{RealmID: "dev.test.dep", Version: "1.0.0", Source: "../dep"})
			writeJSON(t, filepath.Join(rootDir, "realm.json"), root)
			child := file("SAV", nil, nil, nil)
			child.Entity.Inherits = "dev.test.dep.team:TEAM"
			writeJSON(t, filepath.Join(rootDir, "SAV.json"), child)

			out, res := realmresolve.New().Resolve(t.Context(), &root, rootDir)
			if !res.OK() {
				t.Fatalf("Resolve: %v", res.Diagnostics)
			}
			check(t, Compute(out.Entities), tt.want)
		})
	}
}

// TestComputeSubRealmAlone checks a sub-realm resolved on its own, which finds
// its parent on disk as RL031 does.
func TestComputeSubRealmAlone(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), manifest("dev.test.root", []entity.StandardRef{asvs5}, []string{"team"}))
	sub := manifest("dev.test.root.team", nil, nil)
	subDir := filepath.Join(dir, "team")
	writeJSON(t, filepath.Join(subDir, "realm.json"), sub)
	writeJSON(t, filepath.Join(subDir, "TEAM.json"), file("TEAM", nil, []entity.Rule{rule("TEAM-001", "I")},
		map[string][]string{"TEAM-001": {"owasp.asvs:1"}}))

	out, res := realmresolve.New().Resolve(t.Context(), &sub, subDir)
	if !res.OK() {
		t.Fatalf("Resolve: %v", res.Diagnostics)
	}
	check(t, Compute(out.Entities), "TEAM owasp.asvs@5.0:1 covered")
}

// TestComputeDeclaresEveryResolvedReference checks a row whose reference is
// declared only by a dependency.
func TestComputeDeclaresEveryResolvedReference(t *testing.T) {
	base := file("BASE", []entity.StandardRef{asvs4}, []entity.Rule{rule("BASE-001", "I")}, map[string][]string{"BASE-001": {"owasp.asvs:1"}})
	r := Compute([]*realmresolve.Inheritance{chain(hop("prod", file("SAV", nil, nil, nil)), *hop("core", base))})
	if want := []entity.StandardRef{asvs4}; !reflect.DeepEqual(r.Declared, want) {
		t.Errorf("Declared = %+v, want %+v", r.Declared, want)
	}
}

// TestComputeOrdersDeclaredByURL checks the sort is total.
func TestComputeOrdersDeclaredByURL(t *testing.T) {
	a := entity.StandardRef{ID: "owasp.asvs", Relation: entity.RelationImplements, URL: "https://a.example"}
	b := entity.StandardRef{ID: "owasp.asvs", Relation: entity.RelationImplements, URL: "https://b.example"}
	x, y := hop("r", file("AAA", []entity.StandardRef{b}, nil, nil)), hop("r", file("BBB", []entity.StandardRef{a}, nil, nil))
	for range 50 {
		r := Compute([]*realmresolve.Inheritance{chain(x), chain(y)})
		if want := []entity.StandardRef{a, b}; !reflect.DeepEqual(r.Declared, want) {
			t.Fatalf("Declared = %+v, want %+v", r.Declared, want)
		}
	}
}

// TestComputeReportsMalformedNotes checks a note RL029 can't reach, on a
// dependency's rule.
func TestComputeReportsMalformedNotes(t *testing.T) {
	base := file("BASE", []entity.StandardRef{asvs5}, []entity.Rule{rule("BASE-001", "I")}, map[string][]string{"BASE-001": {"owasp.asvs: 2"}})
	r := Compute([]*realmresolve.Inheritance{chain(hop("prod", file("SAV", nil, nil, nil)), *hop("core", base))})
	if len(r.Unresolved) != 1 || r.Unresolved[0].Note != "BASE-001/01" {
		t.Errorf("Unresolved = %+v", r.Unresolved)
	}
}
