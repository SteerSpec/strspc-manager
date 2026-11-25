package rulediff

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Finding 8: a new Realm Note is attributed like a new rule or note (CTRB-005).
func TestCompareRealms_RealmNoteContributor(t *testing.T) {
	list := []entity.Contributor{{ID: "github:alice"}}
	note := func(by string) entity.RealmNote {
		return entity.RealmNote{ID: "REALM/001", Type: "rationale", Content: "Because.", AddedBy: by}
	}
	base := tree{"realm.json": manifestWith(t)}
	for _, tc := range []struct {
		name, by string
		list     []entity.Contributor
		rd023    bool
	}{
		{"listed", "github:alice", list, false},
		{"unlisted", "github:mallory", list, true},
		{"no list", "github:mallory", nil, false},
		{"not an identifier, left to realm-lint", "not an identifier", list, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := tree{"realm.json": manifestWith(t, note(tc.by))}
			res := compareTrees(t, base, head, WithContributors(tc.list))
			if got := hasDiag(res, "RD023"); got != tc.rd023 {
				t.Errorf("RD023 = %v, want %v: %v", got, tc.rd023, res.Diagnostics)
			}
		})
	}
	// A note already in base is not checked again.
	head := tree{"realm.json": manifestWith(t, note("github:mallory"))}
	if res := compareTrees(t, head, head, WithContributors(list)); hasDiag(res, "RD023") {
		t.Errorf("existing note: %v", res.Diagnostics)
	}
}

// Each Realm's notes are checked against that Realm's own contributors, read
// from base: a sub-realm uses its parent's list (CTRB-006), and a change can't
// approve itself by adding a contributor with its note.
func TestCompareRealms_RealmNoteContributorPerRealm(t *testing.T) {
	realm := func(id string, contributors []string, subRealms []string, notes ...entity.RealmNote) string {
		var list []map[string]string
		for _, c := range contributors {
			list = append(list, map[string]string{"id": c})
		}
		data, err := json.Marshal(map[string]any{"realm": map[string]any{"id": id, "contributors": list}, "sub_realms": subRealms, "notes": notes})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	note := func(by string) entity.RealmNote {
		return entity.RealmNote{ID: "REALM/001", Type: "rationale", Content: "Because.", AddedBy: by}
	}
	base := tree{
		"a/realm.json":     realm("x.a", []string{"github:alice"}, []string{"sub"}),
		"a/sub/realm.json": realm("x.a.sub", nil, nil),
		"b/realm.json":     realm("x.b", []string{"github:bob"}, nil),
	}
	for _, tc := range []struct {
		name  string
		head  tree
		rd023 bool
	}{
		{"own contributor in each Realm", base.with(tree{"a/realm.json": realm("x.a", []string{"github:alice"}, []string{"sub"}, note("github:alice")), "b/realm.json": realm("x.b", []string{"github:bob"}, nil, note("github:bob"))}), false},
		{"another Realm's contributor", base.with(tree{"b/realm.json": realm("x.b", []string{"github:bob"}, nil, note("github:alice"))}), true},
		{"sub-realm uses its parent's list", base.with(tree{"a/sub/realm.json": realm("x.a.sub", nil, nil, note("github:alice"))}), false},
		{"sub-realm, unlisted", base.with(tree{"a/sub/realm.json": realm("x.a.sub", nil, nil, note("github:bob"))}), true},
		{"contributor added with the note", base.with(tree{"b/realm.json": realm("x.b", []string{"github:bob", "github:mallory"}, nil, note("github:mallory"))}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDiag(compareTrees(t, base, tc.head), "RD023"); got != tc.rd023 {
				t.Errorf("RD023 = %v, want %v", got, tc.rd023)
			}
		})
	}
}

// Re-review F1: lists come from base only, by realm ID, so a new, moved or
// re-parented Realm can't bring its own; a chain ending at a Realm that
// declares none uses the WithContributors list.
func TestCompareRealms_RealmNoteContributorFromBaseOnly(t *testing.T) {
	realm := func(id string, contributors []string, subRealms []string, notes ...entity.RealmNote) string {
		var list []map[string]string
		for _, c := range contributors {
			list = append(list, map[string]string{"id": c})
		}
		data, err := json.Marshal(map[string]any{"realm": map[string]any{"id": id, "contributors": list}, "sub_realms": subRealms, "notes": notes})
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	by := func(who string) entity.RealmNote {
		return entity.RealmNote{ID: "REALM/001", Type: "rationale", Content: "Because.", AddedBy: who}
	}
	alice := []entity.Contributor{{ID: "github:alice"}}
	for _, tc := range []struct {
		name       string
		base, head tree
		list       []entity.Contributor
	}{
		{"sub-realm under a root declaring none",
			tree{"realm.json": realm("x.a", nil, []string{"sub"}), "sub/realm.json": realm("x.a.sub", nil, nil)},
			tree{"realm.json": realm("x.a", nil, []string{"sub"}), "sub/realm.json": realm("x.a.sub", nil, nil, by("github:mallory"))}, alice},
		{"new sub-realm, parent list widened in head",
			tree{"a/realm.json": realm("x.a", []string{"github:alice"}, nil)},
			tree{"a/realm.json": realm("x.a", []string{"github:alice", "github:mallory"}, []string{"new"}), "a/new/realm.json": realm("x.a.new", nil, nil, by("github:mallory"))}, nil},
		{"Realm moved, list changed",
			tree{"b/realm.json": realm("x.b", []string{"github:bob"}, nil)},
			tree{"c/realm.json": realm("x.b", []string{"github:mallory"}, nil, by("github:mallory"))}, nil},
		{"realm ID changed in place to one base doesn't hold",
			tree{"realm.json": realm("x.b", []string{"github:bob"}, nil)},
			tree{"realm.json": realm("x.other", []string{"github:mallory"}, nil, by("github:mallory"))}, nil},
		{"new root with its own list",
			tree{},
			tree{"realm.json": realm("x.n", []string{"github:mallory"}, nil, by("github:mallory"))}, alice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if res := compareTrees(t, tc.base, tc.head, WithContributors(tc.list)); !hasDiag(res, "RD023") {
				t.Errorf("no RD023: %v", res.Diagnostics)
			}
		})
	}
}
