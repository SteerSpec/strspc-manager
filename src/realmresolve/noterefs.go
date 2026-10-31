package realmresolve

import (
	"context"
	"fmt"
	"regexp"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// qualifiedRuleRefRe matches a realm-qualified rule reference: group 1 is
// the realm ID, group 2 the EUID and group 3 the rule number. Unqualified
// references stay with rule-lint (RL014).
var qualifiedRuleRefRe = regexp.MustCompile(`\b([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*):([A-Za-z0-9]{3,18})-(\d{3})\b`)

// relationNoteTypes are the note types whose content references other rules
// (NTE-007).
var relationNoteTypes = map[string]bool{
	"supersedes": true,
	"extends":    true,
	"related":    true,
}

// checkNoteRefs checks the realm-qualified references in the relation notes
// of live rules of the root Realm and its sub-realms: RR017 when the Realm
// is not in the tree, a dependency of the note's Realm or a declared
// sub-realm of one, RR018 when it holds no such rule. A reference to a
// dependency that failed to resolve adds nothing to that failure. Notes on
// dead rules are skipped: they cannot be changed (NTE-010) and nothing relies
// on them. Dependencies of the tree are already resolved, so nothing loads.
func (r *RealmResolver) checkNoteRefs(ctx context.Context, root *ResolvedRealm, subs []*ResolvedRealm, res *result.Result) {
	entities, paths := treeEntities(root, subs)
	s := r.newChainState(ctx, root, subs, entities, res)
	superseded := supersededRules(entities)
	for _, id := range sortedIDs(entities) {
		if err := ctx.Err(); err != nil {
			res.Add(result.Diagnostic{Module: module, Code: "RR000", Severity: result.Error, Message: err.Error(), Path: root.Dir})
			return
		}
		e := entities[id]
		dead := make(map[string]bool)
		for _, rule := range e.Rules {
			if !isLive(rule, superseded) {
				dead[rule.ID] = true
			}
		}
		for _, n := range e.Notes {
			if relationNoteTypes[n.Type] && !dead[n.RuleRef] {
				s.checkNote(s.treeNode[id], n, paths[id])
			}
		}
	}
}

// checkNote reports the unresolved qualified references of note n, held by
// tree node at.
func (s *chainState) checkNote(at *node, n entity.Note, path string) {
	reported := make(map[string]bool)
	for _, m := range qualifiedRuleRefRe.FindAllStringSubmatch(n.Content, -1) {
		if reported[m[0]] {
			continue
		}
		reported[m[0]] = true
		ruleID := m[2] + "-" + m[3]
		_, target, failure := s.lookup(at, m[1]+":"+m[2])
		var code string
		why := failure.reason
		switch {
		case target != nil:
			if hasRule(target, ruleID) {
				continue
			}
			code = "RR018"
			why = fmt.Sprintf("no rule %s in realm %s", ruleID, m[1])
		case failure.kind == undeclaredRealm:
			code = "RR017"
		case failure.kind == noEntity:
			code = "RR018"
		default:
			continue // unresolvedDependency
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     code,
			Severity: result.Error,
			Message:  fmt.Sprintf("note %s (%s) references %q: %s", n.ID, n.Type, m[0], why),
			Path:     path,
		})
	}
}

// hasRule reports whether e holds the rule id.
func hasRule(e *entity.File, id string) bool {
	for _, r := range e.Rules {
		if r.ID == id {
			return true
		}
	}
	return false
}
