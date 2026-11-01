package entityops

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// maxNoteNumber is the maximum note number supported by the 2-digit ID format.
const maxNoteNumber = 99

// AddNote appends a note at revision 0 to ruleID and returns the new note ID.
// The ID is <rule_id>/<NN> with NN one past the highest number already used
// under that rule (NTE-011). Like the rule operations, it acts on f alone: for
// a rule of a sub-entity, pass that sub-entity's File and bump its ancestors.
func AddNote(f *entity.File, ruleID, noteType, content, addedBy string) (string, error) {
	if _, err := findRule(f, ruleID); err != nil {
		return "", err
	}
	if !entity.IsValidNoteType(noteType) {
		return "", fmt.Errorf("invalid note type %q: must be one of %s", noteType, strings.Join(entity.NoteTypes(), ", "))
	}
	if content == "" {
		return "", fmt.Errorf("note content must not be empty")
	}
	if addedBy == "" {
		return "", fmt.Errorf("added_by must not be empty")
	}
	if noteType == entity.NoteTypeStandard {
		if _, _, err := entity.ParseStandardNote(content); err != nil {
			return "", err
		}
	}

	num := nextNoteNumber(f, ruleID)
	if num > maxNoteNumber {
		return "", fmt.Errorf("cannot add note: maximum number of notes (%d) exceeded for rule %q", maxNoteNumber, ruleID)
	}
	noteID := fmt.Sprintf("%s/%02d", ruleID, num)

	f.Notes = append(f.Notes, entity.Note{
		ID:       noteID,
		RuleRef:  ruleID,
		Type:     noteType,
		Content:  content,
		AddedBy:  addedBy,
		AddedAt:  nowFunc().Format(dateFormat),
		Revision: 0,
	})

	v, err := BumpPatch(f.RuleSet.Version)
	if err != nil {
		return "", fmt.Errorf("bumping version: %w", err)
	}
	f.RuleSet.Version = v
	if err := UpdateMeta(f); err != nil {
		return "", fmt.Errorf("updating metadata: %w", err)
	}
	return noteID, nil
}

// UpdateNoteContent revises a note's content in place and increments its
// revision (NTE-008). It is UpdateNote with the type left unchanged, except
// that empty content is an error once the note is found and not append-only.
func UpdateNoteContent(f *entity.File, noteID, content string) error {
	if content == "" {
		n, err := findNote(f, noteID)
		if err != nil {
			return err
		}
		if !entity.IsAppendOnlyNoteType(n.Type) {
			return fmt.Errorf("note content must not be empty")
		}
	}
	return UpdateNote(f, noteID, content, "")
}

// UpdateNote revises a note's content and/or type in place and increments its
// revision once (NTE-008). An empty content or noteType keeps the current
// value; at least one of them must change. Notes of an append-only type
// cannot be changed, and no note can be retyped into one (NTE-010). It acts
// on f alone, like AddNote.
func UpdateNote(f *entity.File, noteID, content, noteType string) error {
	n, err := findNote(f, noteID)
	if err != nil {
		return err
	}
	if entity.IsAppendOnlyNoteType(n.Type) {
		return fmt.Errorf("note %q has type %q: notes of this type cannot change once added", noteID, n.Type)
	}
	if content == "" {
		content = n.Content
	}
	if noteType == "" {
		// Keep the current type as is, even one outside the enum (RL010
		// reports that), so a content-only edit is never refused for it.
		noteType = n.Type
	} else {
		if !entity.IsValidNoteType(noteType) {
			return fmt.Errorf("invalid note type %q: must be one of %s", noteType, strings.Join(entity.NoteTypes(), ", "))
		}
		if entity.IsAppendOnlyNoteType(noteType) {
			return fmt.Errorf("cannot retype note %q to append-only type %q: add a new note instead", noteID, noteType)
		}
	}
	if content == n.Content && noteType == n.Type {
		return fmt.Errorf("note %q unchanged: content and type are the same", noteID)
	}
	if err := checkStandardChange(n, content, noteType); err != nil {
		return err
	}

	n.Content = content
	n.Type = noteType
	n.Revision++

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

// checkStandardChange refuses a change to a note that a `standard` note may not
// undergo: a retype other than to or from `reference` (NTE-017), a withdrawal
// that changes the content (NTE-019), or malformed content (NTE-014).
func checkStandardChange(n *entity.Note, content, noteType string) error {
	if noteType == entity.NoteTypeStandard {
		if _, _, err := entity.ParseStandardNote(content); err != nil {
			return fmt.Errorf("note %q: %w", n.ID, err)
		}
	}
	if n.Type == noteType || (n.Type != entity.NoteTypeStandard && noteType != entity.NoteTypeStandard) {
		return nil
	}
	if n.Type != entity.NoteTypeReference && noteType != entity.NoteTypeReference {
		return fmt.Errorf("cannot retype note %q from %q to %q: a `standard` note changes only to or from `reference`", n.ID, n.Type, noteType)
	}
	if noteType == entity.NoteTypeReference && content != n.Content {
		return fmt.Errorf("note %q: withdrawing a mapping keeps the note's content", n.ID)
	}
	return nil
}

// nextNoteNumber returns one past the highest note number used under ruleID.
func nextNoteNumber(f *entity.File, ruleID string) int {
	max := 0
	prefix := ruleID + "/"
	for _, n := range f.Notes {
		if n.RuleRef != ruleID || !strings.HasPrefix(n.ID, prefix) {
			continue
		}
		if num, err := strconv.Atoi(strings.TrimPrefix(n.ID, prefix)); err == nil && num > max {
			max = num
		}
	}
	return max + 1
}

// findNote returns a pointer to the note with the given ID, or an error.
func findNote(f *entity.File, noteID string) (*entity.Note, error) {
	if f == nil {
		return nil, fmt.Errorf("entity file is nil")
	}
	for i := range f.Notes {
		if f.Notes[i].ID == noteID {
			return &f.Notes[i], nil
		}
	}
	return nil, fmt.Errorf("note %q not found", noteID)
}
