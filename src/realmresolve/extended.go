package realmresolve

import (
	"context"
	"regexp"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// ruleRefRe matches a rule reference with or without a realm qualifier: group
// 1 is the realm ID, group 2 the EUID and group 3 the rule number.
var ruleRefRe = regexp.MustCompile(`\b(?:([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*):)?([A-Za-z0-9]{3,18})-(\d{3})\b`)

// resolveExtended maps each rule with an `extends` note, in the Realm, its
// sub-realms and their inheritance chains, to the live rules the note names,
// one hop only. An ancestor's references resolve from the ancestor's Realm.
// It reports nothing: checkNoteRefs and resolveInheritance have reported what
// the Realm answers for, and a reference that fails here is just left out.
func (r *RealmResolver) resolveExtended(ctx context.Context, root *ResolvedRealm, subs []*ResolvedRealm) map[string][]inherit.Extended {
	entities, paths := treeEntities(root, subs)
	s := r.newChainState(ctx, root, subs, entities, &result.Result{})
	out := make(map[string][]inherit.Extended)
	visited := make(map[*entity.File]bool)
	for _, id := range sortedIDs(entities) {
		if ctx.Err() != nil {
			return out
		}
		e := entities[id]
		s.collectExtended(s.treeNode[id], e, visited, out)
		if e.Entity.Inherits == "" {
			continue
		}
		ancestors, ok := s.walk(s.treeNode[id], e, paths[id])
		if !ok {
			continue
		}
		for _, hop := range ancestors {
			s.collectExtended(s.holder[hop.Entity], hop.Entity, visited, out)
		}
	}
	return out
}

// collectExtended adds the targets of the `extends` notes of e, held by node
// at, to out.
func (s *chainState) collectExtended(at *node, e *entity.File, visited map[*entity.File]bool, out map[string][]inherit.Extended) {
	if visited[e] {
		return
	}
	visited[e] = true
	seen := make(map[string]bool)
	for _, n := range e.Notes {
		if n.Type != "extends" {
			continue
		}
		key := at.ns + ":" + n.RuleRef
		for _, m := range ruleRefRe.FindAllStringSubmatch(n.Content, -1) {
			if seen[n.RuleRef+"\x00"+m[0]] {
				continue
			}
			seen[n.RuleRef+"\x00"+m[0]] = true
			ref := m[2]
			if m[1] != "" {
				ref = m[1] + ":" + m[2]
			}
			next, target, _ := s.lookup(at, ref)
			if target == nil {
				continue
			}
			rule, ok := findRule(target, m[2]+"-"+m[3])
			if !ok || s.notLive(next, rule) != "" {
				continue
			}
			out[key] = append(out[key], inherit.Extended{ID: m[0], Revision: rule.Revision, Body: rule.Body})
		}
	}
}
