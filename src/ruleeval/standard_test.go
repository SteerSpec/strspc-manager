package ruleeval_test

import (
	"context"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// standardInput returns an input for a rule of a Standard Realm.
func standardInput(id, state string) ruleeval.RuleInput {
	in := makeInput(id, state)
	in.EntityID = "STD"
	in.RealmKind = entity.KindStandard
	return in
}

// STDRLM-011: neither a diff evaluation nor an audit sends a rule of a
// Standard Realm to the provider, whatever its state.
func TestStandardRealmRulesAreNotEvaluated(t *testing.T) {
	evaluate := func(ctx context.Context, e *ruleeval.Evaluator, in []ruleeval.RuleInput) error {
		e.Evaluate(ctx, in, "+ code\n")
		return nil
	}
	audit := func(ctx context.Context, e *ruleeval.Evaluator, in []ruleeval.RuleInput) error {
		_, err := e.Audit(ctx, in, auditFiles)
		return err
	}
	tests := []struct {
		name  string
		run   func(context.Context, *ruleeval.Evaluator, []ruleeval.RuleInput) error
		state string
		batch bool
	}{
		{"evaluate a Draft", evaluate, entity.StateDraft, true},
		{"evaluate a Published", evaluate, entity.StatePublished, true},
		{"evaluate an Implemented, which RL037 reports", evaluate, entity.StateImplemented, true},
		{"evaluate a Terminated, reported by its Realm and not by its state", evaluate, entity.StateTerminated, true},
		{"evaluate rule by rule", evaluate, entity.StatePublished, false},
		{"audit", audit, entity.StatePublished, true},
		{"audit rule by rule", audit, entity.StatePublished, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
				"STD-001": {Verdict: ruleeval.Compliant},
				"APP-001": {Verdict: ruleeval.Compliant},
			}}
			mp := &mockProvider{}
			var p ruleeval.Provider = auditMock{mp}
			if tc.batch {
				p = auditProvider{batchProvider: bp, version: "audit-test"}
			}
			obs := &recordingObserver{}
			e, err := ruleeval.New(p, ruleeval.WithObserver(obs))
			if err != nil {
				t.Fatal(err)
			}
			code := makeInput("APP-001", entity.StateImplemented)
			code.EntityID = "APP"
			if err := tc.run(t.Context(), e, []ruleeval.RuleInput{standardInput("STD-001", tc.state), code}); err != nil {
				t.Fatal(err)
			}

			var sent []string
			for _, b := range bp.batches {
				for _, in := range b.Inputs {
					sent = append(sent, in.Rule.ID)
				}
			}
			for _, c := range mp.calls {
				sent = append(sent, c.Rule.ID)
			}
			if !slices.Equal(sent, []string{"APP-001"}) {
				t.Errorf("sent %v to the provider, want [APP-001]", sent)
			}
			if n := obs.count(ruleeval.EventStandardRule); n != 1 {
				t.Errorf("got %d EventStandardRule, want 1", n)
			}
			if n := obs.count(ruleeval.EventSkipped); n != 0 {
				t.Errorf("got %d EventSkipped, want 0: that event is for the state", n)
			}
		})
	}
}

// A verdict cached before STDRLM-011 was enforced is not served.
func TestStandardRealmRulesIgnoreCachedVerdicts(t *testing.T) {
	const diff = "+ code\n"
	in := standardInput("STD-001", entity.StateImplemented)
	cache := newMemCache()
	cache.entries[ruleeval.CacheKey(in, ruleeval.DiffHash(diff), "test-model", "vtest")] =
		&ruleeval.EvalResponse{Verdict: ruleeval.Violated, Explanation: "cached"}
	e, err := ruleeval.New(&batchProvider{}, ruleeval.WithCache(cache), ruleeval.WithFailOn([]string{entity.StateImplemented}))
	if err != nil {
		t.Fatal(err)
	}
	if res := e.Evaluate(t.Context(), []ruleeval.RuleInput{in}, diff); len(res.Diagnostics) != 0 {
		t.Errorf("got %v, want no diagnostic", res.Diagnostics)
	}
}

// A rule of an effective set has the kind of the Realm owning it, which the
// annotation of the inheriting entity's file leaves alone.
func TestRealmKindFollowsTheOwner(t *testing.T) {
	tests := []struct {
		name          string
		child, parent string
	}{
		{"code inherits from standard", "", entity.KindStandard},
		{"standard inherits from code", entity.KindStandard, ""},
		{"code inherits from code", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			child := inherit.Hop{RealmID: "dev.child", Entity: &entity.File{
				Entity: entity.Entity{ID: "CHD"}, Rules: []entity.Rule{makeRule("CHD-001", entity.StatePublished)},
			}}
			parent := inherit.Hop{RealmID: "dev.parent", RealmKind: tc.parent, Entity: &entity.File{
				Entity: entity.Entity{ID: "PAR"}, Rules: []entity.Rule{makeRule("PAR-001", entity.StatePublished)},
			}}
			inputs := ruleeval.RuleInputsFromEffective("CHD", inherit.Effective(child, []inherit.Hop{parent}, nil))
			// The child's kind comes from its file alone here, as it does for
			// an entity without a chain.
			ruleeval.AnnotateRealmKind(inputs, tc.child)

			got := make(map[string]string, len(inputs))
			for _, in := range inputs {
				got[in.Rule.ID] = in.RealmKind
			}
			if got["CHD-001"] != tc.child || got["PAR-001"] != tc.parent {
				t.Errorf("kinds = %q, want CHD-001 %q and PAR-001 %q", got, tc.child, tc.parent)
			}
		})
	}
}
