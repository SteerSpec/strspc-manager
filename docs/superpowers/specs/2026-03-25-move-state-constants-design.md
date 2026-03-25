# Move Rule Lifecycle State Constants from entityops to entity

**Date:** 2026-03-25
**GH Issue:** #45
**Status:** Design approved

## Problem

The rule lifecycle state constants (`StateDraft`, `StateAbandoned`, etc.) live in `entityops/state.go` — a package for mutation logic. These constants describe valid values of `entity.Rule.State`, making them data model concerns.

This forces packages like `ruleeval` to import `entityops` just for constants, creating an unnecessary dependency on the mutation layer. Additionally, three packages (`entityops`, `ruleeval`, `rulelint`) each maintain their own `validStates` map — a duplication that invites drift.

## Design

### New file: `entity/state.go`

All state-related code moves from `entityops/state.go` to `entity/state.go`.

**Exported constants** (same names, new package):

```go
const (
	StateDraft       = "D"
	StateAbandoned   = "A"
	StatePublished   = "P"
	StateImplemented = "I"
	StateRetired     = "R"
	StateTerminated  = "T"
)
```

**Accessor functions** (new — replace duplicated maps and expose the model):

```go
func IsValidState(s string) bool
func IsTerminalState(s string) bool
func ValidateTransition(from, to string) error
```

**Unexported internals** (implementation detail):

```go
var validStates = map[string]bool{ ... }
var terminalStates = map[string]bool{ ... }
var validTransitions = map[string]map[string]bool{ ... }
```

### Consumer changes

| File | Change |
|------|--------|
| `entityops/state.go` | Deleted |
| `entityops/promote.go` | `entityops.State*` → `entity.State*`, `ValidateTransition` → `entity.ValidateTransition` |
| `entityops/supersede.go` | Same constant rename |
| `entityops/entityops.go` | Same constant rename |
| `ruleeval/ruleeval.go` | Drop `entityops` import, delete local `validStates` map, use `entity.IsValidState()` |
| `rulediff/checks.go` | `entityops.State*` → `entity.State*`, drop `entityops` import if no other usage |
| `rulelint/rulelint.go` | Delete duplicated `validStates` map (lines 35-38), use `entity.IsValidState()` |
| Test files | Mechanical constant renames, no logic changes |

### New tests: `entity/state_test.go`

Table-driven tests moved from `entityops/entityops_test.go`:

- `TestIsValidState` — all 6 valid codes return true, invalid codes return false
- `TestIsTerminalState` — A and T true, others false
- `TestValidateTransition` — valid transitions succeed, invalid (skip, reverse, from terminal) return error

### Risk

The compiler catches every stale `entityops.State*` reference. No runtime risk.

## Decision rationale

The state machine is intrinsic to the model — a state without its valid transitions is incomplete. Accessor functions (`IsValidState`, `IsTerminalState`) are preferred over exported maps to prevent mutation and provide a self-documenting API.
