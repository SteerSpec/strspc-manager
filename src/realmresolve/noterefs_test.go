package realmresolve

import (
	"context"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// noteRefDiags returns the RR017 and RR018 diagnostics of res.
func noteRefDiags(res *result.Result) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "RR017" || d.Code == "RR018" {
			out = append(out, d)
		}
	}
	return out
}

func TestCheckNoteRefs(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "note-refs/root"), absTestdata(t, "note-refs/root"))

	tests := []struct{ code, substr string }{
		{"RR017", `note ROOT-002/01 (related) references "dev.other:OTHER-001": realm dev.other is not a dependency of realm dev.root`},
		{"RR017", `note ROOT-003/01 (extends) references "dev.deep:DEEP-001"`},
		{"RR017", `note ROOT-004/01 (related) references "dev.side:SIDE-001"`},
		{"RR017", `note ROOTSUB-001/01 (related) references "dev.other:OTHER-006"`},
		{"RR018", `note ROOT-005/01 (extends) references "dev.lib:NOPE-001": no entity NOPE in realm dev.lib`},
		{"RR018", `note ROOT-006/01 (supersedes) references "dev.lib:LIBENT-999": no rule LIBENT-999 in realm dev.lib`},
		{"RR018", `note ROOT-007/01 (related) references "dev.root:ROOT-999": no rule ROOT-999 in realm dev.root`},
	}
	for _, tt := range tests {
		got := diagsFor(res, tt.code, tt.substr)
		if len(got) != 1 {
			t.Errorf("%s %q: got %d diagnostics, want 1", tt.code, tt.substr, len(got))
			continue
		}
		if got[0].Severity != result.Error {
			t.Errorf("%s %q: severity = %v, want Error", tt.code, tt.substr, got[0].Severity)
		}
	}

	// Anything else stays silent: valid references, the repeat in ROOT-002/01,
	// notes that are not relation notes and notes on dead rules.
	if got := noteRefDiags(res); len(got) != len(tests) {
		t.Errorf("got %d note diagnostics, want %d: %v", len(got), len(tests), got)
	}
}

func TestCheckNoteRefs_PathIsTheEntityFile(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "note-refs/root"), absTestdata(t, "note-refs/root"))
	want := absTestdata(t, "note-refs/root/ROOT.json")
	got := diagsFor(res, "RR017", "ROOT-003/01")
	if len(got) != 1 || got[0].Path != want {
		t.Errorf("diagnostics = %v, want one with path %s", got, want)
	}
}

func TestCheckNoteRefs_RealmWithNothingToResolve(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "note-refs-solo"), absTestdata(t, "note-refs-solo"))
	if got := diagsFor(res, "RR017", "realm dev.other is not a dependency of realm dev.solo"); len(got) != 1 {
		t.Errorf("got %d RR017 diagnostics, want 1: %v", len(got), res.Diagnostics)
	}
}

func TestCheckNoteRefs_FailedDependencyIsReportedOnce(t *testing.T) {
	// A note into a dependency that failed to load, directly or through the
	// parent's list in a sub-realm, adds nothing to the load failure.
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "note-refs-broken"), absTestdata(t, "note-refs-broken"))
	if !hasDiagCode(res, "RR002") {
		t.Errorf("expected RR002 for the missing dependency, got: %v", res.Diagnostics)
	}
	if got := noteRefDiags(res); len(got) != 0 {
		t.Errorf("expected no note diagnostics, got: %v", got)
	}
}

func TestCheckNoteRefs_StopsWhenCancelled(t *testing.T) {
	dir := absTestdata(t, "note-refs-solo")
	rf := loadTestRealm(t, "note-refs-solo")
	res := &result.Result{}
	euids, entities := collectEUIDs(context.Background(), dir, res)
	root := &ResolvedRealm{Realm: rf, Dir: dir, EUIDs: euids, Entities: entities}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	New().checkNoteRefs(ctx, root, nil, res)

	if !hasDiagCode(res, "RR000") {
		t.Errorf("expected RR000, got: %v", res.Diagnostics)
	}
	if got := noteRefDiags(res); len(got) != 0 {
		t.Errorf("expected no note diagnostics, got: %v", got)
	}
}

func TestCheckNoteRefs_TargetNotLive(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "note-refs-state/root"), absTestdata(t, "note-refs-state/root"))

	tests := []struct{ code, substr string }{
		{"RR019", `note BODY-001/01 (extends) references "dev.lib:LIB-002": rule LIB-002 is Abandoned`},
		{"RR019", `note BODY-001/02 (related) references "dev.lib:LIB-003": rule LIB-003 is Terminated`},
		{"RR019", `rule BODY-002 body references "dev.lib:LIB-002": rule LIB-002 is Abandoned`},
		{"RR019", `rule BODY-003 body references "dev.lib:LIB-003": rule LIB-003 is Terminated`},
		{"RR019", `rule BODY-004 body references "dev.lib:LIB-004": rule LIB-004 is superseded`},
		{"RR020", `rule BODY-005 body references "dev.other:OTHER-001": realm dev.other is not a dependency of realm dev.root`},
		{"RR020", `rule BODY-010 body references "dev.other:OTHER-004"`},
		{"RR021", `rule BODY-006 body references "dev.lib:LIB-999": no rule LIB-999 in realm dev.lib`},
		{"RR021", `rule BODY-007 body references "dev.lib:NOPE-001": no entity NOPE in realm dev.lib`},
	}
	for _, tt := range tests {
		got := diagsFor(res, tt.code, tt.substr)
		if len(got) != 1 {
			t.Errorf("%s %q: got %d diagnostics, want 1", tt.code, tt.substr, len(got))
			continue
		}
		if got[0].Severity != result.Warning {
			t.Errorf("%s %q: severity = %v, want Warning", tt.code, tt.substr, got[0].Severity)
		}
		if want := absTestdata(t, "note-refs-state/root/BODY.json"); got[0].Path != want {
			t.Errorf("%s %q: path = %s, want %s", tt.code, tt.substr, got[0].Path, want)
		}
	}

	// Anything else stays silent: live targets, a supersedes note naming a
	// dead rule, backticked text, and the body and notes of a dead rule.
	var warnings int
	for _, d := range res.Diagnostics {
		if d.Code == "RR019" || d.Code == "RR020" || d.Code == "RR021" {
			warnings++
		}
	}
	if warnings != len(tests) {
		t.Errorf("got %d warnings, want %d: %v", warnings, len(tests), res.Diagnostics)
	}
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}
