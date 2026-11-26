package ruleeval

import (
	"fmt"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// maxSkippedNamed is how many rules an RE005 message names.
const maxSkippedNamed = 10

// reportStandardRules reports the rules that are not evaluated because their
// Realm's kind is standard (STDRLM-011). It runs however the run ends, also
// static-only and with nothing to evaluate: a change that relabels rules
// touches only rules/, which is never evaluated.
//
// RE006, an Error, is a skip that would hide a rule that can block or that
// can't be a transcription, so a relabel fails closed. RE005, a Warning, names
// the other rules skipped, so no skip is silent.
func (e *Evaluator) reportStandardRules(res *result.Result, inputs []RuleInput) {
	var skipped []string
	seen := make(map[string]bool)
	for i := range inputs {
		in := &inputs[i]
		if in.RealmKind != entity.KindStandard {
			continue
		}
		id := in.Rule.ID
		if reason := e.unsafeSkip(in); reason != "" {
			if key := id + "\x00" + reason; !seen[key] {
				seen[key] = true
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     CodeUnsafeSkip,
					Severity: result.Error,
					Message: fmt.Sprintf("rule %s was not evaluated, as its Realm declares the kind %q (STDRLM-011), but %s; "+
						"if these are the project's own rules, remove the kind from realm.json, "+
						"and if not, a transcription can't be judged against code, so don't enforce or verify it", id, entity.KindStandard, reason),
				})
			}
			continue
		}
		if e.cfg.EvaluateStates[in.EnforcedState()] && !slices.Contains(skipped, id) {
			skipped = append(skipped, id)
		}
	}
	if len(skipped) == 0 {
		return
	}
	names := strings.Join(skipped[:min(len(skipped), maxSkippedNamed)], ", ")
	if more := len(skipped) - maxSkippedNamed; more > 0 {
		names += fmt.Sprintf(" and %d more", more)
	}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     CodeStandardSkipped,
		Severity: result.Warning,
		Message: fmt.Sprintf("%d rule(s) not evaluated, as their Realm declares the kind %q (STDRLM-011): %s",
			len(skipped), entity.KindStandard, names),
	})
}

// unsafeSkip says why in, a rule labelled as a Standard Realm's, can't be
// skipped with a warning only, or returns "". States are read as the rule's
// enforced state (SPCFG-010).
func (e *Evaluator) unsafeSkip(in *RuleInput) string {
	r := &in.Rule
	state := in.EnforcedState()
	switch {
	case r.State == entity.StateImplemented:
		return "it is Implemented, which a rule of a Standard Realm can't be (STDRLM-010)"
	case state == entity.StateImplemented:
		return "it is enforced as Implemented (RUL-021), which a rule of a Standard Realm can't be (STDRLM-010)"
	}
	if !e.cfg.EvaluateStates[state] {
		return "" // It wouldn't be evaluated in a code Realm either.
	}
	switch {
	case e.cfg.FailOn[state]:
		return fmt.Sprintf("a violation of a rule in state %s blocks here (fail_on), and a skipped rule gets no verdict", state)
	case in.Overrides != "":
		return fmt.Sprintf("it overrides %s, which would then be enforced by neither rule", *r.Overrides)
	case r.Overrides != nil && *r.Overrides != "":
		// Fail closed before the override takes effect: once Published it
		// replaces its target with a rule that is never evaluated.
		return fmt.Sprintf("it declares overrides of %s, which it would replace, unevaluated, once Published", *r.Overrides)
	case strings.TrimSpace(r.Clause) == "" && entity.IsLiveStandardRule(*r):
		return "it has no clause, so it transcribes no standard (STDRLM-005)"
	}
	return ""
}
