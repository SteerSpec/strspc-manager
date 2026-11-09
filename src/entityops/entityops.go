package entityops

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

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
	return AddRuleWith(f, body, addedBy, AddRuleOptions{})
}

// AddRuleOptions holds what a new rule may carry beyond its body.
type AddRuleOptions struct {
	// Clause names the clause of an external standard the rule transcribes
	// (RUL-015).
	Clause string
	// Profiles lists the profiles of its Standard Realm the rule belongs to
	// (RUL-016).
	Profiles []string
	// Realm is the manifest of the Realm holding the entity: a sub-realm's
	// own, since each Realm's kind is its own manifest's (RLMMNFST-009). Only
	// its kind and profiles are read. It is needed to set Clause or Profiles,
	// and when it is a Standard Realm's, AddRuleWith also checks what a live
	// rule of that Realm must have.
	Realm *entity.RealmMeta
}

// AddRuleWith appends a new Draft rule with the clause and profiles of opts
// and returns the new rule ID. It refuses:
//   - a clause or profiles unless opts.Realm is a Standard Realm (RUL-017);
//   - a blank clause or profile, a clause with leading or trailing spaces,
//     or a profile listed twice (RUL-015, RUL-016);
//   - a profile the Realm doesn't declare (STDRLM-008);
//   - in a Standard Realm, no clause (STDRLM-005), or no profiles when the
//     Realm declares some (STDRLM-018), since a new Draft rule is live
//     (STDRLM-014);
//   - a clause that a live rule of the entity f carries with other profiles
//     (STDRLM-009). Rules of the clause in its sub-entities and other files
//     are left to rule-lint (RL036).
func AddRuleWith(f *entity.File, body, addedBy string, opts AddRuleOptions) (string, error) {
	if f == nil {
		return "", fmt.Errorf("entity file is nil")
	}
	if body == "" {
		return "", fmt.Errorf("rule body must not be empty")
	}
	if err := CheckAddedBy(addedBy, nil); err != nil {
		return "", err
	}

	if err := ValidateEUID(f.Entity.ID); err != nil {
		return "", fmt.Errorf("invalid entity ID: %w", err)
	}
	if err := checkStandardFields(f, opts); err != nil {
		return "", err
	}

	num := NextRuleNumber(f)
	if num > maxRuleNumber {
		return "", fmt.Errorf("cannot add rule: maximum number of rules (%d) exceeded for entity %q", maxRuleNumber, f.Entity.ID)
	}
	ruleID := fmt.Sprintf("%s-%03d", f.Entity.ID, num)

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}

	f.Rules = append(f.Rules, entity.Rule{
		ID:       ruleID,
		Revision: 0,
		State:    entity.StateDraft,
		Body:     body,
		AddedBy:  addedBy,
		AddedAt:  nowFunc().Format(dateFormat),
		Clause:   opts.Clause,
		Profiles: slices.Clone(opts.Profiles),
	})

	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return ruleID, nil
}

// checkStandardFields checks the clause and profiles of a new rule of f.
func checkStandardFields(f *entity.File, opts AddRuleOptions) error {
	standard := opts.Realm != nil && opts.Realm.Kind == entity.KindStandard
	if !standard {
		if opts.Clause != "" || len(opts.Profiles) > 0 {
			return fmt.Errorf("a rule of entity %q can have a clause or profiles only in a Standard Realm (RUL-017)", f.Entity.ID)
		}
		return nil
	}
	if strings.TrimSpace(opts.Clause) == "" {
		return fmt.Errorf("a new rule of Standard Realm %q must have a clause (STDRLM-005)", opts.Realm.ID)
	}
	if strings.TrimSpace(opts.Clause) != opts.Clause {
		return fmt.Errorf("clause %q has leading or trailing spaces, which would make it another clause (RUL-015)", opts.Clause)
	}
	if len(opts.Realm.Profiles) > 0 && len(opts.Profiles) == 0 {
		return fmt.Errorf("a new rule of Standard Realm %q must declare profiles, one or more of %s (STDRLM-018)", opts.Realm.ID, strings.Join(opts.Realm.Profiles, ", "))
	}
	seen := make(map[string]bool)
	for _, p := range opts.Profiles {
		switch {
		case strings.TrimSpace(p) == "":
			return errors.New("a profile name must not be blank (RUL-016)")
		case seen[p]:
			return fmt.Errorf("profile %q is listed twice (RUL-016)", p)
		case !slices.Contains(opts.Realm.Profiles, p):
			return fmt.Errorf("profile %q is not declared in the profiles of Standard Realm %q (STDRLM-008)", p, opts.Realm.ID)
		}
		seen[p] = true
	}
	key := entity.ProfileSetKey(opts.Profiles)
	for _, r := range f.Rules {
		if r.Clause == opts.Clause && entity.IsLiveStandardRule(r) && entity.ProfileSetKey(r.Profiles) != key {
			return fmt.Errorf("rule %s carries clause %q with profiles [%s]; the live rules of a clause declare the same profiles (STDRLM-009)",
				r.ID, opts.Clause, strings.Join(r.Profiles, ", "))
		}
	}
	return nil
}

// UpdateRuleBody changes the body of a Draft rule and increments its revision.
func UpdateRuleBody(f *entity.File, ruleID, body string) error {
	r, err := findRule(f, ruleID)
	if err != nil {
		return err
	}
	if body == "" {
		return fmt.Errorf("rule body must not be empty")
	}
	if r.State != entity.StateDraft {
		return fmt.Errorf("rule %q is in state %q: only Draft rules can be edited", ruleID, r.State)
	}

	// Compute the new version first, so that a version that does not parse
	// fails before anything is edited.
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version: %w", err)
	}

	r.Body = body
	r.Revision++

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
