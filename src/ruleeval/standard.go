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
		if reason := e.unsafeSkip(&in.Rule); reason != "" {
			if key := id + "\x00" + reason; !seen[key] {
				seen[key] = true
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     CodeUnsafeSkip,
					Severity: result.Error,
					Message: fmt.Sprintf("rule %s was not evaluated, as its Realm declares the kind %q (STDRLM-011), but %s; "+
						"if these are the project's own rules, remove the kind from realm.json", id, entity.KindStandard, reason),
				})
			}
			continue
		}
		if e.cfg.EvaluateStates[in.Rule.State] && !slices.Contains(skipped, id) {
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

// unsafeSkip says why r, a rule labelled as a Standard Realm's, can't be
// skipped with a warning only, or returns "".
func (e *Evaluator) unsafeSkip(r *entity.Rule) string {
	// Implemented is the enforced state, whatever the configuration says.
	if r.State == entity.StateImplemented {
		return "it is Implemented, which a rule of a Standard Realm can't be (STDRLM-010)"
	}
	if !e.cfg.EvaluateStates[r.State] {
		return "" // It wouldn't be evaluated in a code Realm either.
	}
	switch {
	case e.cfg.FailOn[r.State]:
		return fmt.Sprintf("its state %s is in fail_on, so skipping it would switch off a blocking rule", r.State)
	case r.Overrides != nil && *r.Overrides != "":
		return fmt.Sprintf("it overrides %s, which would then be enforced by neither rule", *r.Overrides)
	case strings.TrimSpace(r.Clause) == "" && entity.IsLiveStandardRule(*r):
		return "it has no clause, so it transcribes no standard (STDRLM-005)"
	}
	return ""
}
