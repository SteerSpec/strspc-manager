package realmresolve

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// maxInheritHops is the longest inheritance chain allowed by ENT-008.
const maxInheritHops = 3

// Inheritance is the resolved inheritance chain of one entity of the
// resolved Realm or one of its sub-realms.
type Inheritance struct {
	// Entity is the inheriting entity. Its RealmID is the resolved Realm's
	// ID, also for entities of its sub-realms.
	Entity inherit.Hop
	// Ancestors lists the inherited entities, nearest first, each with the
	// ID of the Realm holding it.
	Ancestors []inherit.Hop
	// Path is the file holding the inheriting entity.
	Path string
}

// node is a Realm an inheritance hop can sit in, with what is needed to
// resolve that hop's `inherits` target.
type node struct {
	// realm is the Realm holding the hop's entity (for the resolved Realm
	// tree: the Realm itself or one of its sub-realms).
	realm *ResolvedRealm
	// ns is the namespace of the hop: the Realm tree's ID.
	ns string
	// subRealmIDs are the IDs of the sub-realms the tree's manifest
	// declares; a qualifier naming one of them stays in the namespace.
	subRealmIDs []string
	// entities indexes the EUIDs visible as short references: the whole
	// tree for the resolved Realm (RLM-013), the dependency's own directory
	// otherwise.
	entities map[string]*entity.File
	// deps holds the resolved effective dependencies by realm ID, or nil
	// for a dependency Realm, whose own dependencies load on demand.
	deps map[string]*ResolvedRealm
}

// chainState carries one Resolve call's inheritance resolution: the
// Realms loaded on demand and the nodes built for them.
type chainState struct {
	r   *RealmResolver
	ctx context.Context
	res *result.Result
	// loaded caches dependency Realms by realm_id@version; a nil value
	// records a failed load so its diagnostics are reported once.
	loaded map[string]*ResolvedRealm
	nodes  map[*ResolvedRealm]*node
	// treeNode maps each EUID of the resolved Realm tree to the node of the
	// Realm or sub-realm that holds it.
	treeNode map[string]*node
	// superseded caches supersededRules by node.
	superseded map[*node]map[string]bool
}

// resolveInheritance resolves the inheritance chain of every entity of the
// root Realm and its sub-realms (RR013, RR014) and checks their overrides
// against the inherited rule sets (RR015, RR016). Nothing is loaded when no
// entity uses inheritance.
func (r *RealmResolver) resolveInheritance(ctx context.Context, root *ResolvedRealm, subs []*ResolvedRealm, res *result.Result) []*Inheritance {
	entities, paths := treeEntities(root, subs)
	if !usesInheritance(entities) {
		return nil
	}
	s := r.newChainState(ctx, root, subs, entities, res)
	rootSubs := subRealmIDs(root.Realm)
	ids := sortedIDs(entities)

	var out []*Inheritance
	chains := make(map[string][]inherit.Hop)
	failed := make(map[string]bool)
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			res.Add(result.Diagnostic{Module: module, Code: "RR000", Severity: result.Error, Message: err.Error(), Path: root.Dir})
			return out
		}
		e := entities[id]
		if e.Entity.Inherits == "" {
			continue
		}
		ancestors, ok := s.walk(s.treeNode[id], e, paths[id])
		if !ok {
			failed[id] = true
			continue
		}
		chains[id] = ancestors
		out = append(out, &Inheritance{
			Entity:    inherit.Hop{RealmID: root.Realm.Realm.ID, SubRealmIDs: rootSubs, Entity: e},
			Ancestors: ancestors,
			Path:      paths[id],
		})
	}

	superseded := supersededRules(entities)
	for _, id := range ids {
		if !failed[id] {
			checkOverrides(inherit.Hop{RealmID: root.Realm.Realm.ID, SubRealmIDs: rootSubs, Entity: entities[id]}, chains[id], superseded, paths[id], res)
		}
	}
	return out
}

// treeEntities indexes the entities of the root Realm and its sub-realms by
// EUID, with the file holding each.
func treeEntities(root *ResolvedRealm, subs []*ResolvedRealm) (entities map[string]*entity.File, paths map[string]string) {
	entities = make(map[string]*entity.File)
	paths = make(map[string]string)
	for _, rr := range append([]*ResolvedRealm{root}, subs...) {
		for id, e := range rr.Entities {
			entities[id] = e
			paths[id] = rr.EUIDs[id]
		}
	}
	return entities, paths
}

// sortedIDs returns the keys of entities in order.
func sortedIDs(entities map[string]*entity.File) []string {
	ids := make([]string, 0, len(entities))
	for id := range entities {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// newChainState builds the state and tree nodes for resolving references
// made by the entities of the root Realm and its sub-realms.
func (r *RealmResolver) newChainState(ctx context.Context, root *ResolvedRealm, subs []*ResolvedRealm, entities map[string]*entity.File, res *result.Result) *chainState {
	s := &chainState{
		r:          r,
		ctx:        ctx,
		res:        res,
		loaded:     make(map[string]*ResolvedRealm),
		nodes:      make(map[*ResolvedRealm]*node),
		treeNode:   make(map[string]*node),
		superseded: make(map[*node]map[string]bool),
	}
	rootSubs := subRealmIDs(root.Realm)
	for _, rr := range append([]*ResolvedRealm{root}, subs...) {
		n := &node{realm: rr, ns: root.Realm.Realm.ID, subRealmIDs: rootSubs, entities: entities, deps: make(map[string]*ResolvedRealm)}
		for _, d := range rr.Dependencies {
			n.deps[d.Realm.Realm.ID] = d
			s.loaded[d.Realm.Realm.ID+"@"+d.Realm.Realm.Version] = d
		}
		s.nodes[rr] = n
		for id := range rr.Entities {
			s.treeNode[id] = n
		}
	}
	// Declared dependencies that failed to resolve are cached as failed, so
	// a dependency Realm declaring the same realm_id@version does not load
	// it again and repeat its diagnostics.
	for _, rr := range append([]*ResolvedRealm{root}, subs...) {
		for _, decl := range rr.Realm.Dependencies {
			if _, ok := s.loaded[decl.RealmID+"@"+decl.Version]; !ok {
				s.loaded[decl.RealmID+"@"+decl.Version] = nil
			}
		}
	}
	return s
}

// subRealmIDs returns the IDs of the sub-realms rf declares.
func subRealmIDs(rf *entity.RealmFile) []string {
	ids := make([]string, 0, len(rf.SubRealms))
	for _, name := range rf.SubRealms {
		ids = append(ids, rf.Realm.ID+"."+name)
	}
	return ids
}

// supersededRules returns the IDs of the rules superseded by a rule that is
// not Abandoned, across entities: such a rule is no longer live (RUL-011),
// the convention rule-lint's RL017 uses.
func supersededRules(entities map[string]*entity.File) map[string]bool {
	out := make(map[string]bool)
	for _, e := range entities {
		for _, r := range e.Rules {
			if r.Supersedes != nil && r.State != entity.StateAbandoned {
				out[*r.Supersedes] = true
			}
		}
	}
	return out
}

// isLive reports whether r is in force: not Abandoned, Terminated or in
// superseded.
func isLive(r entity.Rule, superseded map[string]bool) bool {
	return r.State != entity.StateAbandoned && r.State != entity.StateTerminated && !superseded[r.ID]
}

// usesInheritance reports whether any entity declares inherits or has a
// rule declaring overrides.
func usesInheritance(entities map[string]*entity.File) bool {
	for _, e := range entities {
		if e.Entity.Inherits != "" {
			return true
		}
		for _, r := range e.Rules {
			if r.Overrides != nil {
				return true
			}
		}
	}
	return false
}

// walk follows the inheritance chain of e, held by node start, and returns
// its ancestors nearest first. It reports RR013 for an unresolvable target
// and RR014 for a cycle or a chain longer than maxInheritHops.
func (s *chainState) walk(start *node, e *entity.File, path string) ([]inherit.Hop, bool) {
	chain := []string{start.ns + ":" + e.Entity.ID}
	seen := map[string]bool{chain[0]: true}
	var ancestors []inherit.Hop
	cur, curEnt := start, e
	for curEnt.Entity.Inherits != "" {
		if err := s.ctx.Err(); err != nil {
			s.res.Add(result.Diagnostic{Module: module, Code: "RR000", Severity: result.Error, Message: err.Error(), Path: path})
			return nil, false
		}
		if len(ancestors) == maxInheritHops {
			s.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR014",
				Severity: result.Error,
				Message:  fmt.Sprintf("entity %s: inheritance chain %s → %s exceeds %d hops", e.Entity.ID, strings.Join(chain, " → "), curEnt.Entity.Inherits, maxInheritHops),
				Path:     path,
			})
			return nil, false
		}
		next, target, failure := s.lookup(cur, curEnt.Entity.Inherits)
		if target == nil {
			who := e.Entity.ID
			if curEnt != e {
				who = fmt.Sprintf("%s (through %s)", e.Entity.ID, chain[len(chain)-1])
			}
			s.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR013",
				Severity: result.Error,
				Message:  fmt.Sprintf("entity %s: inherits %q cannot be resolved: %s", who, curEnt.Entity.Inherits, failure.reason),
				Path:     path,
			})
			return nil, false
		}
		key := next.ns + ":" + target.Entity.ID
		chain = append(chain, key)
		if seen[key] {
			s.res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR014",
				Severity: result.Error,
				Message:  fmt.Sprintf("entity %s: inheritance chain %s is cyclic", e.Entity.ID, strings.Join(chain, " → ")),
				Path:     path,
			})
			return nil, false
		}
		seen[key] = true
		ancestors = append(ancestors, inherit.Hop{RealmID: next.ns, SubRealmIDs: next.subRealmIDs, Entity: target})
		cur, curEnt = next, target
	}
	return ancestors, true
}

// lookupFailure says why lookup found no entity.
type lookupFailure struct {
	kind   failureKind
	reason string
}

// failureKind classifies a failed lookup.
type failureKind int

const (
	// noEntity: the Realm is known but does not hold the entity.
	noEntity failureKind = iota
	// undeclaredRealm: the qualifier is neither n's namespace, a dependency
	// of n's Realm nor a declared sub-realm of one.
	undeclaredRealm
	// unresolvedDependency: the Realm is declared but failed to resolve, and
	// its diagnostics were already reported.
	unresolvedDependency
)

// lookup resolves a reference to an entity made by an entity held by n. A
// short EUID, or one qualified with n's Realm or a sub-realm its manifest
// declares, names an entity of n's own namespace; any other qualifier must
// be a dependency of n's Realm or a declared sub-realm of one, loaded on
// demand for dependency Realms. On failure it returns a nil entity and the
// reason.
func (s *chainState) lookup(n *node, ref string) (*node, *entity.File, lookupFailure) {
	realmID, euid, qualified := strings.Cut(ref, ":")
	if !qualified {
		euid = ref
		realmID = n.ns
	}
	if realmID == n.ns || slices.Contains(n.subRealmIDs, realmID) {
		if e, ok := n.entities[euid]; ok {
			return s.nodeOf(n, euid), e, lookupFailure{}
		}
		problem := fmt.Sprintf("no entity %s in realm %s", euid, n.ns)
		if !qualified {
			if dep := s.knownDepHolding(n, euid); dep != "" {
				problem += fmt.Sprintf("; it exists in dependency %s, so use the qualified form %s:%s", dep, dep, euid)
			}
		}
		return nil, nil, lookupFailure{noEntity, problem}
	}
	depID := realmID
	dep, declared := s.dependency(n, realmID)
	if !declared {
		dep, depID, declared = s.dependencySubRealm(n, realmID)
	}
	switch {
	case !declared:
		return nil, nil, lookupFailure{undeclaredRealm, fmt.Sprintf("realm %s is not a dependency of realm %s", realmID, n.realm.Realm.Realm.ID)}
	case dep == nil:
		return nil, nil, lookupFailure{unresolvedDependency, fmt.Sprintf("dependency %s of realm %s could not be resolved", depID, n.realm.Realm.Realm.ID)}
	}
	e, ok := dep.Entities[euid]
	if !ok {
		return nil, nil, lookupFailure{noEntity, fmt.Sprintf("no entity %s in realm %s", euid, realmID)}
	}
	return s.depNode(dep), e, lookupFailure{}
}

// nodeOf returns the node holding euid within n's namespace: for the
// resolved Realm tree, the Realm or sub-realm whose files declare it.
func (s *chainState) nodeOf(n *node, euid string) *node {
	if t, ok := s.treeNode[euid]; ok && n.deps != nil && n.ns == t.ns {
		return t
	}
	return n
}

// knownDepHolding names an already resolved dependency of n that holds
// euid, for the RR013 hint on a short cross-realm reference.
func (s *chainState) knownDepHolding(n *node, euid string) string {
	var ids []string
	if n.deps != nil {
		for id := range n.deps {
			ids = append(ids, id)
		}
	} else {
		for _, d := range n.realm.Realm.Dependencies {
			ids = append(ids, d.RealmID)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		var dep *ResolvedRealm
		if n.deps != nil {
			dep = n.deps[id]
		} else {
			dep = s.loaded[id+"@"+depVersion(n.realm.Realm.Dependencies, id)]
		}
		if dep != nil {
			if _, ok := dep.Entities[euid]; ok {
				return id
			}
		}
	}
	return ""
}

// dependency returns the dependency realmID of n's Realm. declared is false
// when the Realm does not declare it. For a dependency Realm the Realm is
// resolved on first use, relative to its own directory and origin, and
// cached by realm_id@version; a nil Realm means it failed to resolve (its
// diagnostics were reported).
func (s *chainState) dependency(n *node, realmID string) (dep *ResolvedRealm, declared bool) {
	if n.deps != nil {
		d, ok := n.deps[realmID]
		if !ok {
			for _, decl := range n.realm.Realm.Dependencies {
				if decl.RealmID == realmID {
					return nil, true // declared but failed to resolve
				}
			}
		}
		return d, ok
	}
	for _, decl := range n.realm.Realm.Dependencies {
		if decl.RealmID != realmID {
			continue
		}
		key := decl.RealmID + "@" + decl.Version
		if d, ok := s.loaded[key]; ok {
			return d, true
		}
		sub := &RealmResolver{cfg: s.r.cfg}
		sub.cfg.Origin = n.realm.origin
		d := sub.resolveDep(s.ctx, decl, n.realm.Dir, n.realm.Dir, s.res)
		s.loaded[key] = d
		return d, true
	}
	return nil, false
}

// dependencySubRealm returns the dependency of n's Realm whose manifest
// declares realmID as one of its sub-realms, and that dependency's ID:
// entities of a sub-realm are found in the dependency's directory, and a
// qualifier naming the sub-realm is a qualifier of that dependency. A
// declared dependency that failed to resolve, and whose ID prefixes
// realmID, is returned as nil with declared true, so the failure is
// reported rather than a missing dependency. declared is false when no
// dependency matches.
func (s *chainState) dependencySubRealm(n *node, realmID string) (dep *ResolvedRealm, depID string, declared bool) {
	decls := make([]string, 0, len(n.realm.Realm.Dependencies))
	for _, d := range n.realm.Realm.Dependencies {
		decls = append(decls, d.RealmID)
	}
	sort.Strings(decls)
	for _, id := range decls {
		if !strings.HasPrefix(realmID, id+".") {
			continue
		}
		d, ok := s.dependency(n, id)
		if ok && d == nil {
			return nil, id, true
		}
		if d != nil && slices.Contains(subRealmIDs(d.Realm), realmID) {
			return d, id, true
		}
	}
	return nil, "", false
}

// depNode returns the node of a dependency Realm, built on first use. Its
// entities are the dependency directory's, sub-realm directories included;
// those entities resolve their own references against the dependency's
// root dependencies, not a sub-realm's merged list.
func (s *chainState) depNode(dep *ResolvedRealm) *node {
	if n, ok := s.nodes[dep]; ok {
		return n
	}
	n := &node{realm: dep, ns: dep.Realm.Realm.ID, subRealmIDs: subRealmIDs(dep.Realm), entities: dep.Entities}
	s.nodes[dep] = n
	return n
}

// depVersion returns the version deps declares for realmID.
func depVersion(deps []entity.RealmDep, realmID string) string {
	for _, d := range deps {
		if d.RealmID == realmID {
			return d.Version
		}
	}
	return ""
}

// checkOverrides checks every live overriding rule of child against the
// rule set it inherits from ancestors: RR015 when the target is not in that
// set, RR016 when the target exists in the chain but is not Published or
// Implemented, so the override has no effect. A rule is live unless it is
// Abandoned, Terminated or in superseded.
func checkOverrides(child inherit.Hop, ancestors []inherit.Hop, superseded map[string]bool, path string, res *result.Result) {
	var inherited map[string]inherit.Rule
	for _, r := range child.Entity.Rules {
		if r.Overrides == nil || !isLive(r, superseded) {
			continue
		}
		if child.Entity.Entity.Inherits == "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RR015",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s overrides %q but entity %s inherits nothing", r.ID, *r.Overrides, child.Entity.Entity.ID),
				Path:     path,
			})
			continue
		}
		if inherited == nil {
			inherited = make(map[string]inherit.Rule)
			for _, ir := range inherit.Inherited(ancestors) {
				inherited[ir.QualifiedID()] = ir
			}
		}
		target := inherit.Target(child, ancestors, *r.Overrides)
		if _, ok := inherited[target]; ok {
			continue
		}
		dead, why := explainMissing(target, ancestors, inherited)
		msg := fmt.Sprintf("rule %s overrides %q: %s", r.ID, *r.Overrides, why)
		if dead {
			res.Add(result.Diagnostic{Module: module, Code: "RR016", Severity: result.Warning, Message: msg, Path: path})
		} else {
			res.Add(result.Diagnostic{Module: module, Code: "RR015", Severity: result.Error, Message: msg, Path: path})
		}
	}
}

// explainMissing says why target, absent from the inherited set, is absent.
// dead is true when the target exists in the chain but is not Published or
// Implemented (RR016); otherwise it is already replaced by an ancestor's
// override or unknown to the chain (RR015).
func explainMissing(target string, ancestors []inherit.Hop, inherited map[string]inherit.Rule) (dead bool, why string) {
	for _, h := range ancestors {
		for _, r := range h.Entity.Rules {
			if h.RealmID+":"+r.ID != target {
				continue
			}
			if r.State != entity.StatePublished && r.State != entity.StateImplemented {
				return true, fmt.Sprintf("%s is in state %q in %s, so it is not inherited and the override has no effect", target, r.State, h.Entity.Entity.ID)
			}
			for _, ir := range inherited {
				if ir.Overrides == target {
					return false, fmt.Sprintf("%s is already replaced by %s in %s; override that rule instead", target, ir.QualifiedID(), ir.Owner)
				}
			}
			return false, fmt.Sprintf("%s is already replaced by an ancestor's override", target)
		}
	}
	return false, fmt.Sprintf("%s is not a rule of any inherited entity", target)
}
