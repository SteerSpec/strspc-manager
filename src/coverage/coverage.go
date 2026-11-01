// Package coverage reports how a Realm tree's rules map to the clauses of the
// external standards it cites (STDREF-010 to STDREF-016). It reports only what
// is mapped: SteerSpec holds no clause lists, so a clause nothing maps can't be
// named.
package coverage

import (
	"cmp"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
)

// Status is what a clause amounts to in an entity.
type Status string

// The statuses of a clause.
const (
	// StatusCovered is an `implements` clause on an Implemented rule in the
	// entity's coverage rule set (STDREF-012).
	StatusCovered Status = "covered"
	// StatusMapped is an `implements` clause on a rule of that set that doesn't
	// cover it (STDREF-013).
	StatusMapped Status = "mapped"
	// StatusLapsed is an `implements` clause neither covered nor mapped, whose
	// rule was retired or replaced (STDREF-014, STDREF-015).
	StatusLapsed Status = "lapsed"
	// StatusCited is a `derived_from` clause, which claims no coverage
	// (STDREF-006). It takes none of the classes above.
	StatusCited Status = "cited"
)

// Mapping is one `standard` note on one rule.
type Mapping struct {
	// Rule is the rule's qualified ID.
	Rule string
	// State is the rule's lifecycle state.
	State string
	// Note is the note's ID.
	Note string
}

// Clause is one clause of a standard as it stands in one entity. A clause is
// identified by Standard, Version and Clause (STDREF-011).
type Clause struct {
	// Standard is the Standard Reference's `id`.
	Standard string
	// Version is its `version`, empty when it names no edition.
	Version string
	// Clause is the clause as the note writes it.
	Clause string
	// Entity is the EUID of the entity measured.
	Entity string
	// Status is the clause's class in that entity.
	Status Status
	// Rules lists the mappings behind the status, sorted.
	Rules []Mapping
}

// Report is the coverage of a set of entities.
type Report struct {
	// Declared lists each Standard Reference in scope of the entities, sorted,
	// so a standard nothing maps still shows.
	Declared []entity.StandardRef
	// Clauses lists the clauses, sorted by standard, version, clause and entity.
	Clauses []Clause
	// Unresolved lists the `standard` notes whose `<id>` resolves to no
	// reference, which RL031 reports for the Realm's own rules.
	Unresolved []Mapping
}

type clauseKey struct{ standard, version, clause string }

// Compute measures each entity over its coverage rule set (STDREF-010), which
// inherit.Effective yields with its default states.
func Compute(entities []*realmresolve.Inheritance) *Report {
	r := &Report{}
	declared := make(map[entity.StandardRef]bool)
	for _, e := range entities {
		hop := e.Entity
		for _, scope := range append([][]entity.StandardRef{hop.Entity.Entity.Standards}, hop.Scopes...) {
			for _, ref := range scope {
				declared[ref] = true
			}
		}
		r.measure(e)
	}
	for ref := range declared {
		r.Declared = append(r.Declared, ref)
	}
	slices.SortFunc(r.Declared, func(a, b entity.StandardRef) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version), cmp.Compare(a.Relation, b.Relation))
	})
	slices.SortFunc(r.Clauses, func(a, b Clause) int {
		return cmp.Or(cmp.Compare(a.Standard, b.Standard), cmp.Compare(a.Version, b.Version), cmp.Compare(a.Clause, b.Clause), cmp.Compare(a.Entity, b.Entity), cmp.Compare(a.Status, b.Status))
	})
	return r
}

// measure adds the clauses of one entity.
func (r *Report) measure(e *realmresolve.Inheritance) {
	euid := e.Entity.Entity.Entity.ID
	claims := make(map[clauseKey]*Clause)
	cites := make(map[clauseKey]*Clause)
	add := func(set map[clauseKey]*Clause, key clauseKey, status Status, m Mapping) {
		c, ok := set[key]
		if !ok {
			c = &Clause{Standard: key.standard, Version: key.version, Clause: key.clause, Entity: euid, Status: status}
			set[key] = c
		}
		if !slices.Contains(c.Rules, m) {
			c.Rules = append(c.Rules, m)
		}
		if status == StatusCovered {
			c.Status = status
		}
	}

	for _, rule := range inherit.Effective(e.Entity, e.Ancestors, nil) {
		r.eachMapping(rule, func(key clauseKey, ref *entity.StandardRef, m Mapping) {
			switch {
			case ref.Relation != entity.RelationImplements:
				add(cites, key, StatusCited, m)
			case rule.Rule.State == entity.StateImplemented:
				add(claims, key, StatusCovered, m)
			default:
				add(claims, key, StatusMapped, m)
			}
		})
	}
	lapsed := make(map[clauseKey]*Clause)
	for _, rule := range inherit.Displaced(e.Entity, e.Ancestors) {
		r.eachMapping(rule, func(key clauseKey, ref *entity.StandardRef, m Mapping) {
			if _, live := claims[key]; !live && ref.Relation == entity.RelationImplements {
				add(lapsed, key, StatusLapsed, m)
			}
		})
	}

	for _, set := range []map[clauseKey]*Clause{claims, cites, lapsed} {
		for _, c := range set {
			slices.SortFunc(c.Rules, func(a, b Mapping) int {
				return cmp.Or(cmp.Compare(a.Rule, b.Rule), cmp.Compare(a.Note, b.Note))
			})
			r.Clauses = append(r.Clauses, *c)
		}
	}
}

// eachMapping calls f for each `standard` note on rule that resolves, from the
// entity owning the rule (NTE-016), and records those that don't.
func (r *Report) eachMapping(rule inherit.Rule, f func(clauseKey, *entity.StandardRef, Mapping)) {
	for _, n := range rule.Notes {
		if n.Type != entity.NoteTypeStandard {
			continue
		}
		m := Mapping{Rule: rule.QualifiedID(), State: rule.Rule.State, Note: n.ID}
		id, clause, err := entity.ParseStandardNote(n.Content)
		if err != nil {
			continue // RL029.
		}
		ref := entity.ResolveStandard(id, rule.Standards...)
		if ref == nil {
			if !slices.Contains(r.Unresolved, m) {
				r.Unresolved = append(r.Unresolved, m)
			}
			continue
		}
		f(clauseKey{ref.ID, ref.Version, clause}, ref, m)
	}
}
