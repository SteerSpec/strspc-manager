package ruleeval_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// A skip is never silent, and never hides a rule that could block: relabelling
// rules as a standard must not turn their enforcement off (STDRLM-011).
func TestStandardRealmSkipsAreLoud(t *testing.T) {
	overrides := "dev.core:COR-001"
	blank := ""
	rule := func(state string, edit func(*entity.Rule)) ruleeval.RuleInput {
		in := standardInput("STD-001", state)
		in.Rule.Clause = "V1.1.1"
		if edit != nil {
			edit(&in.Rule)
		}
		return in
	}
	noClause := func(r *entity.Rule) { r.Clause = "" }
	blankClause := func(r *entity.Rule) { r.Clause = "  " }
	override := func(r *entity.Rule) { r.Overrides = &overrides }
	emptyOverride := func(r *entity.Rule) { r.Overrides = &blank }

	const warning, blocked = "RE005/warning", "RE006/error"
	tests := []struct {
		name     string
		in       ruleeval.RuleInput
		failOn   []string
		evaluate []string
		want     []string
	}{
		{"Published transcription", rule("P", nil), []string{"I"}, nil, []string{warning}},
		{"Draft transcription", rule("D", nil), []string{"I"}, nil, []string{warning}},
		{"empty override", rule("P", emptyOverride), []string{"I"}, nil, []string{warning}},
		{"Implemented", rule("I", nil), []string{"I"}, nil, []string{blocked}},
		{"Implemented, whatever fail_on says", rule("I", nil), nil, nil, []string{blocked}},
		{"Implemented, whatever evaluate_on says", rule("I", nil), nil, []string{"P"}, []string{blocked}},
		{"state in fail_on", rule("P", nil), []string{"I", "P"}, nil, []string{blocked}},
		{"no clause", rule("P", noClause), []string{"I"}, nil, []string{blocked}},
		{"blank clause", rule("D", blankClause), []string{"I"}, nil, []string{blocked}},
		{"overrides another rule", rule("P", override), []string{"I"}, nil, []string{blocked}},
		{"Terminated", rule("T", noClause), []string{"I"}, nil, nil},
		{"state outside evaluate_on", rule("D", nil), []string{"D"}, []string{"P"}, nil},
		{"code rule", makeInput("APP-001", "T"), []string{"I"}, nil, nil},
	}

	// Each way a run can end must report it: a change that only relabels
	// rules/ leaves an empty diff, since rules/ is not evaluated.
	runs := []struct {
		name   string
		static bool
		run    func(*testing.T, *ruleeval.Evaluator, []ruleeval.RuleInput) *result.Result
	}{
		{"diff", false, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			return e.Evaluate(t.Context(), in, "+ code\n")
		}},
		{"empty diff", false, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			return e.Evaluate(t.Context(), in, "")
		}},
		{"static-only", true, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			return e.Evaluate(t.Context(), in, "+ code\n")
		}},
		{"audit", false, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			res, err := e.Audit(t.Context(), in, auditFiles)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}},
		{"audit of no files", false, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			res, err := e.Audit(t.Context(), in, nil)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}},
		{"static-only audit", true, func(t *testing.T, e *ruleeval.Evaluator, in []ruleeval.RuleInput) *result.Result {
			res, err := e.Audit(t.Context(), in, auditFiles)
			if err != nil {
				t.Fatal(err)
			}
			return res
		}},
	}
	for _, r := range runs {
		for _, tc := range tests {
			t.Run(r.name+"/"+tc.name, func(t *testing.T) {
				opts := []ruleeval.Option{ruleeval.WithStaticOnly(r.static), ruleeval.WithFailOn(tc.failOn)}
				if tc.evaluate != nil {
					opts = append(opts, ruleeval.WithEvaluateStates(tc.evaluate))
				}
				e, err := ruleeval.New(auditMock{&mockProvider{}}, opts...)
				if err != nil {
					t.Fatal(err)
				}
				// The same rule twice, as a source listed twice gives it.
				var got []string
				for _, d := range r.run(t, e, []ruleeval.RuleInput{tc.in, tc.in}).Diagnostics {
					if d.Code != ruleeval.CodeStaticOnly && d.Code != ruleeval.CodeNoDiff {
						got = append(got, d.Code+"/"+d.Severity.String())
					}
				}
				if !slices.Equal(got, tc.want) {
					t.Errorf("got %v, want %v", got, tc.want)
				}
			})
		}
	}
}

// The message fits a real standard too: rule verify puts every state in
// fail_on, so it must not say only that the rules were relabelled.
func TestUnsafeSkipMessageFitsARealStandard(t *testing.T) {
	e, err := ruleeval.New(auditMock{&mockProvider{}}, ruleeval.WithFailOn([]string{"P"}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Audit(t.Context(), []ruleeval.RuleInput{standardInput("STD-001", "P")}, auditFiles)
	if err != nil || len(res.Diagnostics) != 1 {
		t.Fatalf("Audit = %v, %v, want one RE006", res, err)
	}
	if msg := res.Diagnostics[0].Message; !strings.Contains(msg, "a transcription can't be judged against code") {
		t.Errorf("message %q doesn't say what to do with a real standard", msg)
	}
}
