package ruleeval

import (
	"context"
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// CodeOverrideConflict reports a Draft override that conflicts with the
// inherited rule it overrides (RUL-023). It is a Warning whatever fail_on
// says: the Draft replaces nothing yet, and the inherited rule stays enforced.
const CodeOverrideConflict = "RE007"

// ConflictChecker is an optional capability of a Provider: it judges whether a
// Draft override conflicts with the inherited rule it overrides, so that the
// Evaluator can report it without failing (RUL-023). Two rules conflict when
// code that follows the overriding rule would violate the inherited one. The
// answer depends on the two rules alone, never on the diff or files, so the
// Evaluator caches it under both rules' texts. A wrapper around another
// provider must forward both methods, or no conflict is checked.
type ConflictChecker interface {
	// ConflictPromptVersion identifies the conflict prompt revision, which
	// cache keys mix in. An empty version disables the check.
	ConflictPromptVersion() string
	// CheckConflict judges req.Rule, a Draft override, against
	// req.Rule.Overridden.
	CheckConflict(ctx context.Context, req ConflictRequest) (*ConflictResponse, error)
}

// ConflictRequest asks whether a Draft override conflicts with the rule it
// overrides.
type ConflictRequest struct {
	// Rule is the Draft override; Rule.Overridden is the inherited rule.
	Rule RuleInput
}

// ConflictResponse is a ConflictChecker's answer.
type ConflictResponse struct {
	// Conflicts is true when code following Rule would violate the inherited
	// rule.
	Conflicts bool `json:"conflicts"`
	// Explanation says why, or why not.
	Explanation string `json:"explanation"`
}

// conflictKeyPrefix keeps conflict answers apart from verdicts in a Cache.
const conflictKeyPrefix = "conflict:"

// ConflictCacheKey is the cache key of a conflict check of in, a Draft
// override, against in.Overridden. It covers everything the check sends, the
// rule with its notes and the overridden rule (CacheKey's fields), with the
// model and the conflict prompt version, and no diff: the answer depends on
// the two rules alone.
func ConflictCacheKey(in RuleInput, model, promptVersion string) string {
	return conflictKeyPrefix + CacheKey(in, "", model, promptVersion)
}

// reportConflicts checks every Draft override among inputs that is evaluated
// against the rule it overrides, when the provider can and that rule is still
// in force (not already replaced by another rule of the entity), and reports each
// conflict as RE007 (RUL-023). A failed check is an RE002 warning. A cached
// answer is stored as a Violated (conflict) or Compliant EvalResponse.
func (e *Evaluator) reportConflicts(ctx context.Context, res *result.Result, inputs []RuleInput) {
	cc, ok := e.provider.(ConflictChecker)
	if !ok || cc.ConflictPromptVersion() == "" {
		return
	}
	var model string
	if d, ok := e.provider.(Describer); ok {
		model = d.Model()
	}
	for _, in := range inputs {
		if ctx.Err() != nil {
			return
		}
		if !in.draftOverride() || in.OverriddenReplaced || in.RealmKind == entity.KindStandard || !e.cfg.EvaluateStates[in.EnforcedState()] {
			continue
		}
		resp, err := e.checkConflict(ctx, cc, in, ConflictCacheKey(in, model, cc.ConflictPromptVersion()))
		switch {
		case err != nil:
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     CodeProviderError,
				Severity: result.Warning,
				Message:  fmt.Sprintf("conflict check failed for rule %s against %s: %v", in.Rule.ID, in.Overridden.ID, err),
			})
		case resp.Conflicts:
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     CodeOverrideConflict,
				Severity: result.Warning,
				Message: fmt.Sprintf("Draft rule %s conflicts with %s, which it proposes to override; %s stays in force until %s is Published (RUL-023): %s",
					in.Rule.ID, in.Overridden.ID, in.Overridden.ID, in.Rule.ID, resp.Explanation),
			})
		}
	}
}

// checkConflict returns the cached answer for key, or asks cc and caches it.
func (e *Evaluator) checkConflict(ctx context.Context, cc ConflictChecker, in RuleInput, key string) (*ConflictResponse, error) {
	if e.cfg.Cache != nil {
		cached, ok, err := e.cfg.Cache.Get(key)
		switch {
		case err != nil:
			e.observe(Event{Kind: EventCacheError, RuleID: in.Rule.ID, Err: err})
		case ok && cached != nil:
			e.observe(Event{Kind: EventCacheHit, RuleID: in.Rule.ID})
			return &ConflictResponse{Conflicts: cached.Verdict == Violated, Explanation: cached.Explanation}, nil
		}
	}
	resp, err := cc.CheckConflict(ctx, ConflictRequest{Rule: in})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("provider returned nil conflict response without error")
	}
	if e.cfg.Cache != nil {
		v := Compliant
		if resp.Conflicts {
			v = Violated
		}
		if err := e.cfg.Cache.Put(key, &EvalResponse{Verdict: v, Explanation: resp.Explanation}); err != nil {
			e.observe(Event{Kind: EventCacheError, RuleID: in.Rule.ID, Err: err})
		}
	}
	return resp, nil
}
