package rulelint

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

var rfc2119 = []entity.StandardRef{{ID: "ietf.rfc2119", Relation: entity.RelationImplements}}

// The RL031 message is what users read; pin it so a reword is deliberate.
func TestRL031_Message(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	var res result.Result
	CheckRealmStandards(dir, realmFile("dev.test.root", nil), &res)
	if len(res.Diagnostics) != 1 {
		t.Fatalf("got %d diagnostics, want 1", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	want := `note MAPPED-001/01: standard "owasp.asvs" is not declared by the entity, an entity containing it, the Realm or its parent (NTE-016)`
	if d.Code != "RL031" || d.Message != want || d.Path != filepath.Join(dir, "MAPPED.json") {
		t.Errorf("got %+v", d)
	}
}

func TestUnresolvedStandardNotes_Fields(t *testing.T) {
	dir := t.TempDir()
	rf := realmFile("dev.test.root", nil)
	writeJSON(t, filepath.Join(dir, "realm.json"), rf)
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1", "not a mapping"))
	got := UnresolvedStandardNotes(dir, rf)
	want := []UnresolvedNote{{Path: filepath.Join(dir, "MAPPED.json"), NoteID: "MAPPED-001/01", StandardID: "owasp.asvs"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// The manifest passed in is used as given, not reread from disk: a caller
// checking a removal passes the manifest without the standard.
func TestUnresolvedStandardNotes_InMemoryManifest(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", asvs))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := UnresolvedStandardNotes(dir, realmFile("dev.test.root", asvs)); len(got) != 0 {
		t.Errorf("declared: got %+v, want none", got)
	}
	if got := UnresolvedStandardNotes(dir, realmFile("dev.test.root", nil)); len(got) != 1 {
		t.Errorf("removed in memory: got %+v, want 1", got)
	}
}

func TestUnresolvedStandardNotes_SubRealms(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "team")
	// The parent declares asvs and the sub-realm rfc2119; each side maps both.
	root := realmFile("dev.test.root", asvs, "team", "broken")
	writeJSON(t, filepath.Join(dir, "realm.json"), root)
	writeJSON(t, filepath.Join(team, "realm.json"), realmFile("dev.test.root.team", rfc2119))
	writeJSON(t, filepath.Join(dir, "ROOT.json"), mappedFile("ROOT", nil, "owasp.asvs:2.1.1", "ietf.rfc2119:MUST"))
	writeJSON(t, filepath.Join(team, "TEAM.json"), mappedFile("TEAM", nil, "owasp.asvs:2.1.1", "ietf.rfc2119:MUST"))
	// A sub-realm without a loadable manifest is skipped (RM009).
	writeJSON(t, filepath.Join(dir, "broken", "BROKEN.json"), mappedFile("BROKEN", nil, "owasp.asvs:2.1.1"))

	got := UnresolvedStandardNotes(dir, root)
	want := []UnresolvedNote{{Path: filepath.Join(dir, "ROOT.json"), NoteID: "ROOT-001/02", StandardID: "ietf.rfc2119"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("from the parent: got %+v, want %+v", got, want)
	}

	// Linted on its own, the sub-realm finds its parent through ParentRealm.
	sub, err := entity.LoadRealm(filepath.Join(team, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := UnresolvedStandardNotes(team, sub); len(got) != 0 {
		t.Errorf("from the sub-realm: got %+v, want none", got)
	}
}

func TestRealmStandardScopes(t *testing.T) {
	dir := t.TempDir()
	team := filepath.Join(dir, "team")
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", asvs, "team", "broken"))
	writeJSON(t, filepath.Join(team, "realm.json"), realmFile("dev.test.root.team", rfc2119))

	tests := []struct {
		name, dir, path string
		want            [][]entity.StandardRef
	}{
		{"root file", dir, filepath.Join(dir, "A.json"), [][]entity.StandardRef{asvs, nil}},
		{"root nested dir", dir, filepath.Join(dir, "group", "A.json"), [][]entity.StandardRef{asvs, nil}},
		{"sub-realm file", dir, filepath.Join(team, "A.json"), [][]entity.StandardRef{rfc2119, asvs}},
		{"broken sub-realm", dir, filepath.Join(dir, "broken", "A.json"), [][]entity.StandardRef{asvs}},
		{"sub-realm on its own", team, filepath.Join(team, "A.json"), [][]entity.StandardRef{rfc2119, asvs}},
		{"no manifest", t.TempDir(), "A.json", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RealmStandardScopes(tt.dir, tt.path); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestUnresolvedEntityNotes(t *testing.T) {
	outer := mappedFile("OUTER", asvs)
	inner := mappedFile("INNER", nil, "owasp.asvs:2.1.1", "ietf.rfc2119:MUST")
	inner.Entity.Parent = "OUTER"
	outer.SubEntities = []entity.File{*inner}

	// The containing entity resolves asvs; nothing resolves rfc2119.
	got := UnresolvedEntityNotes(outer, nil, "OUTER.json")
	want := []UnresolvedNote{{Path: "OUTER.json", NoteID: "INNER-001/02", StandardID: "ietf.rfc2119"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}

	// Scopes passed from outside, as for the Realm, resolve too.
	if got := UnresolvedEntityNotes(outer, [][]entity.StandardRef{rfc2119}, "OUTER.json"); len(got) != 0 {
		t.Errorf("with outer scopes: got %+v, want none", got)
	}
}
