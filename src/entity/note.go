package entity

// Note types with their own handling in this module's callers.
const (
	// NoteTypeStandard marks a Note that maps its Rule to one clause of a
	// standard (NTE-013).
	NoteTypeStandard = "standard"
	// NoteTypeReference marks a free-text reference, and a withdrawn mapping
	// (NTE-019).
	NoteTypeReference = "reference"
)

// noteTypes is the closed enum of note types (SPEC §2.6), in schema order.
var noteTypes = []string{
	"rationale",
	"example",
	"counter_example",
	"reference",
	"applies_to",
	"changelog",
	"clarification",
	"deprecation_notice",
	"supersedes",
	"extends",
	"related",
	NoteTypeStandard,
}

// validNoteTypes is the set form of noteTypes.
var validNoteTypes = func() map[string]bool {
	m := make(map[string]bool, len(noteTypes))
	for _, t := range noteTypes {
		m[t] = true
	}
	return m
}()

// appendOnlyNoteTypes are the note types that must not change once added
// (NTE-010).
var appendOnlyNoteTypes = map[string]bool{
	"changelog":  true,
	"supersedes": true,
	"extends":    true,
	"related":    true,
}

// NoteTypes returns the closed enum of note types in schema order. The slice
// is a fresh copy the caller may modify.
func NoteTypes() []string {
	return append([]string(nil), noteTypes...)
}

// IsValidNoteType reports whether t is one of the closed note types.
func IsValidNoteType(t string) bool {
	return validNoteTypes[t]
}

// IsAppendOnlyNoteType reports whether notes of type t are immutable once
// added (changelog, supersedes, extends, related).
func IsAppendOnlyNoteType(t string) bool {
	return appendOnlyNoteTypes[t]
}
