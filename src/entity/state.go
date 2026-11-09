package entity

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Rule lifecycle state constants.
const (
	// StateDraft marks a rule that is being written and may still change
	// freely.
	StateDraft = "D"
	// StateAbandoned marks a draft that was dropped before publication; the
	// state is terminal.
	StateAbandoned = "A"
	// StatePublished marks a rule that is agreed upon but not yet implemented.
	StatePublished = "P"
	// StateImplemented marks a rule that is in force and enforced.
	StateImplemented = "I"
	// StateRetired marks a rule that is no longer in force, typically because
	// another rule supersedes it.
	StateRetired = "R"
	// StateTerminated marks a retired rule that is closed for good; the state
	// is terminal.
	StateTerminated = "T"
)

// validStates is the set of recognised rule lifecycle state codes.
var validStates = map[string]bool{
	StateDraft:       true,
	StateAbandoned:   true,
	StatePublished:   true,
	StateImplemented: true,
	StateRetired:     true,
	StateTerminated:  true,
}

// validTransitions defines the allowed forward-only state machine.
//
//	D → P (Published)    D → A (Abandoned, terminal)
//	P → I (Implemented)
//	I → R (Retired)
//	R → T (Terminated, terminal)
var validTransitions = map[string]map[string]bool{
	StateDraft:       {StatePublished: true, StateAbandoned: true},
	StatePublished:   {StateImplemented: true},
	StateImplemented: {StateRetired: true},
	StateRetired:     {StateTerminated: true},
}

// terminalStates are states from which no further transition is allowed.
var terminalStates = map[string]bool{
	StateAbandoned:  true,
	StateTerminated: true,
}

// IsValidState reports whether s is a recognised rule lifecycle state code.
func IsValidState(s string) bool {
	return validStates[s]
}

// IsTerminalState reports whether s is a terminal state from which
// no further transition is allowed.
func IsTerminalState(s string) bool {
	return terminalStates[s]
}

// IsLiveStandardRule reports whether r is a live Rule of a Standard Realm
// (STDRLM-014): not Abandoned, Retired or Terminated, and not a Draft Rule that
// supersedes another. A Draft transcription fix doesn't count until it is
// Published, and the Rule it supersedes stays live until then.
func IsLiveStandardRule(r Rule) bool {
	switch r.State {
	case StateAbandoned, StateRetired, StateTerminated:
		return false
	case StateDraft:
		return r.Supersedes == nil
	}
	return true
}

// ValidateTransition checks whether a state transition is allowed.
func ValidateTransition(from, to string) error {
	if terminalStates[from] {
		return fmt.Errorf("state %q is terminal: no further transitions allowed", from)
	}
	if targets, ok := validTransitions[from]; ok {
		if targets[to] {
			return nil
		}
	}
	return fmt.Errorf("invalid transition from %q to %q", from, to)
}

// ProfileSetKey returns a key that is equal for two `profiles` lists when they
// name the same profiles in any order. Profiles are unique (RUL-016), so the
// rules of one clause compare them as sets (STDRLM-009).
func ProfileSetKey(profiles []string) string {
	sorted := slices.Clone(profiles)
	slices.Sort(sorted)
	quoted := make([]string, len(sorted))
	for i, p := range sorted {
		quoted[i] = strconv.Quote(p)
	}
	return strings.Join(quoted, ",")
}
