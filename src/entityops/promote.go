package entityops

import (
	"fmt"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// PromoteRule advances a rule: D→P or P→I.
func PromoteRule(f *entity.File, ruleID string) error {
	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}

	var target string
	switch r.State {
	case entity.StateDraft:
		target = entity.StatePublished
	case entity.StatePublished:
		target = entity.StateImplemented
	default:
		return fmt.Errorf("rule %q is in state %q: cannot promote", ruleID, r.State)
	}

	if err := entity.ValidateTransition(r.State, target); err != nil {
		return err
	}

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpMinor(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}

	r.State = target
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	return nil
}

// RetireRule advances a rule: I→R or R→T.
func RetireRule(f *entity.File, ruleID string) error {
	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}

	var target string
	switch r.State {
	case entity.StateImplemented:
		target = entity.StateRetired
	case entity.StateRetired:
		target = entity.StateTerminated
	case entity.StatePublished:
		return fmt.Errorf("rule %q is in state %q: cannot retire; a superseded Published rule goes to Terminated with TerminateRule (RULST-012)", ruleID, r.State)
	default:
		return fmt.Errorf("rule %q is in state %q: cannot retire", ruleID, r.State)
	}

	if err := entity.ValidateTransition(r.State, target); err != nil {
		return err
	}

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpMinor(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}

	r.State = target
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	return nil
}

// TerminateRule moves a rule to Terminated: P→T or R→T. A Published rule may
// go to Terminated only once a rule of the same entity that supersedes it is
// Published or later (RULST-012): a Published rule's text can't be edited, so a
// wrong text is fixed by a superseding rule, and the old rule, never in force,
// skips Retired. R→T is the same move as RetireRule makes from Retired.
func TerminateRule(f *entity.File, ruleID string) error {
	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}

	switch r.State {
	case entity.StatePublished:
		if err := entity.ValidateSupersededTermination(ruleID, slices.Values(f.Rules)); err != nil {
			return err
		}
	case entity.StateRetired:
		if err := entity.ValidateTransition(r.State, entity.StateTerminated); err != nil {
			return err
		}
	default:
		return fmt.Errorf("rule %q is in state %q: only a Published rule that is superseded, or a Retired rule, can be terminated", ruleID, r.State)
	}

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpMinor(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}

	r.State = entity.StateTerminated
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	return nil
}

// AbandonRule transitions a Draft rule to Abandoned (D→A).
func AbandonRule(f *entity.File, ruleID string) error {
	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}

	if r.State != entity.StateDraft {
		return fmt.Errorf("rule %q is in state %q: only Draft rules can be abandoned", ruleID, r.State)
	}

	if err := entity.ValidateTransition(r.State, entity.StateAbandoned); err != nil {
		return err
	}

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}

	r.State = entity.StateAbandoned
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	return nil
}
