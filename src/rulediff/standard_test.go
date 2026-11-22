package rulediff

import (
	"fmt"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// clause and profiles are fixed once a rule leaves Draft (RUL-007), in a code
// Realm too: the kind is what a relabel changes, so it can't be a condition.
func TestDiff_ClauseAndProfilesAreFrozenAfterDraft(t *testing.T) {
	edits := []struct {
		name, code string
		base, head entity.Rule
	}{
		{"clause added", "RD026", entity.Rule{}, entity.Rule{Clause: "1.1"}},
		{"clause removed", "RD026", entity.Rule{Clause: "1.1"}, entity.Rule{}},
		{"clause changed", "RD026", entity.Rule{Clause: "1.1"}, entity.Rule{Clause: "1.2"}},
		{"clause respelled", "RD026", entity.Rule{Clause: "1.1"}, entity.Rule{Clause: "1.1 "}},
		{"profiles added", "RD027", entity.Rule{}, entity.Rule{Profiles: []string{"L1"}}},
		{"profiles removed", "RD027", entity.Rule{Profiles: []string{"L1"}}, entity.Rule{}},
		{"profiles changed", "RD027", entity.Rule{Profiles: []string{"L1"}}, entity.Rule{Profiles: []string{"L2"}}},
		{"profiles reordered", "RD027", entity.Rule{Profiles: []string{"L1", "L2"}}, entity.Rule{Profiles: []string{"L2", "L1"}}},
	}
	for _, state := range []string{"A", "P", "I", "R", "T"} {
		for _, e := range edits {
			for _, nested := range []bool{false, true} {
				name := state + "/" + e.name
				if nested {
					name += "/sub-entity"
				}
				t.Run(name, func(t *testing.T) {
					base := makeBase()
					base.Rules[1].State = state
					base.Rules[1].Clause, base.Rules[1].Profiles = e.base.Clause, e.base.Profiles
					head := makeHead(base)
					head.Rules[1].Clause, head.Rules[1].Profiles = e.head.Clause, e.head.Profiles
					if nested {
						b, h := makeBase(), makeHead(makeBase())
						b.Entity.ID, h.Entity.ID = "PARENT", "PARENT"
						b.Rules, h.Rules, b.Notes, h.Notes = nil, nil, nil, nil
						b.SubEntities, h.SubEntities = []entity.File{*base}, []entity.File{*head}
						base, head = b, h
					}
					assertCodeSeverity(t, New().Diff(base, head), e.code, result.Error)
				})
			}
		}
	}
}

func TestDiff_ClauseAndProfilesUnchanged(t *testing.T) {
	base := makeBase()
	base.Rules[1].Clause, base.Rules[1].Profiles = "1.1", []string{"L1"}
	base.Rules[2].Profiles = []string{}
	head := makeHead(base)
	head.Rules[1].Profiles = slices.Clone(base.Rules[1].Profiles)
	head.Rules[2].Profiles = nil // an empty list and none are the same.
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD026")
	assertNoCode(t, res, "RD027")
}

func TestDiff_DraftClauseAndProfilesEditMovesRevision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		edit     func(*entity.Rule)
		rejected bool
	}{
		{"clause, by one", func(r *entity.Rule) { r.Clause, r.Revision = "1.1", 1 }, false},
		{"profiles, by one", func(r *entity.Rule) { r.Profiles, r.Revision = []string{"L1"}, 1 }, false},
		{"clause, by two", func(r *entity.Rule) { r.Clause, r.Revision = "1.1", 2 }, true},
		{"profiles, by two", func(r *entity.Rule) { r.Profiles, r.Revision = []string{"L1"}, 2 }, true},
		{"clause with the promotion", func(r *entity.Rule) { r.Clause, r.Revision, r.State = "1.1", 1, "P" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := makeBase()
			head := makeHead(base)
			tc.edit(&head.Rules[0])
			res := New().Diff(base, head)
			if tc.rejected {
				assertHasCode(t, res, "RD021")
			} else if len(res.Diagnostics) != 0 {
				t.Errorf("an edit of a Draft rule drew %v", res.Diagnostics)
			}
		})
	}
}

// An edit and the move out of Draft are separate changes, as for the body
// (RD001): otherwise the text that gets frozen was never a Draft.
func TestDiff_ClauseAndProfilesEditWithLeavingDraft(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		base, head entity.Rule
	}{
		{"clause added", "RD026", entity.Rule{}, entity.Rule{Clause: "1.1"}},
		{"clause removed", "RD026", entity.Rule{Clause: "1.1"}, entity.Rule{}},
		{"clause changed", "RD026", entity.Rule{Clause: "1.1"}, entity.Rule{Clause: "1.2"}},
		{"profiles added", "RD027", entity.Rule{}, entity.Rule{Profiles: []string{"L1"}}},
		{"profiles removed", "RD027", entity.Rule{Profiles: []string{"L1"}}, entity.Rule{}},
		{"profiles reordered", "RD027", entity.Rule{Profiles: []string{"L1", "L2"}}, entity.Rule{Profiles: []string{"L2", "L1"}}},
	} {
		for _, state := range []string{"P", "A"} {
			for _, revision := range []int{0, 1} {
				t.Run(fmt.Sprintf("%s/D to %s/revision %d", tc.name, state, revision), func(t *testing.T) {
					base := makeBase()
					base.Rules[0].Clause, base.Rules[0].Profiles = tc.base.Clause, tc.base.Profiles
					head := makeHead(base)
					head.Rules[0].Clause, head.Rules[0].Profiles = tc.head.Clause, tc.head.Profiles
					head.Rules[0].State, head.Rules[0].Revision = state, revision
					assertCodeSeverity(t, New().Diff(base, head), tc.code, result.Error)
				})
			}
		}
	}
}

func TestDiff_PromotionKeepsClauseAndProfiles(t *testing.T) {
	base := makeBase()
	base.Rules[0].Clause, base.Rules[0].Profiles = "1.1", []string{"L1"}
	head := makeHead(base)
	head.Rules[0].State = "P"
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD026")
	assertNoCode(t, res, "RD027")
}

// The bump is allowed on a Draft edit of either, not required.
func TestDiff_DraftClauseAndProfilesEditMayKeepRevision(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Clause, head.Rules[0].Profiles = "1.1", []string{"L1"}
	if res := New().Diff(base, head); len(res.Diagnostics) != 0 {
		t.Errorf("an edit of a Draft rule drew %v", res.Diagnostics)
	}
}
