package entity

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// The relations a Standard Reference can declare (STDREF-004).
const (
	// RelationImplements claims coverage of the standard (STDREF-005).
	RelationImplements = "implements"
	// RelationDerivedFrom cites the standard as a source and claims no
	// coverage (STDREF-006).
	RelationDerivedFrom = "derived_from"
)

// standardIDRe is the grammar of a Standard Reference `id` (STDREF-002).
var standardIDRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*(\.[a-z0-9]+(-[a-z0-9]+)*)+$`)

// StandardRef is a citation of an external standard, declared by a Realm or
// an Entity (STDREF-001).
type StandardRef struct {
	// ID names the standard, as a namespaced slug (STDREF-002). There is no
	// central registry.
	ID string `json:"id"`
	// Relation is one of the Relation* constants (STDREF-004).
	Relation string `json:"relation"`
	// Version is the edition of the standard, a free string (STDREF-007).
	// Empty means the reference names no edition. The schema rejects an empty
	// value in a file.
	Version string `json:"version,omitempty"`
	// URL is the absolute URL of the standard (STDREF-008).
	URL string `json:"url,omitempty"`
}

// ValidateStandardRef checks one reference against STDREF-002, STDREF-004 and
// STDREF-008.
func ValidateStandardRef(ref StandardRef) error {
	if !standardIDRe.MatchString(ref.ID) {
		return fmt.Errorf("standard id %q: must be a dot-separated lowercase slug (STDREF-002)", ref.ID)
	}
	if ref.Relation != RelationImplements && ref.Relation != RelationDerivedFrom {
		return fmt.Errorf("standard %s: relation %q must be %s or %s (STDREF-004)", ref.ID, ref.Relation, RelationImplements, RelationDerivedFrom)
	}
	if ref.URL != "" {
		u, err := url.Parse(ref.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
			return fmt.Errorf("standard %s: url %q must be an absolute http or https URL (STDREF-008)", ref.ID, ref.URL)
		}
	}
	return nil
}

// ValidateStandardRefs checks each reference of a `standards` array and that
// no `id` repeats (STDREF-003).
func ValidateStandardRefs(refs []StandardRef) []error {
	var errs []error
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if err := ValidateStandardRef(ref); err != nil {
			errs = append(errs, err)
		}
		if seen[ref.ID] {
			errs = append(errs, fmt.Errorf("standard id %q is declared more than once (STDREF-003)", ref.ID))
		}
		seen[ref.ID] = true
	}
	return errs
}

// ParseStandardNote splits the content of a `standard` Note at its first colon
// into the standard's id and the clause (NTE-014).
func ParseStandardNote(content string) (id, clause string, err error) {
	id, clause, ok := strings.Cut(content, ":")
	if !ok || !standardIDRe.MatchString(id) || !validClause(clause) {
		return "", "", fmt.Errorf("content %q must be <id>:<clause> (NTE-014)", content)
	}
	return id, clause, nil
}

// validClause reports whether a clause is printable and not blank, with nothing
// around it. Clauses identify what a report groups (STDREF-011), so two that
// read the same must be the same string.
func validClause(clause string) bool {
	return clause != "" && strings.TrimSpace(clause) == clause && strings.IndexFunc(clause, func(r rune) bool {
		return !unicode.IsPrint(r)
	}) < 0
}

// FindStandard returns the reference with the given id, or nil.
func FindStandard(refs []StandardRef, id string) *StandardRef {
	for i := range refs {
		if refs[i].ID == id {
			return &refs[i]
		}
	}
	return nil
}

// ParentRealm returns the manifest of the Realm that lists dir as a sub-realm
// and whose ID makes childID that sub-realm's ID (RLM-011), or nil when there is
// none on disk.
func ParentRealm(dir, childID string) *RealmFile {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil
	}
	path := filepath.Join(filepath.Dir(abs), "realm.json")
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	parent, err := LoadRealm(path)
	base := filepath.Base(abs)
	if err != nil || !slices.Contains(parent.SubRealms, base) || parent.Realm.ID+"."+base != childID {
		return nil
	}
	return parent
}

// ResolveStandard returns the first reference with the given id, searching
// each scope in turn, nearest first (NTE-016).
func ResolveStandard(id string, scopes ...[]StandardRef) *StandardRef {
	for _, refs := range scopes {
		if ref := FindStandard(refs, id); ref != nil {
			return ref
		}
	}
	return nil
}
