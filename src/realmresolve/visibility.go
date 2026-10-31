package realmresolve

import (
	"fmt"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// relation is how the Realm making a reference stands to the Realm holding its
// target. Tree checks hold from a top-level Realm (RLM-016/01): a sub-realm
// resolved on its own sees neither its parent nor its siblings as such.
type relation int

const (
	// otherRealm is a Realm outside the Realm tree.
	otherRealm relation = iota
	// sameTree is another Realm of the Realm tree (RLM-016): the parent, a
	// sub-realm or a sibling.
	sameTree
	// sameRealm is the Realm itself, at any version (RLM-016/01).
	sameRealm
)

// relationOf returns how from stands to to. A dependency's sub-realms share
// its node, which is harmless: from a Realm outside that tree, they are all
// otherRealm.
func relationOf(from, to *node) relation {
	switch {
	case from.ns != to.ns:
		return otherRealm
	case from.realm.Realm.Realm.ID == to.realm.Realm.Realm.ID:
		return sameRealm
	default:
		return sameTree
	}
}

// mayInherit reports whether visibility permits inheriting across rel (ENT-012
// to ENT-014). An unknown value permits nothing.
func mayInherit(visibility string, rel relation) bool {
	switch visibility {
	case entity.VisibilityPublic, entity.VisibilityProtected:
		return true
	case entity.VisibilityPrivate:
		return rel != otherRealm
	default:
		return false
	}
}

// mayOverride reports whether visibility permits overriding across rel (ENT-012
// to ENT-014). An unknown value permits nothing.
func mayOverride(visibility string, rel relation) bool {
	switch visibility {
	case entity.VisibilityPublic:
		return true
	case entity.VisibilityProtected:
		return rel != otherRealm
	case entity.VisibilityPrivate:
		return rel == sameRealm
	default:
		return false
	}
}

// visibility returns the effective visibility of e, an entity of n (ENT-011).
// Each step follows `parent` to the containing entity; the bound stops a file
// whose `parent` loops, which rule-lint reports as RL007.
func (n *node) visibility(e *entity.File) string {
	for i := 0; i <= len(n.entities); i++ {
		if e.Entity.Visibility != "" {
			return e.Entity.Visibility
		}
		container, ok := n.entities[e.Entity.Parent]
		if e.Entity.Parent == "" || !ok {
			break
		}
		e = container
	}
	return entity.VisibilityPublic
}

// outOfForce reports whether a rule in state no longer counts for ENT-016 and
// NTE-012. It is narrower than !isLive: a superseded rule stays in force, and
// so inherited and evaluated, until it is Retired.
func outOfForce(state string) bool {
	return state == entity.StateAbandoned || state == entity.StateRetired || state == entity.StateTerminated
}

// checkVisibility checks child, held by from, against the visibility of its
// ancestors, given nearest first: RR022 when the inherited entity refuses the
// inheritance, RR023 when an entity between it and the owner of an overridden
// rule refuses the override (ENT-016), RR024 when child is wider than the
// entity it inherits (ENT-015). ENT-015 keeps the inherited entity no wider
// than those above it, so it alone answers for the inheritance. A refused
// inheritance keeps its chain, so its overrides are still reported.
func (s *chainState) checkVisibility(from *node, child inherit.Hop, ancestors []inherit.Hop, path string) {
	if len(ancestors) == 0 {
		return
	}
	self := child.Entity.Entity
	parent := ancestors[0]
	at := s.holder[parent.Entity]
	v := at.visibility(parent.Entity)
	name := parent.RealmID + ":" + parent.Entity.Entity.ID
	if !mayInherit(v, relationOf(from, at)) {
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR022",
			Severity: result.Error,
			Message:  fmt.Sprintf("entity %s: inherits %s, whose visibility `%s` does not permit it from realm %s", self.ID, name, v, from.realm.Realm.Realm.ID),
			Path:     path,
		})
	}
	if own := from.visibility(child.Entity); entity.VisibilityWider(own, v) {
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR024",
			Severity: result.Error,
			Message:  fmt.Sprintf("entity %s: visibility `%s` is wider than `%s` of inherited entity %s", self.ID, own, v, name),
			Path:     path,
		})
	}
	for _, r := range child.Entity.Rules {
		if r.Overrides != nil && !outOfForce(r.State) {
			s.checkOverrideVisibility(from, r.ID, inherit.Target(child, ancestors, *r.Overrides), ancestors, path)
		}
	}
}

// checkOverrideVisibility reports RR023 for the first entity from the nearest
// ancestor up to the owner of target that refuses the override declared by
// ruleID. Entities above the owner have no say. A target that no ancestor
// owns is left to RR015; one that isn't inherited, such as a Draft rule, is
// still checked (ENT-016/01).
func (s *chainState) checkOverrideVisibility(from *node, ruleID, target string, ancestors []inherit.Hop, path string) {
	owner := -1
	for i, h := range ancestors {
		if id, ok := strings.CutPrefix(target, h.RealmID+":"); ok {
			if _, ok := findRule(h.Entity, id); ok {
				owner = i
				break
			}
		}
	}
	if owner < 0 {
		return
	}
	for _, h := range ancestors[:owner+1] {
		at := s.holder[h.Entity]
		v := at.visibility(h.Entity)
		if mayOverride(v, relationOf(from, at)) {
			continue
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR023",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule %s overrides %s: visibility `%s` of %s:%s does not permit it from realm %s", ruleID, target, v, h.RealmID, h.Entity.Entity.ID, from.realm.Realm.Realm.ID),
			Path:     path,
		})
		return
	}
}

// checkExtendsVisibility reports RR025 for each reference of `extends` note n,
// on a rule of e held by at, to a rule its Realm may not inherit or of an
// entity narrower than e (NTE-012). A short reference counts: the narrower
// target is usually in the same Realm. What cannot be resolved is left to the
// other reference checks.
func (s *chainState) checkExtendsVisibility(at *node, e *entity.File, n entity.Note, path string) {
	reported := make(map[string]bool)
	for _, m := range ruleRefRe.FindAllStringSubmatch(n.Content, -1) {
		if reported[m[0]] {
			continue
		}
		reported[m[0]] = true
		ref := m[2]
		if m[1] != "" {
			ref = m[1] + ":" + m[2]
		}
		next, target, _ := s.lookup(at, ref)
		if target == nil {
			continue
		}
		if _, ok := findRule(target, m[2]+"-"+m[3]); !ok {
			continue
		}
		v := next.visibility(target)
		var why string
		switch own := at.visibility(e); {
		case !mayInherit(v, relationOf(at, next)):
			why = fmt.Sprintf("visibility `%s` does not permit inheritance from realm %s", v, at.realm.Realm.Realm.ID)
		case entity.VisibilityWider(own, v):
			why = fmt.Sprintf("visibility `%s` is narrower than `%s` of entity %s", v, own, e.Entity.ID)
		default:
			continue
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR025",
			Severity: result.Error,
			Message:  fmt.Sprintf("note %s references %q: %s", n.ID, m[0], why),
			Path:     path,
		})
	}
}
