package ruleeval_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

func overrideChain(state string) []inherit.Rule {
	target := "core:ACCT-001"
	acct := inherit.Hop{RealmID: "core", Entity: &entity.File{
		Entity: entity.Entity{ID: "ACCT"},
		Rules:  []entity.Rule{{ID: "ACCT-001", Revision: 2, State: "I", Body: "Accounts MUST have an owner."}},
	}}
	sav := inherit.Hop{RealmID: "prod", Entity: &entity.File{
		Entity: entity.Entity{ID: "SAV"},
		Rules:  []entity.Rule{{ID: "SAV-001", State: state, Body: "Savings accounts MUST have two owners.", Overrides: &target}},
	}}
	return inherit.Effective(sav, []inherit.Hop{acct}, nil)
}

func ownInput(t *testing.T, in []ruleeval.RuleInput) ruleeval.RuleInput {
	t.Helper()
	for _, r := range in {
		if r.Rule.ID == "SAV-001" {
			return r
		}
	}
	t.Fatalf("SAV-001 not in %+v", in)
	return ruleeval.RuleInput{}
}

// TestRuleInputsFromEffective_OverrideContext checks that an overriding rule
// carries the inherited rule as context, Draft or not (RUL-020), and its
// enforced state (RUL-024).
func TestRuleInputsFromEffective_OverrideContext(t *testing.T) {
	want := &inherit.Extended{ID: "core:ACCT-001", Revision: 2, Body: "Accounts MUST have an owner."}
	for _, tt := range []struct{ state, overrides, enforced string }{
		{"D", "", "D"},
		{"P", "core:ACCT-001", "I"},
	} {
		own := ownInput(t, ruleeval.RuleInputsFromEffective("SAV", overrideChain(tt.state)))
		if !reflect.DeepEqual(own.Overridden, want) {
			t.Errorf("%s: Overridden = %+v, want %+v", tt.state, own.Overridden, want)
		}
		if own.Overrides != tt.overrides {
			t.Errorf("%s: Overrides = %q, want %q", tt.state, own.Overrides, tt.overrides)
		}
		if got := own.EnforcedState(); got != tt.enforced {
			t.Errorf("%s: EnforcedState = %q, want %q", tt.state, got, tt.enforced)
		}
	}
}

// TestEvaluate_EnforcedStateDecides checks that evaluate_on and fail_on match
// the enforced state (SPCFG-010): a Published override of an Implemented rule
// is evaluated and fails as Implemented.
func TestEvaluate_EnforcedStateDecides(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"SAV-001": {Verdict: ruleeval.Violated, Explanation: "one owner"},
	}}
	e, err := ruleeval.New(bp, ruleeval.WithEvaluateStates([]string{"I"}), ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}
	in := entityInput("SAV", "SAV-001", "P")
	in.Enforced = "I"
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{in}, "diff")
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Severity != result.Error {
		t.Fatalf("diagnostics = %v, want one Error", res.Diagnostics)
	}
}

func TestCacheKey_OverrideContext(t *testing.T) {
	base := entityInput("SAV", "SAV-001", "P")
	key := ruleeval.CacheKey(base, "d", "m", "v")
	// The state, enforced or not, decides severity, never the verdict.
	enforced := base
	enforced.Enforced = "I"
	if ruleeval.CacheKey(enforced, "d", "m", "v") != key {
		t.Error("the enforced state must not change the cache key")
	}
	overridden := base
	overridden.Overridden = &inherit.Extended{ID: "core:ACCT-001", Body: "a"}
	edited := overridden
	edited.Overridden = &inherit.Extended{ID: "core:ACCT-001", Revision: 1, Body: "b"}
	keys := map[string]string{"base": key}
	for name, in := range map[string]ruleeval.RuleInput{"overridden": overridden, "edited": edited} {
		k := ruleeval.CacheKey(in, "d", "m", "v")
		for other, ok := range keys {
			if k == ok {
				t.Errorf("%s has the cache key of %s", name, other)
			}
		}
		keys[name] = k
	}
}

func TestEvaluate_PerRuleFallback_CarriesOverrideContext(t *testing.T) {
	mp := &mockProvider{}
	e, err := ruleeval.New(mp)
	if err != nil {
		t.Fatal(err)
	}
	in := entityInput("SAV", "SAV-001", "P")
	in.Enforced = "I"
	in.Overrides = "core:ACCT-001"
	in.Overridden = &inherit.Extended{ID: "core:ACCT-001", Revision: 2, Body: "b"}
	e.Evaluate(context.Background(), []ruleeval.RuleInput{in}, "diff")
	if len(mp.calls) != 1 || !reflect.DeepEqual(mp.calls[0].Input(), in) {
		t.Fatalf("calls = %+v, want one carrying %+v", mp.calls, in)
	}
}

// TestStandardSkip_ReadsTheEnforcedState checks that a skipped Standard Realm
// rule enforced as Implemented fails closed like an Implemented one.
func TestStandardSkip_ReadsTheEnforcedState(t *testing.T) {
	e, err := ruleeval.New(&batchProvider{})
	if err != nil {
		t.Fatal(err)
	}
	in := entityInput("STD", "STD-001", "P")
	in.Rule.Clause = "1.1"
	in.RealmKind = entity.KindStandard
	in.Enforced = "I"
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{in}, "diff")
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != ruleeval.CodeUnsafeSkip {
		t.Fatalf("diagnostics = %v, want one %s", res.Diagnostics, ruleeval.CodeUnsafeSkip)
	}
}
