package inherit

import (
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// find returns the entry of set with the qualified ID id.
func find(t *testing.T, set []Rule, id string) Rule {
	t.Helper()
	for _, r := range set {
		if r.QualifiedID() == id {
			return r
		}
	}
	t.Fatalf("%s not in %v", id, ids(set))
	return Rule{}
}

// TestEffective_DraftOverrideReplacesNothing follows the example of ENT-020:
// A owns an Implemented R, B inherits A, C inherits B, and B's S overrides R.
func TestEffective_DraftOverrideReplacesNothing(t *testing.T) {
	a := hop("core", "A", rule("A-001", "I", nil))
	for _, tt := range []struct {
		state      string
		wantB      []string
		wantC      []string
		replaces   string
		enforcedAs string
	}{
		{"D", []string{"core:A-001", "lib:B-001"}, []string{"core:A-001"}, "", "D"},
		{"P", []string{"lib:B-001"}, []string{"lib:B-001"}, "core:A-001", "I"},
		{"I", []string{"lib:B-001"}, []string{"lib:B-001"}, "core:A-001", "I"},
	} {
		t.Run(tt.state, func(t *testing.T) {
			b := hop("lib", "B", rule("B-001", tt.state, ptr("core:A-001")))
			effB := Effective(b, []Hop{a}, nil)
			if !reflect.DeepEqual(ids(effB), tt.wantB) {
				t.Fatalf("Effective(B) = %v, want %v", ids(effB), tt.wantB)
			}
			s := find(t, effB, "lib:B-001")
			if s.Overrides != tt.replaces {
				t.Errorf("S Overrides = %q, want %q", s.Overrides, tt.replaces)
			}
			if s.Overridden == nil || s.Overridden.QualifiedID() != "core:A-001" {
				t.Errorf("S Overridden = %+v, want core:A-001 as context (RUL-020)", s.Overridden)
			}
			if got := s.EnforcedState(); got != tt.enforcedAs {
				t.Errorf("S enforced state = %q, want %q", got, tt.enforcedAs)
			}
			if got := ids(Effective(hop("app", "C"), []Hop{b, a}, nil)); !reflect.DeepEqual(got, tt.wantC) {
				t.Errorf("Effective(C) = %v, want %v", got, tt.wantC)
			}
		})
	}
}

// TestEnforcedState_FollowsTheChain checks RUL-021's recursion: a Published
// override of a Published override of an Implemented rule is enforced as
// Implemented, and one of a Published rule stays Published.
func TestEnforcedState_FollowsTheChain(t *testing.T) {
	a := hop("core", "A", rule("A-001", "I", nil), rule("A-002", "P", nil))
	b := hop("lib", "B", rule("B-001", "P", ptr("core:A-001")), rule("B-002", "P", ptr("core:A-002")))
	c := hop("app", "C", rule("C-001", "P", ptr("lib:B-001")), rule("C-002", "P", ptr("lib:B-002")))
	got := Effective(c, []Hop{b, a}, nil)
	for id, want := range map[string]string{"app:C-001": "I", "app:C-002": "P"} {
		if s := find(t, got, id).EnforcedState(); s != want {
			t.Errorf("%s enforced state = %q, want %q", id, s, want)
		}
	}
}

// TestEffective_StatesMatchTheEnforcedState checks that evaluate_on [I] keeps a
// Published override of an Implemented rule (SPCFG-010).
func TestEffective_StatesMatchTheEnforcedState(t *testing.T) {
	a := hop("core", "A", rule("A-001", "I", nil), rule("A-002", "P", nil))
	b := hop("lib", "B", rule("B-001", "P", ptr("core:A-001")), rule("B-002", "P", nil))
	if got, want := ids(Effective(b, []Hop{a}, []string{"I"})), []string{"core:A-002", "lib:B-001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Effective = %v, want %v", got, want)
	}
}

// TestEffective_DraftSuccessorKeepsTheContext checks that a Draft successor of
// a Published override, both overriding R, receives R as context although the
// Published one has already replaced it.
func TestEffective_DraftSuccessorKeepsTheContext(t *testing.T) {
	a := hop("core", "A", rule("A-001", "I", nil))
	succ := rule("B-002", "D", ptr("core:A-001"))
	succ.Supersedes = ptr("B-001")
	b := hop("lib", "B", rule("B-001", "P", ptr("core:A-001")), succ)
	got := Effective(b, []Hop{a}, nil)
	if want := []string{"lib:B-001", "lib:B-002"}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("Effective = %v, want %v", ids(got), want)
	}
	if o := find(t, got, "lib:B-002").Overridden; o == nil || o.QualifiedID() != "core:A-001" {
		t.Errorf("B-002 Overridden = %+v, want core:A-001", o)
	}
}

func TestEnforcedState_DefaultsToTheState(t *testing.T) {
	if got := (Rule{Rule: entity.Rule{State: "P"}}).EnforcedState(); got != "P" {
		t.Errorf("EnforcedState = %q, want P", got)
	}
}
