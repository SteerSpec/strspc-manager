package entity

import (
	"slices"
	"testing"
)

func TestIsValidState(t *testing.T) {
	t.Parallel()

	valid := []string{StateDraft, StateAbandoned, StatePublished, StateImplemented, StateRetired, StateTerminated}
	for _, s := range valid {
		if !IsValidState(s) {
			t.Errorf("IsValidState(%q) = false, want true", s)
		}
	}

	invalid := []string{"X", "", "draft", "d", "published", "ZZ"}
	for _, s := range invalid {
		if IsValidState(s) {
			t.Errorf("IsValidState(%q) = true, want false", s)
		}
	}
}

func TestIsTerminalState(t *testing.T) {
	t.Parallel()

	terminal := []string{StateAbandoned, StateTerminated}
	for _, s := range terminal {
		if !IsTerminalState(s) {
			t.Errorf("IsTerminalState(%q) = false, want true", s)
		}
	}

	nonTerminal := []string{StateDraft, StatePublished, StateImplemented, StateRetired}
	for _, s := range nonTerminal {
		if IsTerminalState(s) {
			t.Errorf("IsTerminalState(%q) = true, want false", s)
		}
	}
}

func TestValidateTransition(t *testing.T) {
	t.Parallel()

	valid := []struct{ from, to string }{
		{StateDraft, StatePublished},
		{StateDraft, StateAbandoned},
		{StatePublished, StateImplemented},
		{StateImplemented, StateRetired},
		{StateRetired, StateTerminated},
	}
	for _, tc := range valid {
		if err := ValidateTransition(tc.from, tc.to); err != nil {
			t.Errorf("ValidateTransition(%q, %q) = %v, want nil", tc.from, tc.to, err)
		}
	}

	invalid := []struct{ from, to string }{
		{StateDraft, StateImplemented},     // skip
		{StatePublished, StateDraft},       // reverse
		{StateAbandoned, StatePublished},   // from terminal
		{StateTerminated, StateDraft},      // from terminal
		{StateImplemented, StatePublished}, // reverse
		{StateRetired, StateImplemented},   // reverse
		{StateDraft, StateRetired},         // skip
		{StatePublished, StateTerminated},  // skip
	}
	for _, tc := range invalid {
		if err := ValidateTransition(tc.from, tc.to); err == nil {
			t.Errorf("ValidateTransition(%q, %q) = nil, want error", tc.from, tc.to)
		}
	}
}

// Two profile sets are told apart whatever characters their names hold, and
// the same set matches in any order.
func TestProfileSetKey(t *testing.T) {
	if ProfileSetKey([]string{"a\x00b"}) == ProfileSetKey([]string{"a", "b"}) {
		t.Error(`["a\x00b"] and ["a", "b"] share a key`)
	}
	if ProfileSetKey([]string{"a,b"}) == ProfileSetKey([]string{"a", "b"}) {
		t.Error(`["a,b"] and ["a", "b"] share a key`)
	}
	if ProfileSetKey([]string{"L2", "L1"}) != ProfileSetKey([]string{"L1", "L2"}) {
		t.Error("the order of the names changed the key")
	}
}

// A Published rule may go to Terminated once a rule of its entity that
// supersedes it is Published or later (RULST-012).
func TestValidateSupersededTermination(t *testing.T) {
	t.Parallel()

	old := "TST-001"
	successor := func(id, state string) Rule {
		return Rule{ID: id, State: state, Supersedes: &old}
	}
	other := Rule{ID: "TST-009", State: StatePublished}

	for _, state := range []string{StatePublished, StateImplemented, StateRetired, StateTerminated} {
		rules := []Rule{{ID: old, State: StatePublished}, other, successor("TST-002", state)}
		if err := ValidateSupersededTermination(old, slices.Values(rules)); err != nil {
			t.Errorf("successor in %s: %v, want nil", state, err)
		}
	}

	// One qualifying successor is enough, whatever the others are.
	rules := []Rule{successor("TST-002", StateAbandoned), successor("TST-003", StatePublished)}
	if err := ValidateSupersededTermination(old, slices.Values(rules)); err != nil {
		t.Errorf("Abandoned and Published successors: %v, want nil", err)
	}

	tests := []struct {
		name  string
		rules []Rule
		want  string
	}{
		{"no successor", []Rule{{ID: old, State: StatePublished}, other},
			`rule "TST-001" is Published and no Published rule supersedes it (RULST-012)`},
		{"Draft successor", []Rule{successor("TST-002", StateDraft)},
			`rule "TST-001" is Published and no Published rule supersedes it: TST-002 is in state "D" (RULST-012)`},
		{"Abandoned and Draft successors", []Rule{successor("TST-003", StateDraft), successor("TST-002", StateAbandoned)},
			`rule "TST-001" is Published and no Published rule supersedes it: TST-002 is in state "A", TST-003 is in state "D" (RULST-012)`},
	}
	for _, tc := range tests {
		err := ValidateSupersededTermination(old, slices.Values(tc.rules))
		if err == nil || err.Error() != tc.want {
			t.Errorf("%s: got %v, want %q", tc.name, err, tc.want)
		}
	}
}
