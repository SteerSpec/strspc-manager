package rulelint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func codes(res *result.Result) map[string]int {
	out := make(map[string]int)
	for _, d := range res.Diagnostics {
		out[d.Code]++
	}
	return out
}

// mappedFile is an entity with one rule and the given `standard` notes.
func mappedFile(id string, refs []entity.StandardRef, contents ...string) *entity.File {
	f := &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: id, Title: "Mapped", Standards: refs},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules: []entity.Rule{{
			ID: id + "-001", State: entity.StatePublished, Body: "The system MUST work.",
			AddedBy: "@a", AddedAt: "2026-01-01",
		}},
	}
	for i, c := range contents {
		f.Notes = append(f.Notes, entity.Note{
			ID: id + "-001/0" + string(rune('1'+i)), RuleRef: id + "-001", Type: entity.NoteTypeStandard,
			Content: c, AddedBy: "@a", AddedAt: "2026-01-01",
		})
	}
	return f
}

var asvs = []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements}}

func TestRL028_EntityStandards(t *testing.T) {
	bad := []entity.StandardRef{{ID: "owasp", Relation: entity.RelationImplements}}
	if got := codes(New().LintFile(mappedFile("MAPPED", bad)))["RL028"]; got != 1 {
		t.Errorf("RL028 = %d, want 1", got)
	}
	if got := codes(New().LintFile(mappedFile("MAPPED", asvs)))["RL028"]; got != 0 {
		t.Errorf("RL028 = %d, want 0", got)
	}
}

func TestRL029_NoteContent(t *testing.T) {
	if got := codes(New().LintFile(mappedFile("MAPPED", asvs, "not a mapping")))["RL029"]; got != 1 {
		t.Errorf("RL029 = %d, want 1", got)
	}
	if got := codes(New().LintFile(mappedFile("MAPPED", asvs, "owasp.asvs:2.1.1")))["RL029"]; got != 0 {
		t.Errorf("RL029 = %d, want 0", got)
	}
}

func TestRL030_DuplicateMapping(t *testing.T) {
	f := mappedFile("MAPPED", asvs, "owasp.asvs:2.1.1", "owasp.asvs:2.1.1", "owasp.asvs:2.1.2")
	if got := codes(New().LintFile(f))["RL030"]; got != 1 {
		t.Errorf("RL030 = %d, want 1", got)
	}
}

// A note retyped to `reference` is a withdrawn mapping, not a mapping.
func TestStandardChecksIgnoreOtherNoteTypes(t *testing.T) {
	f := mappedFile("MAPPED", asvs, "not a mapping")
	f.Notes[0].Type = entity.NoteTypeReference
	if got := codes(New().LintFile(f)); got["RL029"]+got["RL030"] != 0 {
		t.Errorf("got %v", got)
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

func realmFile(id string, refs []entity.StandardRef, subs ...string) *entity.RealmFile {
	return &entity.RealmFile{
		Schema:    "./_schema/realm.v1.schema.json",
		Realm:     entity.RealmMeta{ID: id, Title: "R", Version: "1.0.0", Standards: refs},
		SubRealms: subs,
	}
}

func rl031(t *testing.T, dir string) int {
	t.Helper()
	return codes(New().LintRealm(t.Context(), dir))["RL031"]
}

func TestRL031_UndeclaredPrefix(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 1 {
		t.Errorf("RL031 = %d, want 1", got)
	}
}

func TestRL031_DeclaredOnEntity(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", asvs, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}

func TestRL031_DeclaredOnContainingEntity(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil))
	outer := mappedFile("OUTER", asvs)
	sub := mappedFile("INNER", nil, "owasp.asvs:2.1.1")
	sub.Entity.Parent = "OUTER"
	outer.SubEntities = []entity.File{*sub}
	writeJSON(t, filepath.Join(dir, "OUTER.json"), outer)
	if got := rl031(t, dir); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}

func TestRL031_DeclaredOnRealm(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", asvs))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}

func TestRL031_SubRealmUsesParent(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", asvs, "team"))
	writeJSON(t, filepath.Join(dir, "team", "realm.json"), realmFile("dev.test.root.team", nil))
	writeJSON(t, filepath.Join(dir, "team", "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 0 {
		t.Errorf("from the parent: RL031 = %d, want 0", got)
	}
	if got := rl031(t, filepath.Join(dir, "team")); got != 0 {
		t.Errorf("from the sub-realm: RL031 = %d, want 0", got)
	}
}

// A parent does not see its sub-realm's declaration.
func TestRL031_ParentDoesNotUseSubRealm(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil, "team"))
	writeJSON(t, filepath.Join(dir, "team", "realm.json"), realmFile("dev.test.root.team", asvs))
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 1 {
		t.Errorf("RL031 = %d, want 1", got)
	}
}

// An `inherits` declaration passes on no references (STDREF-009).
func TestRL031_InheritsPassesNothing(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", nil))
	writeJSON(t, filepath.Join(dir, "BASE.json"), mappedFile("BASE", asvs))
	child := mappedFile("CHILD", nil, "owasp.asvs:2.1.1")
	child.Entity.Inherits = "BASE"
	writeJSON(t, filepath.Join(dir, "CHILD.json"), child)
	if got := rl031(t, dir); got != 1 {
		t.Errorf("RL031 = %d, want 1", got)
	}
}

// Without a manifest there is no Realm to resolve against, so nothing can be
// called undeclared.
func TestRL031_NoManifestSkipped(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "MAPPED.json"), mappedFile("MAPPED", nil, "owasp.asvs:2.1.1"))
	if got := rl031(t, dir); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}
