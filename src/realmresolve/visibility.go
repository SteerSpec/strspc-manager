package realmresolve

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// relation is how the Realm making a reference stands to the Realm holding its
// target. Tree checks hold from a top-level Realm (RLM-016/01, RLM-020): a sub-realm
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

// place is where an entity sits: its Realm tree and the Realm holding it.
type place struct {
	// ns is the Realm tree's ID.
	ns string
	// realm is the ID of the Realm holding the entity.
	realm string
}

// relationOf returns how from stands to to.
func relationOf(from, to place) relation {
	switch {
	case from.ns != to.ns:
		return otherRealm
	case from.realm == to.realm:
		return sameRealm
	default:
		return sameTree
	}
}

// place returns where e, an entity of n, sits. A dependency's node also holds
// the entities of its sub-realms, so for those the Realm comes from the file:
// a sub-realm is a directory of its parent (RLM-010).
func (n *node) place(e *entity.File) place {
	p := place{ns: n.ns, realm: n.realm.Realm.Realm.ID}
	if n.deps != nil {
		return p
	}
	rel, err := filepath.Rel(n.realm.Dir, n.realm.EUIDs[e.Entity.ID])
	if err != nil {
		return p
	}
	dir, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	if slices.Contains(n.realm.Realm.SubRealms, dir) {
		p.realm += "." + dir
	}
	return p
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

// visibility returns the effective visibility of e, an entity of n (ENT-011):
// its own, or else the narrowest one declared by an entity reached through
// `parent` and `inherits` links, and `public` when none declares one
// (ENT-011/02). Each branch stops at the first entity declaring one. A link
// that loops or cannot be resolved adds nothing; rule-lint and the chain walk
// report those (RL007, RR013, RR014). Following an `inherits` link can load a
// dependency, and a failure to load it is reported, as the visibility then
// cannot be known.
func (s *chainState) visibility(n *node, e *entity.File) string {
	if v := s.narrowest(n, e, make(map[*entity.File]bool)); v != "" {
		return v
	}
	return entity.VisibilityPublic
}

// narrowest returns the narrowest visibility declared by e or, when e declares
// none, by the entities its `parent` and `inherits` links reach, or "" when
// none of them declares one. seen holds the entities already visited.
func (s *chainState) narrowest(n *node, e *entity.File, seen map[*entity.File]bool) string {
	if seen[e] {
		return ""
	}
	seen[e] = true
	if e.Entity.Visibility != "" {
		return e.Entity.Visibility
	}
	var out string
	if container, ok := n.entities[e.Entity.Parent]; e.Entity.Parent != "" && ok {
		out = narrower(out, s.narrowest(n, container, seen))
	}
	if e.Entity.Inherits != "" {
		if next, target, _ := s.lookup(n, e.Entity.Inherits); target != nil {
			out = narrower(out, s.narrowest(next, target, seen))
		}
	}
	return out
}

// narrower returns the narrower of a and b, either of which may be "" for
// none (ENT-015).
func narrower(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "" || entity.VisibilityWider(b, a):
		return a
	default:
		return b
	}
}

// containedVisibility returns the effective visibility of e from `parent` links
// alone, as rule-lint computes it for RL024. The bound stops a `parent` loop.
func containedVisibility(entities map[string]*entity.File, e *entity.File) string {
	for i := 0; i <= len(entities); i++ {
		if e.Entity.Visibility != "" {
			return e.Entity.Visibility
		}
		container, ok := entities[e.Entity.Parent]
		if e.Entity.Parent == "" || !ok {
			break
		}
		e = container
	}
	return entity.VisibilityPublic
}

// checkContainment reports RR026 when e, an entity of the resolved Realm tree,
// declares a visibility wider than the effective visibility of the entity
// containing it (ENT-018) only because of what that entity, or one containing
// it, inherits (ENT-011). Without inheritance rule-lint reports it (RL024), as
// it does an unknown value (RL023).
func (s *chainState) checkContainment(entities map[string]*entity.File, e *entity.File, path string) {
	own := e.Entity.Visibility
	container, ok := entities[e.Entity.Parent]
	if own == "" || !entity.ValidVisibility(own) || e.Entity.Parent == "" || !ok {
		return
	}
	limit := s.visibility(s.treeNode[e.Entity.ID], container)
	if !entity.VisibilityWider(own, limit) || entity.VisibilityWider(own, containedVisibility(entities, container)) {
		return
	}
	s.res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RR026",
		Severity: result.Error,
		Message:  fmt.Sprintf("entity %s: visibility `%s` is wider than `%s` of containing entity %s, whose visibility comes from inheritance", e.Entity.ID, own, limit, container.Entity.ID),
		Path:     path,
	})
}

// outOfForce reports whether a rule in state no longer counts for ENT-019 and
// NTE-012. It is narrower than !isLive: a superseded rule stays in force, and
// so inherited and evaluated, until it is Retired.
func outOfForce(state string) bool {
	return state == entity.StateAbandoned || state == entity.StateRetired || state == entity.StateTerminated
}

// checkVisibility checks the chain of child, held by from, against ENT-015,
// ENT-016 and ENT-019, ancestors given nearest first. Each entity of the tree answers for
// itself, but a dependency's entities aren't otherwise read here, and a wider
// entity there would hand down what its parent holds back, so each dependency
// hop is checked too, from the Realm holding it. Hops resolve at their own pins,
// so this only reports what that dependency's own run would. A refused
// inheritance keeps its chain, so its overrides are still reported.
func (s *chainState) checkVisibility(from *node, child inherit.Hop, ancestors []inherit.Hop, path string) {
	s.checkHop(child.Entity.Entity.ID, from, child, ancestors, path)
	for i, h := range ancestors {
		at := s.holder[h.Entity]
		if at.deps != nil {
			continue
		}
		who := fmt.Sprintf("%s (through %s:%s)", child.Entity.Entity.ID, h.RealmID, h.Entity.Entity.ID)
		s.checkHop(who, at, h, ancestors[i+1:], path)
	}
}

// checkHop checks hop, held by from and named who in diagnostics, against the
// ancestors above it: RR022 when the inherited entity refuses the inheritance,
// RR024 when hop is wider than it, RR023 for each override that the entities up
// to the rule's owner refuse. ENT-015 keeps the inherited entity no wider than
// those above it, so it alone answers for the inheritance.
func (s *chainState) checkHop(who string, from *node, hop inherit.Hop, ancestors []inherit.Hop, path string) {
	if len(ancestors) == 0 {
		return
	}
	parent := ancestors[0]
	at := s.holder[parent.Entity]
	v := s.visibility(at, parent.Entity)
	name := parent.RealmID + ":" + parent.Entity.Entity.ID
	here := from.place(hop.Entity)
	if !mayInherit(v, relationOf(here, at.place(parent.Entity))) {
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR022",
			Severity: result.Error,
			Message:  fmt.Sprintf("entity %s: inherits %s, whose visibility `%s` does not permit it from realm %s", who, name, v, here.realm),
			Path:     path,
		})
	}
	if own := s.visibility(from, hop.Entity); entity.VisibilityWider(own, v) {
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR024",
			Severity: result.Error,
			Message:  fmt.Sprintf("entity %s: visibility `%s` is wider than `%s` of inherited entity %s", who, own, v, name),
			Path:     path,
		})
	}
	for _, r := range hop.Entity.Rules {
		if r.Overrides != nil && !outOfForce(r.State) {
			s.checkOverrideVisibility(here, r.ID, inherit.Target(hop, ancestors, *r.Overrides), ancestors, path)
		}
	}
}

// checkOverrideVisibility reports RR023 for the first entity from the nearest
// ancestor up to the owner of target that refuses the override declared by
// ruleID. Entities above the owner have no say. A target that no ancestor
// owns is left to RR015; one that isn't inherited, such as a Draft rule, is
// still checked (ENT-019/01).
func (s *chainState) checkOverrideVisibility(from place, ruleID, target string, ancestors []inherit.Hop, path string) {
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
		v := s.visibility(at, h.Entity)
		if mayOverride(v, relationOf(from, at.place(h.Entity))) {
			continue
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RR023",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule %s overrides %s: visibility `%s` of %s:%s does not permit it from realm %s", ruleID, target, v, h.RealmID, h.Entity.Entity.ID, from.realm),
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
		v := s.visibility(next, target)
		var why string
		switch own := s.visibility(at, e); {
		case !mayInherit(v, relationOf(at.place(e), next.place(target))):
			why = fmt.Sprintf("visibility `%s` does not permit inheritance from realm %s", v, at.place(e).realm)
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
