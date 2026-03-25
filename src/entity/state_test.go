package entity

import "testing"

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
