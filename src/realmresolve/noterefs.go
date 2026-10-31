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

// inlineCodeRe matches text in backticks, which quotes a reference rather
// than making one.
var inlineCodeRe = regexp.MustCompile("`[^`]*`")

// relationNoteTypes are the note types whose content references other rules
// (NTE-007).
var relationNoteTypes = map[string]bool{
	"supersedes": true,
	"extends":    true,
	"related":    true,
}

// checkNoteRefs checks the realm-qualified references of live rules of the
// root Realm and its sub-realms. In their relation notes: RR017 when the Realm
// is not in the tree, a dependency of the note's Realm or a declared sub-realm
// of one, RR018 when it holds no such rule, RR019 when that rule is no longer
// live, except from a supersedes note, which names a rule it replaces. In
// their bodies, as warnings: RR019, RR020 and RR021 for the same three faults;
// no rule requires a body to reference anything. A reference to a dependency
// that failed to resolve adds nothing to that failure. Dead rules are skipped:
// their notes cannot be changed (NTE-010) and nothing relies on them. `extends`
// notes also get RR025 (NTE-012), which spares fewer rules: see outOfForce.
// Dependencies of the tree are already resolved, so nothing loads.
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
		gone := make(map[string]bool)
		for _, rule := range e.Rules {
			if isLive(rule, superseded) {
				s.checkBody(s.treeNode[id], rule, paths[id])
			} else {
				dead[rule.ID] = true
			}
			gone[rule.ID] = outOfForce(rule.State)
		}
		for _, n := range e.Notes {
			if relationNoteTypes[n.Type] && !dead[n.RuleRef] {
				s.checkNote(s.treeNode[id], n, paths[id])
			}
			if n.Type == "extends" && !gone[n.RuleRef] {
				s.checkExtendsVisibility(s.treeNode[id], e, n, paths[id])
			}
		}
	}
}

// refFault is what is wrong with a qualified reference.
type refFault int

const (
	// refOK is a reference to a live rule, or one that cannot be judged
	// because its dependency failed to resolve.
	refOK refFault = iota
	// refUndeclaredRealm names a Realm the referrer cannot reach.
	refUndeclaredRealm
	// refMissingRule names a reachable Realm that holds no such rule.
	refMissingRule
	// refDeadRule names a rule that is no longer live.
	refDeadRule
)

// checkRef judges the reference matched by qualifiedRuleRefRe, made from node
// at.
func (s *chainState) checkRef(at *node, m []string) (refFault, string) {
	ruleID := m[2] + "-" + m[3]
	next, target, failure := s.lookup(at, m[1]+":"+m[2])
	switch {
	case target != nil:
		rule, ok := findRule(target, ruleID)
		if !ok {
			return refMissingRule, fmt.Sprintf("no rule %s in realm %s", ruleID, m[1])
		}
		if why := s.notLive(next, rule); why != "" {
			return refDeadRule, why
		}
		return refOK, ""
	case failure.kind == undeclaredRealm:
		return refUndeclaredRealm, failure.reason
	case failure.kind == noEntity:
		return refMissingRule, failure.reason
	default:
		return refOK, "" // unresolvedDependency
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
		fault, why := s.checkRef(at, m)
		var code string
		severity := result.Error
		switch fault {
		case refUndeclaredRealm:
			code = "RR017"
		case refMissingRule:
			code = "RR018"
		case refDeadRule:
			if n.Type == "supersedes" {
				continue
			}
			code = "RR019"
			severity = result.Warning
		default:
			continue
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     code,
			Severity: severity,
			Message:  fmt.Sprintf("note %s (%s) references %q: %s", n.ID, n.Type, m[0], why),
			Path:     path,
		})
	}
}

// checkBody reports the unresolved qualified references in the body of rule,
// held by tree node at. Text in backticks is ignored, as in rule-lint.
func (s *chainState) checkBody(at *node, rule entity.Rule, path string) {
	reported := make(map[string]bool)
	for _, m := range qualifiedRuleRefRe.FindAllStringSubmatch(inlineCodeRe.ReplaceAllString(rule.Body, " "), -1) {
		if reported[m[0]] {
			continue
		}
		reported[m[0]] = true
		fault, why := s.checkRef(at, m)
		var code string
		switch fault {
		case refDeadRule:
			code = "RR019"
		case refUndeclaredRealm:
			code = "RR020"
		case refMissingRule:
			code = "RR021"
		default:
			continue
		}
		s.res.Add(result.Diagnostic{
			Module:   module,
			Code:     code,
			Severity: result.Warning,
			Message:  fmt.Sprintf("rule %s body references %q: %s", rule.ID, m[0], why),
			Path:     path,
		})
	}
}

// notLive explains why rule, held in node at, is no longer live; empty when
// it is live.
func (s *chainState) notLive(at *node, rule entity.Rule) string {
	superseded, ok := s.superseded[at]
	if !ok {
		superseded = supersededRules(at.entities)
		s.superseded[at] = superseded
	}
	switch {
	case isLive(rule, superseded):
		return ""
	case rule.State == entity.StateAbandoned:
		return fmt.Sprintf("rule %s is Abandoned", rule.ID)
	case rule.State == entity.StateTerminated:
		return fmt.Sprintf("rule %s is Terminated", rule.ID)
	default:
		return fmt.Sprintf("rule %s is superseded", rule.ID)
	}
}

// findRule returns the rule of e with the given id, and whether e holds it.
func findRule(e *entity.File, id string) (entity.Rule, bool) {
	for _, r := range e.Rules {
		if r.ID == id {
			return r, true
		}
	}
	return entity.Rule{}, false
}
