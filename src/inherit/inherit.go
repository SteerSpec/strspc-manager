// Package inherit computes the effective rule set of an entity that
// inherits another entity's rule set (ENT-006..010, RUL-009..011).
//
// It works on an already resolved inheritance chain: the inheriting entity
// and its ancestors, each tagged with the namespace (realm ID) it belongs to.
// Finding the ancestors across realms is realm-resolve's job.
package inherit

import (
	"slices"
	"sort"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// DefaultStates are the states of the inheriting entity's own rules that
// Effective keeps when no states are given. They match the evaluator's
// default evaluate_on (ruleeval.DefaultEvaluateStates).
var DefaultStates = []string{entity.StateDraft, entity.StatePublished, entity.StateImplemented}

// Hop is one entity of an inheritance chain.
type Hop struct {
	// RealmID is the namespace the entity belongs to: the ID of the realm
	// tree holding it. Entities of a realm and of its sub-realms share the
	// realm's ID, since EUIDs are unique across both (RLM-013).
	RealmID string
	// RealmKind is the kind the manifest of the Realm holding the entity
	// declares, its own for an entity of a sub-realm: entity.KindStandard, or
	// empty for a code Realm (RLMMNFST-009).
	RealmKind string
	// SubRealmIDs are the IDs of the sub-realms the realm's manifest
	// declares. A rule qualified with one of them belongs to RealmID.
	SubRealmIDs []string
	// Entity is the entity file or sub-entity.
	Entity *entity.File
	// Scopes holds the Standard References around the entity, nearest first:
	// its containing entities, its Realm, then that Realm's parent (NTE-016).
	Scopes [][]entity.StandardRef
}

// Extended is a live rule named by an `extends` note, as the evaluator reads
// it alongside the rule that extends it.
type Extended struct {
	// ID is the reference as the note writes it.
	ID string
	// Revision is the rule's revision.
	Revision int
	// Body is the rule's text.
	Body string
}

// Rule is one entry of an effective rule set.
type Rule struct {
	// Rule is the rule as declared by its owner.
	Rule entity.Rule
	// Notes are the owner's notes on the rule.
	Notes []entity.Note
	// OwnerRealm is the namespace of the owning entity.
	OwnerRealm string
	// RealmKind is the kind of the Realm holding the owning entity, as in
	// Hop.RealmKind.
	RealmKind string
	// Owner is the EUID of the entity whose rules array holds the rule.
	Owner string
	// OwnerTitle is the title of that entity.
	OwnerTitle string
	// InheritedBy is the EUID of the inheriting entity when the rule comes
	// from an ancestor, empty for the entity's own rules.
	InheritedBy string
	// InheritedByTitle is the title of that inheriting entity.
	InheritedByTitle string
	// Overrides is the qualified ID of the inherited rule this rule
	// replaced, empty when it replaced none. Only a Published or Implemented
	// rule replaces the rule it overrides (RUL-018, RUL-019).
	Overrides string
	// Overridden is the inherited rule this rule declares it overrides, as the
	// set held it before any override of this entity applied, whether or not
	// this rule replaced it: an evaluation of an overriding rule receives it as
	// context, for a Draft override too (RUL-020). Nil when the rule overrides
	// nothing or its target is not in the inherited set.
	Overridden *Rule
	// OverriddenReplaced is true when Overridden is set, this rule does not
	// replace it, and another rule of the same entity does, as a Published
	// override does while a Draft successor of it is written. Overridden is
	// then no longer in force for the entity.
	OverriddenReplaced bool
	// Enforced is the enforced state of the rule when it differs from its
	// State: Implemented for a Published rule that replaces a rule whose
	// enforced state is Implemented (RUL-021, RUL-024). Read it through
	// EnforcedState; it is derived each time and never stored in a file.
	Enforced string
	// Standards holds the Standard References a `standard` note on the rule
	// resolves against, nearest first, from the entity owning the rule
	// (NTE-016).
	Standards [][]entity.StandardRef
}

// EnforcedState returns the state the rule is evaluated and enforced as
// (RUL-024): Implemented when RUL-021 requires it, the rule's State otherwise.
func (r Rule) EnforcedState() string {
	if r.Enforced != "" {
		return r.Enforced
	}
	return r.Rule.State
}

// QualifiedID returns the rule's ID qualified with its owner's namespace.
func (r Rule) QualifiedID() string {
	return r.OwnerRealm + ":" + r.Rule.ID
}

// Inherited returns the rule set an entity inherits from its ancestors,
// given nearest first (ENT-009): starting from the root-most ancestor, each
// ancestor's Published and Implemented rules are added after removing the
// rules its Published and Implemented rules override. Every entry is
// returned with InheritedBy left empty; Effective fills it in.
func Inherited(ancestors []Hop) []Rule {
	ns := namespaces(ancestors)
	var set []Rule
	for i := len(ancestors) - 1; i >= 0; i-- {
		h := ancestors[i]
		set = apply(set, ownRules(h, ns, func(r entity.Rule) bool {
			return r.State == entity.StatePublished || r.State == entity.StateImplemented
		}))
	}
	return set
}

// Effective returns the rule set child is evaluated against, whose ancestors
// are given nearest first: the inherited set (ENT-020) less the rules that
// child's own Published and Implemented rules replace (RUL-019), then child's
// own rules whose enforced state is in states (nil means DefaultStates). A
// Draft rule replaces nothing (RUL-018), so with the default states a Draft
// override is evaluated beside the rule it overrides. Which rules are replaced
// does not depend on states. Inherited entries carry InheritedBy = the child's
// EUID.
func Effective(child Hop, ancestors []Hop, states []string) []Rule {
	if states == nil {
		states = DefaultStates
	}
	keep := make(map[string]bool, len(states))
	for _, s := range states {
		keep[s] = true
	}
	set := Inherited(ancestors)
	for i := range set {
		set[i].InheritedBy = child.Entity.Entity.ID
		set[i].InheritedByTitle = child.Entity.Entity.Title
	}
	ns := namespaces(append([]Hop{child}, ancestors...))
	// Published and Implemented rules replace whatever states says; the rest
	// are kept only when states asks for them, and replace nothing.
	own := apply(set, ownRules(child, ns, func(r entity.Rule) bool {
		return keep[r.State] || r.State == entity.StatePublished || r.State == entity.StateImplemented
	}))
	return slices.DeleteFunc(own, func(r Rule) bool { return r.InheritedBy == "" && !keep[r.EnforcedState()] })
}

// Displaced returns the rules child no longer has that it or an ancestor once
// held in force: those a Published or Implemented override along the chain
// replaced (RUL-019; a Draft override replaces nothing, STDREF-015), then those
// in state Retired or Terminated owned by child or an ancestor. Ancestors are
// given nearest first. A rule displaced twice appears twice.
func Displaced(child Hop, ancestors []Hop) []Rule {
	publishedOrImplemented := func(r entity.Rule) bool {
		return r.State == entity.StatePublished || r.State == entity.StateImplemented
	}
	var out, set []Rule
	step := func(added []Rule) {
		out = append(out, replaced(set, added)...)
		set = apply(set, added)
	}
	ns := namespaces(ancestors)
	for i := len(ancestors) - 1; i >= 0; i-- {
		step(ownRules(ancestors[i], ns, publishedOrImplemented))
	}
	hops := append([]Hop{child}, ancestors...)
	ns = namespaces(hops)
	step(ownRules(child, ns, publishedOrImplemented))

	for _, h := range hops {
		out = append(out, ownRules(h, ns, func(r entity.Rule) bool {
			return r.State == entity.StateRetired || r.State == entity.StateTerminated
		})...)
	}
	return out
}

// replaced returns the rules of set that the added rules replace.
func replaced(set, added []Rule) []Rule {
	var out []Rule
	for _, r := range set {
		id := r.QualifiedID()
		if slices.ContainsFunc(added, func(a Rule) bool { return a.Overrides == id && replaces(a) }) {
			out = append(out, r)
		}
	}
	return out
}

// OverriddenBy maps the qualified ID of every rule replaced in the given
// effective sets to the rules replacing it, each rendered as
// `<qualified rule id> (<entity title>)` and sorted, for annotating the
// replaced rule where its owner is evaluated on its own. The title names
// the entity whose subject the replaced rule no longer covers: the
// inheriting entity for a replacing rule it inherits (C overrides P, G
// inherits C: P no longer covers G either), the owner otherwise.
func OverriddenBy(sets ...[]Rule) map[string][]string {
	out := make(map[string][]string)
	seen := make(map[string]bool)
	for _, set := range sets {
		for _, r := range set {
			by := r.QualifiedID()
			if title := r.affectedTitle(); title != "" {
				by += " (" + title + ")"
			}
			if r.Overrides == "" || seen[r.Overrides+"\x00"+by] {
				continue
			}
			seen[r.Overrides+"\x00"+by] = true
			out[r.Overrides] = append(out[r.Overrides], by)
		}
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

// affectedTitle is the title of the entity the rule applies to in its set.
func (r Rule) affectedTitle() string {
	if r.InheritedBy != "" {
		return r.InheritedByTitle
	}
	return r.OwnerTitle
}

// Qualify returns ref qualified with namespace when it is a short rule or
// entity ID, and ref unchanged when it already carries a realm prefix.
func Qualify(namespace, ref string) string {
	if strings.Contains(ref, ":") {
		return ref
	}
	return namespace + ":" + ref
}

// Target returns the qualified ID that a rule of child declaring
// `overrides: ref` replaces, in the form Rule.QualifiedID uses.
func Target(child Hop, ancestors []Hop, ref string) string {
	return normalise(Qualify(child.RealmID, ref), namespaces(append([]Hop{child}, ancestors...)))
}

// ownRules returns the rules of h that pass keep, with their notes and,
// in Overrides, the normalised target they declare (not yet applied).
func ownRules(h Hop, ns map[string]string, keep func(entity.Rule) bool) []Rule {
	notes := make(map[string][]entity.Note)
	for _, n := range h.Entity.Notes {
		notes[n.RuleRef] = append(notes[n.RuleRef], n)
	}
	var out []Rule
	for _, r := range h.Entity.Rules {
		if !keep(r) {
			continue
		}
		e := Rule{
			Rule: r, Notes: notes[r.ID], OwnerRealm: h.RealmID, RealmKind: h.RealmKind, Owner: h.Entity.Entity.ID, OwnerTitle: h.Entity.Entity.Title,
			Standards: append([][]entity.StandardRef{h.Entity.Entity.Standards}, h.Scopes...),
		}
		if r.Overrides != nil && *r.Overrides != "" {
			e.Overrides = normalise(Qualify(h.RealmID, *r.Overrides), ns)
		}
		out = append(out, e)
	}
	return out
}

// replaces reports whether a, which declares an override, replaces its target:
// only a Published or Implemented rule does (RUL-018, RUL-019).
func replaces(a Rule) bool {
	return a.Rule.State == entity.StatePublished || a.Rule.State == entity.StateImplemented
}

// apply removes from set the targets that the added Published and Implemented
// rules override, then appends the added rules. Every added rule whose target
// is in set gets it as Overridden; one that replaced it keeps Overrides, and
// a Published one is enforced as Implemented when the target's enforced state
// is Implemented (RUL-021). The others lose Overrides.
func apply(set, added []Rule) []Rule {
	byID := make(map[string]Rule, len(set))
	for _, r := range set {
		byID[r.QualifiedID()] = r
	}
	remove := make(map[string]bool)
	for i := range added {
		a := &added[i]
		target, ok := byID[a.Overrides]
		if a.Overrides == "" || !ok {
			a.Overrides = ""
			continue
		}
		a.Overridden = &target
		if !replaces(*a) {
			a.Overrides = ""
			continue
		}
		remove[a.Overrides] = true
		if a.Rule.State == entity.StatePublished && target.EnforcedState() == entity.StateImplemented {
			a.Enforced = entity.StateImplemented
		}
	}
	for i := range added {
		if a := &added[i]; a.Overridden != nil && a.Overrides == "" {
			a.OverriddenReplaced = remove[a.Overridden.QualifiedID()]
		}
	}
	kept := slices.DeleteFunc(slices.Clone(set), func(r Rule) bool { return remove[r.QualifiedID()] })
	return append(kept, added...)
}

// namespaces maps every realm ID that the hops' namespaces cover (each
// namespace and the sub-realms its manifest declares) to its namespace.
func namespaces(hops []Hop) map[string]string {
	out := make(map[string]string)
	for _, h := range hops {
		out[h.RealmID] = h.RealmID
		for _, id := range h.SubRealmIDs {
			out[id] = h.RealmID
		}
	}
	return out
}

// normalise maps a qualified ID whose realm is a declared sub-realm of one
// of the namespaces (`parent.auth:ACCT-005`) onto that namespace
// (`parent:ACCT-005`), so that it matches the owner's QualifiedID. Other
// qualifiers are left alone: a realm whose ID merely starts with a
// namespace's ID is a different realm.
func normalise(qualified string, ns map[string]string) string {
	realm, id, ok := strings.Cut(qualified, ":")
	if !ok {
		return qualified
	}
	if n, ok := ns[realm]; ok {
		return n + ":" + id
	}
	return qualified
}
