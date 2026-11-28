package rulediff

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// successor returns a rule TSTENT-004 in state that supersedes old.
func successor(state, old string) entity.Rule {
	return entity.Rule{
		ID: "TSTENT-004", State: state, Body: "Fixed body",
		AddedBy: "github:test", AddedAt: "2026-01-01", Supersedes: ptr(old),
	}
}

// A code Realm's rule with a clause is repaired by superseding it; the old
// rule keeps the clause (RUL-007, RUL-017 r1). rule-diff accepts each step.
// A new rule enters as Draft (RD004), so a Published rule takes two changes:
// add the successor, then publish it and terminate the old rule (RULST-012).
// An Implemented rule is retired in the change that adds its successor.
func TestDiff_DeadClauseRepair(t *testing.T) {
	tests := []struct {
		name string
		// old is the index in makeBase of the rule with the clause.
		old int
		// before adds to base; after edits head.
		before, after func(f *entity.File)
		// wantRD004 is set for the one change rule-diff refuses.
		wantRD004 bool
	}{
		{
			name: "Published: add the successor", old: 1,
			after: func(f *entity.File) { f.Rules = append(f.Rules, successor(entity.StateDraft, "TSTENT-002")) },
		},
		{
			name: "Published: publish the successor, terminate the old rule", old: 1,
			before: func(f *entity.File) { f.Rules = append(f.Rules, successor(entity.StateDraft, "TSTENT-002")) },
			after: func(f *entity.File) {
				f.Rules[1].State = entity.StateTerminated
				f.Rules[3].State = entity.StatePublished
			},
		},
		{
			name: "Published: both in one change", old: 1, wantRD004: true,
			after: func(f *entity.File) {
				f.Rules[1].State = entity.StateTerminated
				f.Rules = append(f.Rules, successor(entity.StatePublished, "TSTENT-002"))
			},
		},
		{
			name: "Implemented: retire it and add the successor", old: 2,
			after: func(f *entity.File) {
				f.Rules[2].State = entity.StateRetired
				f.Rules = append(f.Rules, successor(entity.StateDraft, "TSTENT-003"))
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := makeBase()
			base.Rules[tc.old].Clause = "1.1"
			if tc.before != nil {
				tc.before(base)
			}
			head := makeHead(base)
			tc.after(head)
			res := New().Diff(base, head)
			if tc.wantRD004 {
				if n := countCode(res, "RD004"); n != 1 || len(res.Errors()) != 1 {
					t.Errorf("got %v, want RD004 only: a new rule enters as Draft", res.Diagnostics)
				}
				return
			}
			if errs := res.Errors(); len(errs) > 0 {
				t.Errorf("unexpected errors: %v", errs)
			}
		})
	}
}
