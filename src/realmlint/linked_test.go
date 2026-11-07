package realmlint

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// linkedCodes are the codes checkLinkedStandards emits.
var linkedCodes = []string{"RM026", "RM027", "RM028", "RM029"}

// localFetcher resolves a relative source against the declaring Realm.
func localFetcher(_ context.Context, realmDir string, dep entity.RealmDep) (string, error) {
	if dep.Source == "" || strings.Contains(dep.Source, "://") {
		return "", fmt.Errorf("no local directory for %q", dep.Source)
	}
	return filepath.Join(realmDir, dep.Source), nil
}

// linkedFixture is a Realm APP that links the Standard Realm STD through
// realm.standards, with a rule whose notes map its clauses. Each test edits
// it, then writes it.
type linkedFixture struct {
	realm *entity.RealmFile
	app   *entity.File
	std   *entity.RealmFile
	stdV  *entity.File
}

const stdID = "dev.test.std"

func newLinkedFixture() *linkedFixture {
	note := func(n int, content string) entity.Note {
		return entity.Note{ID: fmt.Sprintf("APP-001/%02d", n), RuleRef: "APP-001", Type: entity.NoteTypeStandard, Content: content, AddedBy: "github:a", AddedAt: "2026-01-01"}
	}
	rule := func(id, state, clause string, profiles ...string) entity.Rule {
		return entity.Rule{ID: id, State: state, Body: "The application MUST " + id + ".", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: clause, Profiles: profiles}
	}
	return &linkedFixture{
		realm: &entity.RealmFile{
			Schema: "./_schema/realm.v1.schema.json",
			Realm: entity.RealmMeta{
				ID: "dev.test.app", Title: "A", Version: "1.0.0",
				Standards: []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0", Realm: stdID, Profile: "L1"}},
			},
			Dependencies: []entity.RealmDep{{RealmID: stdID, Version: "2.0.0", Source: "../std/"}},
		},
		app: &entity.File{
			Schema:  "./_schema/entity.v1.schema.json",
			Entity:  entity.Entity{ID: "APP", Title: "A"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
			Rules:   []entity.Rule{{ID: "APP-001", State: entity.StateDraft, Body: "The app MUST work.", AddedBy: "github:a", AddedAt: "2026-01-01"}},
			// V2.1.1 is above the claimed profile L1: the mapping is still
			// valid (STDREF-021).
			Notes: []entity.Note{note(1, "owasp.asvs:V1.1.1"), note(2, "owasp.asvs:V2.1.1")},
		},
		std: &entity.RealmFile{
			Schema: "./_schema/realm.v1.schema.json",
			Realm: entity.RealmMeta{
				ID: stdID, Title: "S", Version: "2.0.0", Kind: entity.KindStandard, Edition: "5.0.0",
				License: "CC-BY-SA-4.0", Attribution: &entity.Attribution{Text: "OWASP", URL: "https://owasp.org"},
				Profiles: []string{"L1", "L2"},
			},
		},
		stdV: &entity.File{
			Schema:  "./_schema/entity.v1.schema.json",
			Entity:  entity.Entity{ID: "V", Title: "V"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
			Rules: []entity.Rule{
				rule("V-001", entity.StatePublished, "V1.1.1", "L1", "L2"),
				rule("V-002", entity.StatePublished, "V2.1.1", "L2"),
				rule("V-003", entity.StateTerminated, "V3.1.1", "L1"),
			},
		},
	}
}

// write writes the fixture under a new directory and returns the Realm's.
func (f *linkedFixture) write(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	root, std := filepath.Join(base, "app"), filepath.Join(base, "std")
	writeManifest(t, root, f.realm, false)
	writeEntityFile(t, root, "APP.json", f.app)
	writeManifest(t, std, f.std, false)
	writeEntityFile(t, std, "V.json", f.stdV)
	return root
}

func writeEntityFile(t *testing.T, dir, name string, ef *entity.File) {
	t.Helper()
	data, err := json.Marshal(ef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func lintLinked(t *testing.T, dir string, opts ...Option) *result.Result {
	t.Helper()
	return New(opts...).Lint(t.Context(), dir)
}

func TestLinked_Clean(t *testing.T) {
	dir := newLinkedFixture().write(t)
	for name, opts := range map[string][]Option{"with a fetcher": {WithDependencyFetcher(localFetcher)}, "without": nil} {
		if got := countCodes(lintLinked(t, dir, opts...), append(linkedCodes, "RM014", "RM016")...); len(got) != 0 {
			t.Errorf("%s: got %v, want none", name, got)
		}
	}
}

func TestLinked_EachCode(t *testing.T) {
	tests := []struct {
		name   string
		change func(f *linkedFixture)
		want   map[string]int
	}{
		{"realm that is no dependency", func(f *linkedFixture) {
			f.realm.Realm.Standards[0].Realm = "dev.test.other"
		}, map[string]int{"RM026": 1}},
		{"dependency that is not a Standard Realm", func(f *linkedFixture) {
			f.std.Realm.Kind = ""
		}, map[string]int{"RM026": 1}},
		{"no version", func(f *linkedFixture) {
			f.realm.Realm.Standards[0].Version = ""
		}, map[string]int{"RM027": 1}},
		{"version that is the dependency's, not the edition", func(f *linkedFixture) {
			f.realm.Realm.Standards[0].Version = "2.0.0"
		}, map[string]int{"RM027": 1}},
		{"undeclared profile", func(f *linkedFixture) {
			f.realm.Realm.Standards[0].Profile = "L3"
		}, map[string]int{"RM028": 1}},
		{"unknown clause", func(f *linkedFixture) {
			f.app.Notes[1].Content = "owasp.asvs:V9.9.9"
		}, map[string]int{"RM029": 1}},
		{"clause whose rules are all Terminated", func(f *linkedFixture) {
			f.app.Notes[1].Content = "owasp.asvs:V3.1.1"
		}, map[string]int{"RM029": 1}},
		{"clause matched exactly", func(f *linkedFixture) {
			f.app.Notes[1].Content = "owasp.asvs:v1.1.1"
		}, map[string]int{"RM029": 1}},
		{"entity reference is checked too", func(f *linkedFixture) {
			f.app.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "4.0.3", Realm: stdID, Profile: "L9"}}
		}, map[string]int{"RM027": 1, "RM028": 1}},
		{"note resolves to the nearest reference, here without a realm", func(f *linkedFixture) {
			f.app.Entity.Standards = []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0"}}
			f.app.Notes[1].Content = "owasp.asvs:V9.9.9"
		}, map[string]int{}},
		{"standard without a realm is not checked", func(f *linkedFixture) {
			f.realm.Realm.Standards = append(f.realm.Realm.Standards, entity.StandardRef{ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom})
		}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newLinkedFixture()
			tt.change(f)
			got := countCodes(lintLinked(t, f.write(t), WithDependencyFetcher(localFetcher)), linkedCodes...)
			if !maps.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Without a fetcher only what the tree's manifests say is checked.
func TestLinked_NoFetcher(t *testing.T) {
	f := newLinkedFixture()
	f.realm.Realm.Standards = append(f.realm.Realm.Standards,
		entity.StandardRef{ID: "iso.27001", Relation: entity.RelationImplements, Realm: "dev.test.missing"}) // RM026, RM027.
	f.realm.Realm.Standards[0].Version = "4.0.3" // Needs the edition.
	f.realm.Realm.Standards[0].Profile = "L9"    // Needs the profiles.
	f.app.Notes[1].Content = "owasp.asvs:V9.9.9" // Needs the clause list.
	got := countCodes(lintLinked(t, f.write(t)), linkedCodes...)
	if want := map[string]int{"RM026": 1, "RM027": 1}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A dependency that isn't what was pinned is RM014's, and only the static
// checks run on references to it.
func TestLinked_Mispinned(t *testing.T) {
	f := newLinkedFixture()
	f.std.Realm.Version = "2.0.1"
	f.realm.Realm.Standards[0].Version = "4.0.3"
	got := countCodes(lintLinked(t, f.write(t), WithDependencyFetcher(localFetcher)), append(linkedCodes, "RM014")...)
	if want := map[string]int{"RM014": 1}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// A Standard Realm whose entity files can't all be read gets a warning, and
// RM029 doesn't run against a clause list that would be short.
func TestLinked_UnreadableStandardRealm(t *testing.T) {
	f := newLinkedFixture()
	f.app.Notes[1].Content = "owasp.asvs:V9.9.9"
	dir := f.write(t)
	if err := os.WriteFile(filepath.Join(dir, "..", "std", "BAD.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := lintLinked(t, dir, WithDependencyFetcher(localFetcher))
	if got, want := countCodes(res, append(linkedCodes, "RM016")...), map[string]int{"RM016": 1}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "RM016" && (d.Severity != result.Warning || d.Path != filepath.Join(dir, "realm.json")) {
			t.Errorf("RM016 = %+v, want a warning at the declaring manifest", d)
		}
	}
}

// A sub-realm's dependencies are the tree's, its references and entities are
// checked, and a sub-realm linted on its own counts its parent's.
func TestLinked_SubRealm(t *testing.T) {
	f := newLinkedFixture()
	f.app.Notes[1].Content = "owasp.asvs:V9.9.9"
	f.realm.SubRealms = []string{"team", "../escape"}
	// The parent links nothing itself; the sub-realm declares the
	// dependency and the reference.
	deps, refs := f.realm.Dependencies, f.realm.Realm.Standards
	f.realm.Dependencies, f.realm.Realm.Standards = nil, nil
	root := f.write(t)
	if err := os.Remove(filepath.Join(root, "APP.json")); err != nil {
		t.Fatal(err)
	}
	team := filepath.Join(root, "team")
	deps[0].Source = "../../std/"
	writeManifest(t, team, &entity.RealmFile{
		Schema:       "./_schema/realm.v1.schema.json",
		Realm:        entity.RealmMeta{ID: "dev.test.app.team", Title: "T", Version: "1.0.0", Standards: refs},
		Dependencies: deps,
	}, false)
	writeEntityFile(t, team, "APP.json", f.app)
	if got, want := countCodes(lintLinked(t, root, WithDependencyFetcher(localFetcher)), linkedCodes...), map[string]int{"RM029": 1}; !maps.Equal(got, want) {
		t.Errorf("tree: got %v, want %v", got, want)
	}

	// The parent declares the dependency, the sub-realm only the reference.
	parent := f.realm
	parent.SubRealms = []string{"team"}
	parent.Dependencies = []entity.RealmDep{{RealmID: stdID, Version: "2.0.0", Source: "../std/"}}
	writeManifest(t, root, parent, false)
	writeManifest(t, team, &entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json",
		Realm:  entity.RealmMeta{ID: "dev.test.app.team", Title: "T", Version: "1.0.0", Standards: refs},
	}, false)
	if got := countCodes(lintLinked(t, team), linkedCodes...); len(got) != 0 {
		t.Errorf("sub-realm alone: got %v, want none", got)
	}
}
