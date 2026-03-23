package entityops

import (
	"fmt"
	"regexp"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

var euidRe = regexp.MustCompile(`^[a-zA-Z0-9]{3,18}$`)

const dateFormat = "2006-01-02"

// ValidateEUID checks that an Entity Unique Identifier is 3-18 alphanumeric characters.
func ValidateEUID(id string) error {
	if !euidRe.MatchString(id) {
		return fmt.Errorf("invalid EUID %q: must be 3-18 alphanumeric characters", id)
	}
	return nil
}

// NewFile creates a new entity.File with version 0.1.0 and computed metadata.
func NewFile(id, title, desc, parent string) (*entity.File, error) {
	if err := ValidateEUID(id); err != nil {
		return nil, err
	}

	f := &entity.File{
		Schema: "./_schema/entity.v1.schema.json",
		Entity: entity.Entity{
			ID:          id,
			Title:       title,
			Description: desc,
			Parent:      parent,
		},
		RuleSet: entity.RuleSet{
			Version: "0.1.0",
		},
		Rules: []entity.Rule{},
		Notes: []entity.Note{},
	}

	if err := UpdateMeta(f); err != nil {
		return nil, fmt.Errorf("setting metadata: %w", err)
	}
	return f, nil
}

// AddRule appends a new Draft rule to the entity file and returns the new rule ID.
func AddRule(f *entity.File, body, addedBy string) (string, error) {
	if f == nil {
		return "", fmt.Errorf("entity file is nil")
	}
	if body == "" {
		return "", fmt.Errorf("rule body must not be empty")
	}
	if addedBy == "" {
		return "", fmt.Errorf("added_by must not be empty")
	}

	if err := ValidateEUID(f.Entity.ID); err != nil {
		return "", fmt.Errorf("invalid entity ID: %w", err)
	}

	num := NextRuleNumber(f)
	if num > maxRuleNumber {
		return "", fmt.Errorf("cannot add rule: maximum number of rules (%d) exceeded for entity %q", maxRuleNumber, f.Entity.ID)
	}
	ruleID := fmt.Sprintf("%s-%03d", f.Entity.ID, num)

	f.Rules = append(f.Rules, entity.Rule{
		ID:       ruleID,
		Revision: 0,
		State:    StateDraft,
		Body:     body,
		AddedBy:  addedBy,
		AddedAt:  nowFunc().Format(dateFormat),
	})

	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return ruleID, nil
}

// UpdateRuleBody changes the body of a Draft rule and increments its revision.
func UpdateRuleBody(f *entity.File, ruleID, body string) error {
	if body == "" {
		return fmt.Errorf("rule body must not be empty")
	}

	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}
	if r.State != StateDraft {
		return fmt.Errorf("rule %q is in state %q: only Draft rules can be edited", ruleID, r.State)
	}

	r.Body = body
	r.Revision++

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

// findRule returns a pointer to the rule with the given ID, or an error.
func findRule(f *entity.File, ruleID string) (*entity.Rule, error) {
	if f == nil {
		return nil, fmt.Errorf("entity file is nil")
	}
	for i := range f.Rules {
		if f.Rules[i].ID == ruleID {
			return &f.Rules[i], nil
		}
	}
	return nil, fmt.Errorf("rule %q not found", ruleID)
}
