package rulediff

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// withClause returns makeBase with a clause on the rule at index i, as the
// rule of a code Realm that breaks RUL-017 would have.
func withClause(i int) *entity.File {
	base := makeBase()
	base.Rules[i].Clause = "1.1"
	return base
}

// successor returns a rule TSTENT-004 in state that supersedes old.
func successor(state, old string) entity.Rule {
	return entity.Rule{
		ID: "TSTENT-004", State: state, Body: "Fixed body",
		AddedBy: "test@example.com", AddedAt: "2026-01-01", Supersedes: ptr(old),
	}
}

// A code Realm's rule with a clause is repaired by superseding it; the old
// rule keeps the clause (RUL-007, RUL-017 r1). rule-diff accepts each step.
// A new rule enters as Draft (RD004), so a Published rule takes two changes:
// add the successor, then publish it and terminate the old rule (RULST-012).
// An Implemented rule is retired in the change that adds its successor.
func TestDiff_DeadClauseRepair(t *testing.T) {
	t.Run("Published: add the successor", func(t *testing.T) {
		base := withClause(1)
		head := makeHead(base)
		head.Rules = append(head.Rules, successor(entity.StateDraft, "TSTENT-002"))
		if errs := New().Diff(base, head).Errors(); len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
	})
	t.Run("Published: publish the successor, terminate the old rule", func(t *testing.T) {
		base := withClause(1)
		base.Rules = append(base.Rules, successor(entity.StateDraft, "TSTENT-002"))
		head := makeHead(base)
		head.Rules[1].State = entity.StateTerminated
		head.Rules[3].State = entity.StatePublished
		if errs := New().Diff(base, head).Errors(); len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
	})
	t.Run("Published: both in one change", func(t *testing.T) {
		base := withClause(1)
		head := makeHead(base)
		head.Rules[1].State = entity.StateTerminated
		head.Rules = append(head.Rules, successor(entity.StatePublished, "TSTENT-002"))
		res := New().Diff(base, head)
		if n := countCode(res, "RD004"); n != 1 || len(res.Errors()) != 1 {
			t.Errorf("got %v, want RD004 only: a new rule enters as Draft", res.Diagnostics)
		}
	})
	t.Run("Implemented: retire it and add the successor", func(t *testing.T) {
		base := withClause(2)
		head := makeHead(base)
		head.Rules[2].State = entity.StateRetired
		head.Rules = append(head.Rules, successor(entity.StateDraft, "TSTENT-003"))
		if errs := New().Diff(base, head).Errors(); len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
	})
}
