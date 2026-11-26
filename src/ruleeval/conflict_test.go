package ruleeval_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// conflictProvider is a batchProvider that also checks Draft overrides for a
// conflict with the rule they override.
type conflictProvider struct {
	batchProvider
	conflicts map[string]bool
	err       error
	checked   []ruleeval.ConflictRequest
}

// ConflictPromptVersion returns a fixed version, so conflicts are checked.
func (c *conflictProvider) ConflictPromptVersion() string { return "ctest" }

// CheckConflict records req and answers from c.conflicts, or fails with c.err.
func (c *conflictProvider) CheckConflict(_ context.Context, req ruleeval.ConflictRequest) (*ruleeval.ConflictResponse, error) {
	c.checked = append(c.checked, req)
	if c.err != nil {
		return nil, c.err
	}
	return &ruleeval.ConflictResponse{Conflicts: c.conflicts[req.Rule.Rule.ID], Explanation: "two owners against one"}, nil
}

func draftOverride(id string) ruleeval.RuleInput {
	in := entityInput("SAV", id, "D")
	in.Overridden = &inherit.Extended{ID: "core:ACCT-001", Revision: 0, Body: "Accounts MUST have one owner."}
	return in
}

func conflicts(res *result.Result) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == ruleeval.CodeOverrideConflict {
			out = append(out, d)
		}
	}
	return out
}

// TestEvaluate_DraftOverrideConflict checks RUL-023: a Draft override that
// conflicts with the rule it overrides is reported as a Warning, whatever
// fail_on says, and only a Draft override is checked.
func TestEvaluate_DraftOverrideConflict(t *testing.T) {
	cp := &conflictProvider{conflicts: map[string]bool{"SAV-001": true}}
	e, err := ruleeval.New(cp, ruleeval.WithFailOn([]string{"D", "P", "I"}))
	if err != nil {
		t.Fatal(err)
	}
	published := entityInput("SAV", "SAV-003", "P")
	published.Overrides = "core:ACCT-002"
	published.Overridden = &inherit.Extended{ID: "core:ACCT-002", Body: "b"}
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{draftOverride("SAV-001"), draftOverride("SAV-002"), published}, "diff")
	got := conflicts(res)
	if len(got) != 1 || got[0].Severity != result.Warning || !strings.Contains(got[0].Message, "SAV-001") || !strings.Contains(got[0].Message, "core:ACCT-001") {
		t.Fatalf("conflicts = %v, want one Warning for SAV-001", got)
	}
	if !res.OK() {
		t.Errorf("a conflict must not fail the run: %v", res.Diagnostics)
	}
	if len(cp.checked) != 2 {
		t.Errorf("checked %d rules, want the two Draft overrides", len(cp.checked))
	}
}

func TestEvaluate_ConflictCheckSkippedOutsideTheEvaluatedStates(t *testing.T) {
	cp := &conflictProvider{conflicts: map[string]bool{"SAV-001": true}}
	e, err := ruleeval.New(cp, ruleeval.WithEvaluateStates([]string{"P", "I"}))
	if err != nil {
		t.Fatal(err)
	}
	e.Evaluate(context.Background(), []ruleeval.RuleInput{draftOverride("SAV-001")}, "diff")
	if len(cp.checked) != 0 {
		t.Errorf("checked %v, want nothing: Draft is not evaluated", cp.checked)
	}
}

func TestEvaluate_ConflictCheckFailureWarns(t *testing.T) {
	cp := &conflictProvider{err: errors.New("boom")}
	e, err := ruleeval.New(cp)
	if err != nil {
		t.Fatal(err)
	}
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{draftOverride("SAV-001")}, "diff")
	var found bool
	for _, d := range res.Diagnostics {
		if d.Code == ruleeval.CodeProviderError && d.Severity == result.Warning && strings.Contains(d.Message, "SAV-001") {
			found = true
		}
	}
	if !found || !res.OK() {
		t.Errorf("diagnostics = %v, want an RE002 warning", res.Diagnostics)
	}
}

func TestEvaluate_ConflictCheckIsCached(t *testing.T) {
	cp := &conflictProvider{conflicts: map[string]bool{"SAV-001": true}}
	cache := newMemCache()
	e, err := ruleeval.New(cp, ruleeval.WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	for _, diff := range []string{"diff one", "diff two"} {
		if got := conflicts(e.Evaluate(context.Background(), []ruleeval.RuleInput{draftOverride("SAV-001")}, diff)); len(got) != 1 {
			t.Fatalf("%s: conflicts = %v, want one", diff, got)
		}
	}
	if len(cp.checked) != 1 {
		t.Errorf("checked %d times, want once: the answer depends on the rules, not the diff", len(cp.checked))
	}
	edited := draftOverride("SAV-001")
	edited.Overridden.Body = "Accounts MUST have an owner."
	edited.Overridden.Revision = 1
	e.Evaluate(context.Background(), []ruleeval.RuleInput{edited}, "diff")
	if len(cp.checked) != 2 {
		t.Errorf("checked %d times, want a new check for a new overridden revision", len(cp.checked))
	}
}

func TestEvaluate_NoConflictCheckerNoCheck(t *testing.T) {
	e, err := ruleeval.New(&batchProvider{})
	if err != nil {
		t.Fatal(err)
	}
	if got := conflicts(e.Evaluate(context.Background(), []ruleeval.RuleInput{draftOverride("SAV-001")}, "diff")); len(got) != 0 {
		t.Errorf("conflicts = %v, want none", got)
	}
}

func TestConflictCacheKey_FollowsWhatIsSent(t *testing.T) {
	base := draftOverride("SAV-001")
	key := ruleeval.ConflictCacheKey(base, "m", "c")
	noted := draftOverride("SAV-001")
	noted.Notes = []entity.Note{{Type: "clarification", Content: "Joint accounts only."}}
	if ruleeval.ConflictCacheKey(noted, "m", "c") == key {
		t.Error("a clarification note must change the conflict cache key")
	}
	if ruleeval.ConflictCacheKey(base, "m", "c") != ruleeval.ConflictCacheKey(draftOverride("SAV-001"), "m", "c") {
		t.Error("the conflict cache key must be stable")
	}
}

// TestEvaluate_NoConflictCheckOnceReplaced checks that a Draft successor of a
// Published override, whose target is already replaced, is not checked.
func TestEvaluate_NoConflictCheckOnceReplaced(t *testing.T) {
	cp := &conflictProvider{conflicts: map[string]bool{"SAV-002": true}}
	e, err := ruleeval.New(cp)
	if err != nil {
		t.Fatal(err)
	}
	in := draftOverride("SAV-002")
	in.OverriddenReplaced = true
	e.Evaluate(context.Background(), []ruleeval.RuleInput{in}, "diff")
	if len(cp.checked) != 0 {
		t.Errorf("checked %v, want nothing", cp.checked)
	}
}
