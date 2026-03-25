package entityops

import (
	"fmt"

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

	r.State = target
	v, err := BumpMinor(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}
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
	default:
		return fmt.Errorf("rule %q is in state %q: cannot retire", ruleID, r.State)
	}

	if err := entity.ValidateTransition(r.State, target); err != nil {
		return err
	}

	r.State = target
	v, err := BumpMinor(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}
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

	r.State = entity.StateAbandoned
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata: %w", err)
	}
	return nil
}
