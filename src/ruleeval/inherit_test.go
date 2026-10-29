package ruleeval

import (
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

func TestDefaultStatesMatchInherit(t *testing.T) {
	if !reflect.DeepEqual(inherit.DefaultStates, DefaultEvaluateStates) {
		t.Errorf("inherit.DefaultStates = %v, DefaultEvaluateStates = %v", inherit.DefaultStates, DefaultEvaluateStates)
	}
}

func effRule(id, state string, overrides *string) entity.Rule {
	return entity.Rule{ID: id, State: state, Body: id, AddedBy: "t", AddedAt: "2026-10-29", Overrides: overrides}
}

func TestRuleInputsFromEffective(t *testing.T) {
	target := "core:ACCT-001"
	acct := inherit.Hop{RealmID: "core", Entity: &entity.File{
		Entity: entity.Entity{ID: "ACCT"},
		Rules:  []entity.Rule{effRule("ACCT-001", "I", nil), effRule("ACCT-002", "I", nil)},
		Notes:  []entity.Note{{ID: "ACCT-002/01", RuleRef: "ACCT-002", Type: "rationale", Content: "why"}},
	}}
	sav := inherit.Hop{RealmID: "prod", Entity: &entity.File{
		Entity: entity.Entity{ID: "SAV"},
		Rules:  []entity.Rule{effRule("SAV-001", "D", &target)},
	}}
	in := RuleInputsFromEffective("SAV", inherit.Effective(sav, []inherit.Hop{acct}, nil))
	if len(in) != 2 {
		t.Fatalf("got %d inputs, want 2: %+v", len(in), in)
	}
	inh, own := in[0], in[1]
	if inh.Rule.ID != "ACCT-002" || inh.EntityID != "SAV" || inh.Owner != "core:ACCT" || inh.InheritedBy != "SAV" || len(inh.Notes) != 1 {
		t.Errorf("inherited input = %+v", inh)
	}
	if own.Rule.ID != "SAV-001" || own.EntityID != "SAV" || own.Owner != "" || own.InheritedBy != "" || own.Overrides != target {
		t.Errorf("own input = %+v", own)
	}
}

func TestRuleInputsFromEffective_CollidingIDsGroupedApart(t *testing.T) {
	// Two realms of a chain reuse the EUID ACCT: the inherited ACCT-001
	// collides with the child's own ACCT-001 and is grouped apart.
	rules := []inherit.Rule{
		{Rule: effRule("ACCT-001", "I", nil), OwnerRealm: "dev.z", Owner: "ACCT", InheritedBy: "ACCT"},
		{Rule: effRule("ACCT-001", "D", nil), OwnerRealm: "dev.x", Owner: "ACCT"},
	}
	for _, order := range [][2]int{{0, 1}, {1, 0}} {
		in := RuleInputsFromEffective("ACCT", []inherit.Rule{rules[order[0]], rules[order[1]]})
		for _, r := range in {
			want := "ACCT"
			if r.InheritedBy != "" {
				want = "ACCT via dev.z:ACCT"
			}
			if r.EntityID != want {
				t.Errorf("order %v: %s (inherited=%v) group = %q, want %q", order, r.Rule.ID, r.InheritedBy != "", r.EntityID, want)
			}
		}
	}
}

func TestAnnotateOverriddenBy(t *testing.T) {
	inputs := []RuleInput{
		{Rule: effRule("ACCT-001", "I", nil), EntityID: "ACCT"},
		{Rule: effRule("ACCT-002", "I", nil), EntityID: "ACCT"},
		{Rule: effRule("ACCT-001", "I", nil), EntityID: "SAV", Owner: "core:ACCT", InheritedBy: "SAV"},
	}
	AnnotateOverriddenBy(inputs, "core", map[string][]string{"core:ACCT-001": {"prod:SAV-001"}})
	if !reflect.DeepEqual(inputs[0].OverriddenBy, []string{"prod:SAV-001"}) {
		t.Errorf("owner-context entry not annotated: %v", inputs[0].OverriddenBy)
	}
	if inputs[1].OverriddenBy != nil || inputs[2].OverriddenBy != nil {
		t.Errorf("unexpected annotations: %v, %v", inputs[1].OverriddenBy, inputs[2].OverriddenBy)
	}
}

func TestCacheKey_InheritanceMetadata(t *testing.T) {
	base := RuleInput{Rule: effRule("ACCT-001", "I", nil), EntityID: "ACCT"}
	key := CacheKey(base, "d", "m", "v")
	for name, in := range map[string]RuleInput{
		"inherited":     {Rule: base.Rule, EntityID: "SAV", Owner: "core:ACCT", InheritedBy: "SAV"},
		"overrides":     {Rule: base.Rule, EntityID: "ACCT", Overrides: "core:BASE-001"},
		"overridden by": {Rule: base.Rule, EntityID: "ACCT", OverriddenBy: []string{"prod:SAV-001"}},
	} {
		if CacheKey(in, "d", "m", "v") == key {
			t.Errorf("%s: cache key must differ from the owner-context entry", name)
		}
	}
	a := RuleInput{Rule: base.Rule, OverriddenBy: []string{"x:A-001", "x:B-001"}}
	b := RuleInput{Rule: base.Rule, OverriddenBy: []string{"x:B-001", "x:A-001"}}
	if CacheKey(a, "d", "m", "v") != CacheKey(b, "d", "m", "v") {
		t.Error("overridden-by order must not change the cache key")
	}
}
