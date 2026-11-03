package entityops

import (
	"fmt"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// CheckAddedBy returns an error unless addedBy may be written as the
// `added_by` of a new Rule or Note. It must be a contributor identifier such
// as `github:<login>` (CTRB-003); while the format is in transition (#248), a
// GitHub @handle or an email address is accepted too, as rule-diff RD019
// accepts it. When contributors is not empty, addedBy must name one of them
// (CTRB-005), as entity.FindContributor matches it. Pass the list
// entity.RealmContributors returns; AddRule, AddNote and SupersedeRule pass
// none and so check the format only.
func CheckAddedBy(addedBy string, contributors []entity.Contributor) error {
	if addedBy == "" {
		return fmt.Errorf("added_by must not be empty")
	}
	if !entity.ValidAddedBy(addedBy) {
		_, _, err := entity.ParseContributorID(addedBy)
		return fmt.Errorf("invalid added_by: %w", err)
	}
	if len(contributors) > 0 && entity.FindContributor(contributors, addedBy) == nil {
		return fmt.Errorf("added_by %q names none of the Realm's contributors (CTRB-005)", addedBy)
	}
	return nil
}

// RewriteAddedBy replaces the `added_by` of every Rule and Note of f and of
// its sub-entities that is a key of m with its value, and returns how many it
// replaced. Each value must be a contributor identifier (CTRB-001, CTRB-002).
// Revisions stay as they are: `added_by` is not part of the body or content a
// revision counts. Every file whose subtree changed, f included, gets a patch
// bump and refreshed metadata, innermost first, as each hash covers its
// subtree. Nothing is changed when m holds an invalid value; on a later error,
// such as a version that does not parse, f may be partly changed and should
// be discarded.
func RewriteAddedBy(f *entity.File, m map[string]string) (int, error) {
	if f == nil {
		return 0, fmt.Errorf("entity file is nil")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k == "" {
			return 0, fmt.Errorf("cannot rewrite an empty added_by")
		}
		if _, _, err := entity.ParseContributorID(m[k]); err != nil {
			return 0, fmt.Errorf("rewriting %q: %w", k, err)
		}
	}
	return rewriteAddedBy(f, m)
}

func rewriteAddedBy(f *entity.File, m map[string]string) (int, error) {
	n := 0
	for i := range f.SubEntities {
		c, err := rewriteAddedBy(&f.SubEntities[i], m)
		if err != nil {
			return n, err
		}
		n += c
	}
	own := 0
	for i := range f.Rules {
		if to, ok := m[f.Rules[i].AddedBy]; ok && to != f.Rules[i].AddedBy {
			f.Rules[i].AddedBy = to
			own++
		}
	}
	for i := range f.Notes {
		if to, ok := m[f.Notes[i].AddedBy]; ok && to != f.Notes[i].AddedBy {
			f.Notes[i].AddedBy = to
			own++
		}
	}
	if own == 0 && n == 0 {
		return 0, nil
	}
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return n + own, fmt.Errorf("bumping version of %s: %w", f.Entity.ID, err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return n + own, fmt.Errorf("updating metadata of %s: %w", f.Entity.ID, err)
	}
	return n + own, nil
}
