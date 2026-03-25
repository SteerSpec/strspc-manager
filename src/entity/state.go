package entity

import "fmt"

// Rule lifecycle state constants.
const (
	StateDraft       = "D"
	StateAbandoned   = "A"
	StatePublished   = "P"
	StateImplemented = "I"
	StateRetired     = "R"
	StateTerminated  = "T"
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
