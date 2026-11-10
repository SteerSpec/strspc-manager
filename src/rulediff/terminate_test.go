package rulediff

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// withSuccessor returns makeBase with TSTENT-004 in state, superseding the
// Published TSTENT-002.
func withSuccessor(state string) *entity.File {
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: state,
		Body: "Fixed published body", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-002"),
	})
	return base
}

func countCode(res *result.Result, code string) int {
	n := 0
	for _, d := range res.Diagnostics {
		if d.Code == code {
			n++
		}
	}
	return n
}

// RULST-012: a superseded Published rule may go to Terminated once its
// successor is Published or later in head, in the same change or after it.
func TestDiff_RD003_SupersededPublishedTerminated(t *testing.T) {
	tests := []struct {
		name string
		// successor's state in base and in head; "" leaves it out.
		before, after string
		ok            bool
	}{
		{"successor already P", entity.StatePublished, entity.StatePublished, true},
		{"successor already I", entity.StateImplemented, entity.StateImplemented, true},
		{"successor D→P in the same change", entity.StateDraft, entity.StatePublished, true},
		{"successor P→I in the same change", entity.StatePublished, entity.StateImplemented, true},
		{"successor still D", entity.StateDraft, entity.StateDraft, false},
		{"successor D→A in the same change", entity.StateDraft, entity.StateAbandoned, false},
		{"no successor", "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := makeBase()
			if tc.before != "" {
				base = withSuccessor(tc.before)
			}
			head := makeHead(base)
			head.Rules[1].State = entity.StateTerminated // TSTENT-002: P → T
			if tc.after != "" {
				head.Rules[3].State = tc.after
			}
			res := New().Diff(base, head)
			if tc.ok {
				assertNoCode(t, res, "RD003")
				assertNoCode(t, res, "RD006")
				if errs := res.Errors(); len(errs) > 0 {
					t.Errorf("unexpected errors: %v", errs)
				}
				return
			}
			if n := countCode(res, "RD003"); n != 1 {
				t.Errorf("got %d RD003, want 1: %v", n, res.Diagnostics)
			}
			assertCodeSeverity(t, res, "RD003", result.Error)
		})
	}
}

// The RD003 message for a P→T without a Published successor names the rule
// and RULST-012.
func TestDiff_RD003_SupersededPublishedTerminatedMessage(t *testing.T) {
	base := withSuccessor(entity.StateDraft)
	head := makeHead(base)
	head.Rules[1].State = entity.StateTerminated
	res := New().Diff(base, head)
	want := `invalid state transition: rule "TSTENT-002" is Published and no Published rule supersedes it: TSTENT-004 is in state "D" (RULST-012)`
	for _, d := range res.Diagnostics {
		if d.Code == "RD003" {
			if d.Message != want {
				t.Errorf("message = %q, want %q", d.Message, want)
			}
			return
		}
	}
	t.Errorf("no RD003: %v", res.Diagnostics)
}

// RULST-012 covers only a Published rule: an Implemented rule still reaches
// Terminated only through Retired.
func TestDiff_RD003_ImplementedTerminatedStillRejected(t *testing.T) {
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: entity.StatePublished,
		Body: "Superseding rule", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-003"),
	})
	head := makeHead(base)
	head.Rules[2].State = entity.StateTerminated // TSTENT-003: I → T
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD003")
}

// The successor can't reach I while the Published rule it supersedes stays P.
func TestDiff_RD006_SupersededStillPublished(t *testing.T) {
	base := withSuccessor(entity.StatePublished)
	head := makeHead(base)
	head.Rules[3].State = entity.StateImplemented // TSTENT-004: P → I; TSTENT-002 stays P
	res := New().Diff(base, head)
	assertCodeSeverity(t, res, "RD006", result.Warning)
	want := `superseding rule reached I but superseded rule "TSTENT-002" has state "P" (expected R, or T for a rule that was Published, RULST-012; may be a linked follow-up PR)`
	for _, d := range res.Diagnostics {
		if d.Code == "RD006" && d.Message != want {
			t.Errorf("message = %q, want %q", d.Message, want)
		}
	}
}

// A superseded rule Terminated in an earlier change satisfies RD006.
func TestDiff_RD006_SupersededAlreadyTerminated(t *testing.T) {
	base := withSuccessor(entity.StatePublished)
	base.Rules[1].State = entity.StateTerminated
	head := makeHead(base)
	head.Rules[3].State = entity.StateImplemented
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD006")
	assertNoCode(t, res, "RD003")
}
