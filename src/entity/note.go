package entity

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Note types with their own handling in this module's callers.
const (
	// NoteTypeStandard marks a Note that maps its Rule to one clause of a
	// standard (NTE-013).
	NoteTypeStandard = "standard"
	// NoteTypeStructured marks a Note holding the machine-readable form of its
	// Rule (NTE-036), as `<format>:<payload>` (NTE-037).
	NoteTypeStructured = "structured"
	// NoteTypeReference marks a free-text reference, and a withdrawn mapping
	// (NTE-019) or exclusion (NTE-035).
	NoteTypeReference = "reference"
	// NoteTypeExclusion marks a Realm Note declaring one clause not applicable
	// to its Realm (NTE-033). Only a Realm Note has this type (NTE-032).
	NoteTypeExclusion = "exclusion"
	// NoteTypeRationale marks a Note explaining why.
	NoteTypeRationale = "rationale"
	// NoteTypeClarification marks a Note clarifying what is meant.
	NoteTypeClarification = "clarification"
)

// realmNoteTypes is the closed enum of Realm Note types (NTE-031), in schema
// order.
var realmNoteTypes = []string{NoteTypeExclusion, NoteTypeRationale, NoteTypeClarification, NoteTypeReference}

// RealmNoteTypes returns the closed enum of Realm Note types (NTE-031) in
// schema order. The slice is a fresh copy the caller may modify.
func RealmNoteTypes() []string {
	return append([]string(nil), realmNoteTypes...)
}

// IsValidRealmNoteType reports whether t is a Realm Note type (NTE-031).
func IsValidRealmNoteType(t string) bool {
	return slices.Contains(realmNoteTypes, t)
}

// realmNoteRefRe is the form of a Realm Note's ref, `<id>:<clause>`, as the
// realm schema states it.
var realmNoteRefRe = regexp.MustCompile(`^[^:]+:\S.*$`)

// CheckExclusionRef returns an error unless ref may be the ref of an exclusion
// in a Realm declaring standards: `<id>:<clause>` whose <id> names a Standard
// Reference with a realm (NTE-034). Whether the clause exists in that Realm
// needs the linked Realm, so it is not checked here. The error doesn't cite
// NTE-034: callers do.
func CheckExclusionRef(ref string, standards []StandardRef) error {
	if ref == "" {
		return errors.New("has no ref")
	}
	if !realmNoteRefRe.MatchString(ref) {
		return fmt.Errorf("ref %q is not <id>:<clause>", ref)
	}
	id, _, _ := strings.Cut(ref, ":")
	std := FindStandard(standards, id)
	switch {
	case std == nil:
		return fmt.Errorf("ref %q names standard %q, which the manifest doesn't declare", ref, id)
	case std.Realm == "":
		return fmt.Errorf("ref %q names standard %q, which links to no Standard Realm", ref, id)
	}
	return nil
}

// noteTypes is the closed enum of note types (SPEC §2.6), in schema order.
var noteTypes = []string{
	NoteTypeRationale,
	"example",
	"counter_example",
	NoteTypeReference,
	"applies_to",
	"changelog",
	NoteTypeClarification,
	"deprecation_notice",
	"supersedes",
	"extends",
	"related",
	NoteTypeStandard,
	NoteTypeStructured,
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
