package entityops

import (
	"fmt"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// SupersedeRule creates a new Draft rule that supersedes an existing rule.
// The superseded rule must be in state P, I, or R. The new rule keeps its
// clause and profiles: a Standard Realm's transcription fix has the clause of
// the rule it fixes (STDRLM-012), and the profiles the clause's rules share
// (STDRLM-009). Outside a Standard Realm a rule has neither (RUL-017); use
// SupersedeRuleWith to replace one of a code Realm that has them.
func SupersedeRule(f *entity.File, ruleID, body, addedBy string) (string, error) {
	return supersede(f, ruleID, body, addedBy, true)
}

// SupersedeOptions holds the Realm a rule is superseded in.
type SupersedeOptions struct {
	// Realm is the manifest of the Realm holding the entity, as for
	// AddRuleOptions.Realm. Only its kind is read.
	Realm *entity.RealmMeta
}

// SupersedeRuleWith is SupersedeRule knowing the Realm. The new rule keeps the
// clause and profiles of the rule it supersedes only when opts.Realm is a
// Standard Realm. Elsewhere it has neither (RUL-017), so a code Realm's rule
// that has them is repaired by superseding it: the old rule keeps them, which
// RUL-017 allows once it is Retired or Terminated.
func SupersedeRuleWith(f *entity.File, ruleID, body, addedBy string, opts SupersedeOptions) (string, error) {
	standard := opts.Realm != nil && opts.Realm.Kind == entity.KindStandard
	return supersede(f, ruleID, body, addedBy, standard)
}

// supersede adds the superseding rule, with the clause and profiles of the old
// one when keep is set.
func supersede(f *entity.File, ruleID, body, addedBy string, keep bool) (string, error) {
	old, err := findRule(f, ruleID)
	if err != nil {
		return "", err
	}
	if body == "" {
		return "", fmt.Errorf("rule body must not be empty")
	}
	if err := CheckAddedBy(addedBy, nil); err != nil {
		return "", err
	}

	switch old.State {
	case entity.StatePublished, entity.StateImplemented, entity.StateRetired:
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

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}

	r := entity.Rule{
		ID:         newID,
		Revision:   0,
		State:      entity.StateDraft,
		Body:       body,
		AddedBy:    addedBy,
		AddedAt:    nowFunc().Format(dateFormat),
		Supersedes: &supersededID,
	}
	if keep {
		r.Clause = old.Clause
		r.Profiles = slices.Clone(old.Profiles)
	}
	f.Rules = append(f.Rules, r)

	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return newID, nil
}
