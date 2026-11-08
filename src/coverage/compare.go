package coverage

import (
	"cmp"
	"slices"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Kind is the kind of a Regression.
type Kind string

// The kinds of regression.
const (
	// KindDowngraded is a clause covered in base and mapped or lapsed in head,
	// or mapped in base and lapsed in head, in the same entity.
	KindDowngraded Kind = "downgraded"
	// KindLost is a clause covered or mapped in base that head no longer
	// claims in that entity: gone from the report, or only unmapped or cited.
	KindLost Kind = "lost"
	// KindExcluded is a clause covered or mapped in base that an `exclusion`
	// Realm Note excludes in head (STDREF-022). The note gives a reason, so a
	// caller may allow it, but a gate shouldn't pass it without being told to.
	KindExcluded Kind = "excluded"
	// KindDropped is an `implements` Standard Reference declared in base that
	// head no longer declares as `implements`, in that edition.
	KindDropped Kind = "dropped"
	// KindRebased is a clause that would be lost, or a reference that would
	// be dropped, because head moved the standard to another edition: head
	// declares no `implements` reference to the base edition and one to
	// another. Editions are different clauses (STDREF-011), so without a
	// crosswalk between them Compare can't tell what carried forward. A
	// caller may allow it, so that a gate doesn't block every edition upgrade.
	KindRebased Kind = "rebased"
	// KindUnresolved is a `standard` note that backed a covered or mapped
	// clause in base, so it resolved, and no longer resolves in head.
	KindUnresolved Kind = "unresolved"
)

// Regression is one way head's coverage is worse than base's.
type Regression struct {
	// Kind is what got worse.
	Kind Kind `json:"kind"`
	// Standard and Version are the clause's or the reference's, in base.
	// Both are empty for KindUnresolved.
	Standard string `json:"standard,omitempty"`
	Version  string `json:"version,omitempty"`
	// Clause, Realm and Entity name the clause and the entity it stands in.
	// They are empty for a reference, and for KindUnresolved.
	Clause string `json:"clause,omitempty"`
	Realm  string `json:"realm,omitempty"`
	Entity string `json:"entity,omitempty"`
	// Base is the clause's status in base, empty for a reference and for
	// KindUnresolved.
	Base Status `json:"base,omitempty"`
	// Head is the clause's status in head: its claim, else `cited` when only
	// a citation is left, empty when head has no row for it.
	Head Status `json:"head,omitempty"`
	// Rules lists the mappings behind the clause in base, or for
	// KindUnresolved the note as head holds it.
	Rules []Mapping `json:"rules,omitempty"`
	// Exclusion is the Realm Note excluding the clause in head, for
	// KindExcluded.
	Exclusion *ExclusionRef `json:"exclusion,omitempty"`
}

// clauseID identifies a clause in an entity across two reports.
type clauseID struct{ standard, version, clause, realm, entity string }

func idOf(c *Clause) clauseID {
	return clauseID{c.Standard, c.Version, c.Clause, c.Realm, c.Entity}
}

// edition is a Standard Reference's identity in Declared: its id and version.
type edition struct{ id, version string }

// mappingID identifies a `standard` note across two reports, whatever the
// state of its rule.
type mappingID struct{ rule, note string }

// Compare returns the ways head's coverage is worse than base's, sorted by
// kind, then standard, version, clause, realm and entity. It reads only
// Declared, Clauses and Unresolved, not Figures. A `cited` clause claims no
// coverage (STDREF-006), so it never regresses, nor does a clause lapsed,
// excluded or unmapped in base. An improvement, such as a clause mapped in
// base and covered in head, is not reported. Identical reports give no
// regression.
func Compare(base, head *Report) []Regression {
	out := []Regression{}
	baseEditions, headEditions := implemented(base), implemented(head)
	// rebased reports whether head moved the standard of e to another edition.
	rebased := func(e edition) bool {
		if headEditions[e] {
			return false
		}
		for h := range headEditions {
			if h.id == e.id {
				return true
			}
		}
		return false
	}

	// A clause can stand in an entity twice, cited and claimed, when two
	// references of one id have different relations: the claim is what a
	// clause is compared with, and a citation only says why it is lost.
	heads := make(map[clauseID]*Clause)
	cites := make(map[clauseID]bool)
	for i := range head.Clauses {
		if c := &head.Clauses[i]; c.Status == StatusCited {
			cites[idOf(c)] = true
		} else {
			heads[idOf(c)] = c
		}
	}
	for i := range base.Clauses {
		b := &base.Clauses[i]
		if b.Status != StatusCovered && b.Status != StatusMapped {
			continue
		}
		r := Regression{
			Standard: b.Standard, Version: b.Version, Clause: b.Clause, Realm: b.Realm, Entity: b.Entity,
			Base: b.Status, Rules: b.Rules,
		}
		h := heads[idOf(b)]
		switch {
		case h != nil:
			r.Head = h.Status
		case cites[idOf(b)]:
			r.Head = StatusCited
		}
		switch {
		case h != nil && h.Status == StatusCovered:
			continue
		case h != nil && h.Status == StatusMapped && b.Status == StatusMapped:
			continue
		case h != nil && (h.Status == StatusMapped || h.Status == StatusLapsed):
			r.Kind = KindDowngraded
		case h != nil && h.Status == StatusExcluded:
			r.Kind, r.Exclusion = KindExcluded, h.Exclusion
		case rebased(edition{b.Standard, b.Version}):
			r.Kind = KindRebased
		default:
			r.Kind = KindLost
		}
		out = append(out, r)
	}

	for e := range baseEditions {
		switch {
		case headEditions[e]:
		case rebased(e):
			out = append(out, Regression{Kind: KindRebased, Standard: e.id, Version: e.version})
		default:
			out = append(out, Regression{Kind: KindDropped, Standard: e.id, Version: e.version})
		}
	}

	// Only a note behind a covered or mapped clause claimed anything, as
	// only such a clause can regress. One behind a citation, a lapsed or an
	// excluded clause alone is debt in the report itself.
	resolved := make(map[mappingID]bool)
	for i := range base.Clauses {
		if st := base.Clauses[i].Status; st != StatusCovered && st != StatusMapped {
			continue
		}
		for _, m := range base.Clauses[i].Rules {
			resolved[mappingID{m.Rule, m.Note}] = true
		}
	}
	reported := make(map[mappingID]bool)
	for _, m := range head.Unresolved {
		id := mappingID{m.Rule, m.Note}
		if resolved[id] && !reported[id] {
			reported[id] = true
			out = append(out, Regression{Kind: KindUnresolved, Rules: []Mapping{m}})
		}
	}

	slices.SortFunc(out, func(a, b Regression) int {
		return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Standard, b.Standard), cmp.Compare(a.Version, b.Version),
			cmp.Compare(a.Clause, b.Clause), cmp.Compare(a.Realm, b.Realm), cmp.Compare(a.Entity, b.Entity),
			slices.CompareFunc(a.Rules, b.Rules, func(x, y Mapping) int {
				return cmp.Or(cmp.Compare(x.Rule, y.Rule), cmp.Compare(x.Note, y.Note))
			}))
	})
	return out
}

// implemented returns the editions r declares with relation `implements`.
func implemented(r *Report) map[edition]bool {
	out := make(map[edition]bool)
	for _, ref := range r.Declared {
		if ref.Relation == entity.RelationImplements {
			out[edition{ref.ID, ref.Version}] = true
		}
	}
	return out
}
