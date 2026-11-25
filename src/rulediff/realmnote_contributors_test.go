package rulediff

import (
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
