package entityops

import (
	"fmt"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// CheckAddedBy returns an error unless addedBy may be written as the
// `added_by` of a new Rule or Note. It must be a contributor identifier such
// as `github:<login>` (CTRB-003); a GitHub @handle or an email address is
// rejected. When contributors is not empty, addedBy must name one of them
// (CTRB-005), as entity.FindContributor matches it. Pass the list
// entity.RealmContributors returns; AddRule, AddNote and SupersedeRule pass
// none and so check the format only.
func CheckAddedBy(addedBy string, contributors []entity.Contributor) error {
	if addedBy == "" {
		return fmt.Errorf("added_by must not be empty")
	}
	if _, _, err := entity.ParseContributorID(addedBy); err != nil {
		return fmt.Errorf("invalid added_by: %w", err)
	}
	if len(contributors) > 0 && entity.FindContributor(contributors, addedBy) == nil {
		return fmt.Errorf("added_by %q names none of the Realm's contributors (CTRB-005)", addedBy)
	}
	return nil
}

// KeptNote names an `override_ack` note that RewriteAddedByReport left as it
// was although its `added_by` is a key of the map: the note never changes once
// added (NTE-044), so it goes on naming the identifier AddedBy.
type KeptNote struct {
	// EntityID is the ID of the entity whose file holds the note.
	EntityID string
	// NoteID is the note's ID.
	NoteID string
	// AddedBy is the note's `added_by`, which stays as it is.
	AddedBy string
}

// AddedByRewrite is what RewriteAddedByReport did.
type AddedByRewrite struct {
	// Replaced is how many `added_by` values were replaced.
	Replaced int
	// Kept lists the `override_ack` notes left as they were, in file order,
	// each sub-entity's before its parent's.
	Kept []KeptNote
}

// RewriteAddedBy is RewriteAddedByReport returning only how many values it
// replaced.
func RewriteAddedBy(f *entity.File, m map[string]string) (int, error) {
	rw, err := RewriteAddedByReport(f, m)
	return rw.Replaced, err
}

// RewriteAddedByReport replaces the `added_by` of every Rule and Note of f and
// of its sub-entities that is a key of m with its value, except that it leaves
// a Note of type `override_ack` as it is and lists it in Kept (NTE-044/01).
// Each value must be a contributor identifier (CTRB-001, CTRB-002). Revisions
// stay as they are: `added_by` is not part of the body or content a revision
// counts. Every file whose subtree changed, f included, gets a patch bump and
// refreshed metadata, innermost first, as each hash covers its subtree; a
// file whose only matches are `override_ack` notes is left alone. Nothing is
// changed when m holds an invalid value; on a later error, such as a version
// that does not parse, f may be partly changed and should be discarded.
func RewriteAddedByReport(f *entity.File, m map[string]string) (AddedByRewrite, error) {
	var rw AddedByRewrite
	if f == nil {
		return rw, fmt.Errorf("entity file is nil")
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		if k == "" {
			return rw, fmt.Errorf("cannot rewrite an empty added_by")
		}
		if _, _, err := entity.ParseContributorID(m[k]); err != nil {
			return rw, fmt.Errorf("rewriting %q: %w", k, err)
		}
	}
	err := rewriteAddedBy(f, m, &rw)
	return rw, err
}

func rewriteAddedBy(f *entity.File, m map[string]string, rw *AddedByRewrite) error {
	before := rw.Replaced
	for i := range f.SubEntities {
		if err := rewriteAddedBy(&f.SubEntities[i], m, rw); err != nil {
			return err
		}
	}
	for i := range f.Rules {
		if to, ok := m[f.Rules[i].AddedBy]; ok && to != f.Rules[i].AddedBy {
			f.Rules[i].AddedBy = to
			rw.Replaced++
		}
	}
	for i := range f.Notes {
		n := &f.Notes[i]
		to, ok := m[n.AddedBy]
		if !ok || to == n.AddedBy {
			continue
		}
		if n.Type == entity.NoteTypeOverrideAck {
			rw.Kept = append(rw.Kept, KeptNote{EntityID: f.Entity.ID, NoteID: n.ID, AddedBy: n.AddedBy})
			continue
		}
		n.AddedBy = to
		rw.Replaced++
	}
	if rw.Replaced == before {
		return nil
	}
	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return fmt.Errorf("bumping version of %s: %w", f.Entity.ID, err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return fmt.Errorf("updating metadata of %s: %w", f.Entity.ID, err)
	}
	return nil
}
