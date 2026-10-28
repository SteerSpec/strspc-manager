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
// under that rule (NTE-011).
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

// UpdateNoteContent revises a note in place and increments its revision
// (NTE-008). Notes of an append-only type cannot be changed (NTE-010).
func UpdateNoteContent(f *entity.File, noteID, content string) error {
	n, err := findNote(f, noteID)
	if err != nil {
		return err
	}
	if entity.IsAppendOnlyNoteType(n.Type) {
		return fmt.Errorf("note %q has type %q: notes of this type cannot change once added", noteID, n.Type)
	}
	if content == "" {
		return fmt.Errorf("note content must not be empty")
	}
	if content == n.Content {
		return fmt.Errorf("note %q content unchanged", noteID)
	}

	n.Content = content
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
