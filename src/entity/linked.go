package entity

import (
	"fmt"
	"slices"
)

// StandardRealm is what a Standard Reference with a `realm` reads of the
// Standard Realm it links: its edition (STDREF-018), its profiles (STDREF-027)
// and its clause list (STDREF-020, STDREF-021).
type StandardRealm struct {
	// ID is the realm_id of the Standard Realm.
	ID string
	// Edition is the edition it transcribes (STDRLM-002).
	Edition string
	// Profiles are the profiles it declares (STDRLM-004).
	Profiles []string
	// Clauses maps each clause of its clause list, the distinct clause
	// values of its live rules (STDRLM-006, STDRLM-014), to the profiles
	// those rules declare, sorted and unique. A profile narrows the list to
	// the clauses whose rules declare it (STDREF-020).
	Clauses map[string][]string
}

// LoadStandardRealm reads the clause list of the Standard Realm whose manifest
// rf is in dir. Its sub-realms are Realms of their own and are not read
// (STDRLM-006). An entity file that can't be read or parsed is an error, as the
// clause list would be short without it.
func LoadStandardRealm(dir string, rf *RealmFile) (*StandardRealm, error) {
	sr := &StandardRealm{
		ID:       rf.Realm.ID,
		Edition:  rf.Realm.Edition,
		Profiles: slices.Clone(rf.Realm.Profiles),
		Clauses:  make(map[string][]string),
	}
	err := WalkEntityFiles(dir, func(fpath string, _ []byte, ef *File, parseErr error) error {
		if parseErr != nil {
			return fmt.Errorf("standard realm %s: %s: %w", rf.Realm.ID, fpath, parseErr)
		}
		sr.addClauses(ef)
		return nil
	}, WithExcludeDirs(rf.SubRealms))
	if err != nil {
		return nil, err
	}
	for c, p := range sr.Clauses {
		slices.Sort(p)
		sr.Clauses[c] = slices.Compact(p)
	}
	return sr, nil
}

// addClauses adds the clauses of the live rules of ef and its sub-entities.
func (sr *StandardRealm) addClauses(ef *File) {
	for _, r := range ef.Rules {
		if r.Clause == "" || !IsLiveStandardRule(r) {
			continue
		}
		sr.Clauses[r.Clause] = append(sr.Clauses[r.Clause], r.Profiles...)
	}
	for i := range ef.SubEntities {
		sr.addClauses(&ef.SubEntities[i])
	}
}

// HasClause reports whether clause is in the clause list, matched exactly
// (STDREF-021).
func (sr *StandardRealm) HasClause(clause string) bool {
	_, ok := sr.Clauses[clause]
	return ok
}
