package entityops

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// SupersedeRule creates a new Draft rule that supersedes an existing rule.
// The superseded rule must be in state P, I, or R.
func SupersedeRule(f *entity.File, ruleID, body, addedBy string) (string, error) {
	if body == "" {
		return "", fmt.Errorf("rule body must not be empty")
	}
	if addedBy == "" {
		return "", fmt.Errorf("added_by must not be empty")
	}

	old, err := findRule(f, ruleID)
	if err != nil {
		return "", err
	}

	switch old.State {
	case StatePublished, StateImplemented, StateRetired:
		// allowed
	default:
		return "", fmt.Errorf("rule %q is in state %q: only Published, Implemented, or Retired rules can be superseded", ruleID, old.State)
	}

	if err := ValidateEUID(f.Entity.ID); err != nil {
		return "", fmt.Errorf("invalid entity ID: %w", err)
	}

	num := NextRuleNumber(f)
	if num > maxRuleNumber {
		return "", fmt.Errorf("cannot add rule: maximum number of rules (%d) exceeded for entity %q", maxRuleNumber, f.Entity.ID)
	}
	newID := fmt.Sprintf("%s-%03d", f.Entity.ID, num)
	supersededID := ruleID

	f.Rules = append(f.Rules, entity.Rule{
		ID:         newID,
		Revision:   0,
		State:      StateDraft,
		Body:       body,
		AddedBy:    addedBy,
		AddedAt:    nowFunc().Format(dateFormat),
		Supersedes: &supersededID,
	})

	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return newID, nil
}
