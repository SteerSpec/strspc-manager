// Package inherit computes the effective rule set of an entity that
// inherits another entity's rule set (ENT-006..010, RUL-009..011).
//
// It works on an already resolved inheritance chain: the inheriting entity
// and its ancestors, each tagged with the namespace (realm ID) it belongs to.
// Finding the ancestors across realms is realm-resolve's job.
package inherit

import (
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
	// Entity is the entity file or sub-entity.
	Entity *entity.File
}

// Rule is one entry of an effective rule set.
type Rule struct {
	// Rule is the rule as declared by its owner.
	Rule entity.Rule
	// Notes are the owner's notes on the rule.
	Notes []entity.Note
	// OwnerRealm is the namespace of the owning entity.
	OwnerRealm string
	// Owner is the EUID of the entity whose rules array holds the rule.
	Owner string
	// InheritedBy is the EUID of the inheriting entity when the rule comes
	// from an ancestor, empty for the entity's own rules.
	InheritedBy string
	// Overrides is the qualified ID of the inherited rule this rule
	// replaced, empty when it replaced none.
	Overrides string
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

// Effective returns the effective rule set of child, whose ancestors are
// given nearest first. The child's own rules are filtered by states (nil
// means DefaultStates) before their overrides are applied, so an inherited
// rule reappears once the rule overriding it leaves the evaluated states.
// Inherited entries carry InheritedBy = the child's EUID.
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
	}
	ns := namespaces(append([]Hop{child}, ancestors...))
	return apply(set, ownRules(child, ns, func(r entity.Rule) bool { return keep[r.State] }))
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
func ownRules(h Hop, ns []string, keep func(entity.Rule) bool) []Rule {
	notes := make(map[string][]entity.Note)
	for _, n := range h.Entity.Notes {
		notes[n.RuleRef] = append(notes[n.RuleRef], n)
	}
	var out []Rule
	for _, r := range h.Entity.Rules {
		if !keep(r) {
			continue
		}
		e := Rule{Rule: r, Notes: notes[r.ID], OwnerRealm: h.RealmID, Owner: h.Entity.Entity.ID}
		if r.Overrides != nil && *r.Overrides != "" {
			e.Overrides = normalise(Qualify(h.RealmID, *r.Overrides), ns)
		}
		out = append(out, e)
	}
	return out
}

// apply removes from set the targets that the added rules override, then
// appends the added rules. An added rule keeps Overrides only when its
// target was actually in the set.
func apply(set, added []Rule) []Rule {
	remove := make(map[string]bool)
	for _, a := range added {
		if a.Overrides != "" {
			remove[a.Overrides] = true
		}
	}
	present := make(map[string]bool, len(set))
	kept := set[:0:0]
	for _, r := range set {
		id := r.QualifiedID()
		if remove[id] {
			present[id] = true
			continue
		}
		kept = append(kept, r)
	}
	for i := range added {
		if added[i].Overrides != "" && !present[added[i].Overrides] {
			added[i].Overrides = ""
		}
	}
	return append(kept, added...)
}

// namespaces lists the distinct namespaces of hops.
func namespaces(hops []Hop) []string {
	var out []string
	seen := make(map[string]bool)
	for _, h := range hops {
		if !seen[h.RealmID] {
			seen[h.RealmID] = true
			out = append(out, h.RealmID)
		}
	}
	return out
}

// normalise maps a qualified ID whose realm is a sub-realm of one of the
// namespaces (`parent.auth:ACCT-005`) onto that namespace
// (`parent:ACCT-005`), so that it matches the owner's QualifiedID. A realm
// that is itself one of the namespaces is left alone, and the longest
// matching namespace wins.
func normalise(qualified string, ns []string) string {
	realm, id, ok := strings.Cut(qualified, ":")
	if !ok {
		return qualified
	}
	best := ""
	for _, n := range ns {
		if realm == n {
			return qualified
		}
		if strings.HasPrefix(realm, n+".") && len(n) > len(best) {
			best = n
		}
	}
	if best == "" {
		return qualified
	}
	return best + ":" + id
}
