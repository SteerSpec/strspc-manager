package entity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// writeEntity writes ef as dir/<name>.
func writeEntity(t *testing.T, dir, name string, ef *File) {
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

func clauseRule(id, state, clause string, profiles ...string) Rule {
	return Rule{ID: id, State: state, Body: "The application MUST " + id + ".", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: clause, Profiles: profiles}
}

func TestLoadStandardRealm(t *testing.T) {
	dir := t.TempDir()
	draftFix := clauseRule("V-006", StateDraft, "V6.1.1")
	target := "V-005"
	draftFix.Supersedes = &target
	writeEntity(t, dir, "V.json", &File{
		Entity: Entity{ID: "V", Title: "V"},
		Rules: []Rule{
			clauseRule("V-001", StatePublished, "V1.1.1", "L2"),
			clauseRule("V-002", StateDraft, "V1.1.1", "L1", "L2"), // A clause split across rules.
			clauseRule("V-003", StateTerminated, "V3.1.1", "L1"),  // Not live.
			clauseRule("V-004", StateAbandoned, "V4.1.1"),
			clauseRule("V-005", StatePublished, "V5.1.1"), // No profile.
			draftFix, // A Draft fix is not live (STDRLM-014).
		},
		SubEntities: []File{{
			Entity: Entity{ID: "SUB", Title: "S"},
			Rules:  []Rule{clauseRule("SUB-001", StatePublished, "V7.1.1", "L1")},
		}},
	})
	// A sub-realm is a Realm of its own.
	writeEntity(t, filepath.Join(dir, "other"), "O.json", &File{
		Entity: Entity{ID: "O", Title: "O"},
		Rules:  []Rule{clauseRule("O-001", StatePublished, "V8.1.1")},
	})
	rf := &RealmFile{
		Realm:     RealmMeta{ID: "dev.test.std", Kind: KindStandard, Edition: "5.0.0", Profiles: []string{"L1", "L2"}},
		SubRealms: []string{"other"},
	}
	sr, err := LoadStandardRealm(dir, rf)
	if err != nil {
		t.Fatal(err)
	}
	if sr.ID != "dev.test.std" || sr.Edition != "5.0.0" || !slices.Equal(sr.Profiles, []string{"L1", "L2"}) {
		t.Errorf("got %+v", sr)
	}
	want := map[string][]string{"V1.1.1": {"L1", "L2"}, "V5.1.1": nil, "V7.1.1": {"L1"}}
	if len(sr.Clauses) != len(want) {
		t.Errorf("clauses = %v, want %v", sr.Clauses, want)
	}
	for c, p := range want {
		if !sr.HasClause(c) || !slices.Equal(sr.Clauses[c], p) {
			t.Errorf("clause %s: profiles %v (present %v), want %v", c, sr.Clauses[c], sr.HasClause(c), p)
		}
	}
	for _, c := range []string{"V3.1.1", "V4.1.1", "V6.1.1", "V8.1.1", "v1.1.1"} {
		if sr.HasClause(c) {
			t.Errorf("HasClause(%q) = true, want false", c)
		}
	}
}

// An entity file that doesn't parse would leave the clause list short.
func TestLoadStandardRealm_Unparseable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "BAD.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := LoadStandardRealm(dir, &RealmFile{Realm: RealmMeta{ID: "dev.test.std"}})
	if err == nil || !strings.Contains(err.Error(), "dev.test.std") {
		t.Errorf("err = %v, want one naming the realm", err)
	}
}
