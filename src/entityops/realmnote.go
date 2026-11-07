package entityops

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// maxRealmNoteNumber is the highest number the REALM/NNN form holds (NTE-030).
const maxRealmNoteNumber = 999

// realmNoteIDRe matches a Realm Note identifier, capturing its number.
var realmNoteIDRe = regexp.MustCompile(`^REALM/(\d{3})$`)

// AddRealmNote appends a Realm Note at revision 0 to rf and returns its ID,
// REALM/NNN with NNN one past the highest number already used (NTE-030). An
// exclusion has a ref whose <id> names a Standard Reference of rf with a realm
// (NTE-034); no other type may be given one, since a reference has a ref only
// when withdrawn from an exclusion (NTE-035). addedBy must pass CheckAddedBy
// against contributors: pass the list entity.RealmContributors returns for the
// manifest's directory, which for a sub-realm is its parent's (CTRB-006), not
// rf.Realm.Contributors.
//
// The Realm's version is left as is, and so is its hash: the caller recomputes
// the hash when it writes the manifest (RLMMNFST-014).
func AddRealmNote(rf *entity.RealmFile, noteType, content, ref, addedBy string, contributors []entity.Contributor) (string, error) {
	if rf == nil {
		return "", errors.New("realm file is nil")
	}
	if !entity.IsValidRealmNoteType(noteType) {
		return "", fmt.Errorf("invalid Realm Note type %q: must be one of %s", noteType, strings.Join(entity.RealmNoteTypes(), ", "))
	}
	if content == "" {
		return "", errors.New("note content must not be empty")
	}
	if noteType == entity.NoteTypeExclusion {
		if err := entity.CheckExclusionRef(ref, rf.Realm.Standards); err != nil {
			return "", fmt.Errorf("exclusion %w (NTE-034)", err)
		}
	} else if ref != "" {
		return "", fmt.Errorf("a ref is not allowed on a Realm Note of type %q: only an exclusion takes one (NTE-034, NTE-035)", noteType)
	}
	if err := CheckAddedBy(addedBy, contributors); err != nil {
		return "", err
	}
	num := nextRealmNoteNumber(rf.Notes)
	if num > maxRealmNoteNumber {
		return "", fmt.Errorf("cannot add Realm Note: maximum number of Realm Notes (%d) exceeded", maxRealmNoteNumber)
	}
	id := fmt.Sprintf("REALM/%03d", num)
	rf.Notes = append(rf.Notes, entity.RealmNote{
		ID:       id,
		Type:     noteType,
		Ref:      ref,
		Content:  content,
		AddedBy:  addedBy,
		AddedAt:  nowFunc().Format(dateFormat),
		Revision: 0,
	})
	return id, nil
}

// UpdateRealmNote revises the content and/or type of a Realm Note in place and
// increments its revision once (NTE-008). An empty content or noteType keeps
// the current value; at least one of them must change. The only change of type
// is the withdrawal of an exclusion, which retypes it to reference and keeps
// its ref and content (NTE-035). A ref never changes, and a Realm Note is never
// removed (NTE-009). Like AddRealmNote, it leaves the version and hash to the
// caller.
func UpdateRealmNote(rf *entity.RealmFile, id, content, noteType string) error {
	n, err := findRealmNote(rf, id)
	if err != nil {
		return err
	}
	if content == "" {
		content = n.Content
	}
	if noteType == "" {
		// Keep the current type as is, even one outside the enum (RM002
		// reports that), so a content-only edit is never refused for it.
		noteType = n.Type
	} else if !entity.IsValidRealmNoteType(noteType) {
		return fmt.Errorf("invalid Realm Note type %q: must be one of %s", noteType, strings.Join(entity.RealmNoteTypes(), ", "))
	}
	if content == n.Content && noteType == n.Type {
		return fmt.Errorf("note %q unchanged: content and type are the same", id)
	}
	if noteType != n.Type {
		if n.Type != entity.NoteTypeExclusion || noteType != entity.NoteTypeReference {
			return fmt.Errorf("cannot retype Realm Note %q from %q to %q: the only change of type is withdrawing an exclusion to reference (NTE-035)", id, n.Type, noteType)
		}
		if content != n.Content {
			return fmt.Errorf("note %q: withdrawing an exclusion keeps its content (NTE-035)", id)
		}
	}
	n.Content = content
	n.Type = noteType
	n.Revision++
	return nil
}

// nextRealmNoteNumber returns one past the highest REALM/NNN number in notes.
func nextRealmNoteNumber(notes []entity.RealmNote) int {
	highest := 0
	for _, n := range notes {
		if m := realmNoteIDRe.FindStringSubmatch(n.ID); m != nil {
			num, _ := strconv.Atoi(m[1]) // Three digits always parse.
			highest = max(highest, num)
		}
	}
	return highest + 1
}

// findRealmNote returns a pointer to the Realm Note of rf with the given ID,
// or an error.
func findRealmNote(rf *entity.RealmFile, id string) (*entity.RealmNote, error) {
	if rf == nil {
		return nil, errors.New("realm file is nil")
	}
	for i := range rf.Notes {
		if rf.Notes[i].ID == id {
			return &rf.Notes[i], nil
		}
	}
	return nil, fmt.Errorf("note %q not found", id)
}
