package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/anthropics/anthropic-sdk-go"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// auditInParts audits a file set too large for one request, in two steps.
// First every part, against every group of rules, collects the findings
// its files hold for each rule, and judges nothing: what a part lacks may
// sit in another. Then one request per group of rules judges every rule on
// the findings of all parts and the paths of the whole set, so something a
// rule requires that no part found is missing, as in an audit that fits one
// request.
//
// A rule whose findings could not be collected from every part, or whose
// findings are too large to judge in one request, gets no verdict and an
// error in out.Errors, which keeps the batch out of the cache.
func (p *Provider) auditInParts(ctx context.Context, req ruleeval.BatchRequest, out *ruleeval.BatchResponse) (*ruleeval.BatchResponse, error) {
	rp := p.planRules(prompt.AuditExtractSystem(), prompt.ExtractSchema, req.Inputs)
	out.Errors = append(out.Errors, rp.errs...)
	if len(rp.groups) == 0 {
		return out, nil
	}
	parts, _, unread, planErrs := planAudit(req.Files, rp.budget)
	out.Errors = append(out.Errors, planErrs...)
	if len(parts) == 0 {
		return out, nil
	}

	var items []work
	for c := range parts {
		for g := range rp.groups {
			items = append(items, work{chunk: c, group: g})
		}
	}
	found := make([]extracted, len(items))
	if err := p.runAll(ctx, len(items), func(ctx context.Context, i int) error {
		var err error
		it := items[i]
		found[i], err = p.extract(ctx, prompt.FilesBlock(parts[it.chunk]), rp.groups[it.group], it.chunk+1, len(parts))
		return err
	}); err != nil {
		return nil, err
	}

	// Items run part by part, so each rule's findings keep the order of the
	// parts, and of the files within them.
	findings := map[string][]prompt.Finding{}
	failed := map[string]bool{}
	for _, f := range found {
		out.Errors = append(out.Errors, f.errs...)
		for id := range f.failed {
			failed[id] = true
		}
		for id, fs := range f.findings {
			findings[id] = append(findings[id], fs...)
		}
	}
	var toJudge []ruleeval.RuleInput
	for _, g := range rp.groups {
		for _, in := range g {
			if !failed[in.Rule.ID] {
				toJudge = append(toJudge, in)
			}
		}
	}

	jp := p.planJudge(toJudge, findings, filePaths(req.Files), unread)
	out.Errors = append(out.Errors, jp.errs...)
	results := make([]judged, len(jp.groups))
	if err := p.runAll(ctx, len(jp.groups), func(ctx context.Context, i int) error {
		var err error
		results[i], err = p.judge(ctx, jp.scope, jp.groups[i], findings)
		return err
	}); err != nil {
		return nil, err
	}
	for _, r := range results {
		out.Errors = append(out.Errors, r.errs...)
		for id, d := range r.details {
			out.Verdicts[id] = d.toResponse()
		}
	}
	return out, nil
}

// extracted is what a part yielded for a group of rules: the findings of
// each rule, the rules it could not answer for and why.
type extracted struct {
	findings map[string][]prompt.Finding
	failed   map[string]bool
	errs     []error
}

func (e extracted) merge(o extracted) extracted {
	if e.findings == nil {
		e.findings = map[string][]prompt.Finding{}
	}
	if e.failed == nil {
		e.failed = map[string]bool{}
	}
	for id, fs := range o.findings {
		e.findings[id] = append(e.findings[id], fs...)
	}
	for id := range o.failed {
		e.failed[id] = true
	}
	e.errs = append(e.errs, o.errs...)
	return e
}

// extract collects the findings of part n of total, rendered as block, for
// group. A response cut short by max_tokens is asked again for each half
// of the group, down to a single rule, which then fails alone.
func (p *Provider) extract(ctx context.Context, block string, group []ruleeval.RuleInput, n, total int) (extracted, error) {
	ids := ruleIDs(group)
	text, err := p.send(ctx, prompt.AuditExtractSystem(), block, []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(prompt.RulesBlock(group)),
	}, prompt.ExtractSchema(ids))
	if errors.Is(err, ErrTruncated) {
		if len(group) == 1 {
			return extracted{failed: map[string]bool{ids[0]: true}, errs: []error{fmt.Errorf(
				"the findings for rule %s in part %d of %d of the file set exceed max_tokens (%d), so it was not judged",
				ids[0], n, total, p.cfg.MaxTokens)}}, nil
		}
		half := len(group) / 2
		a, err := p.extract(ctx, block, group[:half], n, total)
		if err != nil {
			return extracted{}, err
		}
		b, err := p.extract(ctx, block, group[half:], n, total)
		if err != nil {
			return extracted{}, err
		}
		return a.merge(b), nil
	}
	if err != nil {
		return extracted{}, err
	}
	var got map[string][]prompt.Finding
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return extracted{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	out := extracted{findings: map[string][]prompt.Finding{}, failed: map[string]bool{}}
	requested := map[string]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	for _, id := range sortedKeys(got) {
		// Structured outputs should guarantee the enum, but a finding of
		// unknown kind must not pass for a fact or a violation, and makes
		// the whole response suspect, whichever rule it is under.
		for _, f := range got[id] {
			if f.Kind != prompt.FindingViolation && f.Kind != prompt.FindingFact {
				return extracted{}, fmt.Errorf("%w: finding for rule %s has unknown kind %q", ErrMalformed, id, f.Kind)
			}
		}
	}
	for _, id := range sortedKeys(got) {
		if !requested[id] {
			out.errs = append(out.errs, fmt.Errorf("provider returned findings for unrequested rule %q (ignored)", id))
			continue
		}
		out.findings[id] = got[id]
	}
	// A rule the part did not answer for has findings missing, and judged
	// without them something present could pass for missing.
	for _, id := range ids {
		if _, ok := got[id]; !ok {
			out.failed[id] = true
			out.errs = append(out.errs, fmt.Errorf(
				"provider returned no findings for rule %s in part %d of %d of the file set, so it was not judged", id, n, total))
		}
	}
	return out, nil
}

// judgePlan is how the rules of an audit in parts are judged: the list of
// the set's files every request carries, and the groups of rules whose
// findings fit one request with it. errs reports the paths left off the
// list and the rules whose findings fit no request.
type judgePlan struct {
	scope  string
	groups [][]ruleeval.RuleInput
	errs   []error
}

// planJudge packs inputs, in order, into groups of at most
// MaxRulesPerRequest rules whose rules, findings and part of the schema fit
// the input budget next to the list of paths and of what no part shows.
func (p *Provider) planJudge(inputs []ruleeval.RuleInput, findings map[string][]prompt.Finding, paths []string, unread []prompt.Unread) judgePlan {
	var jp judgePlan
	if len(inputs) == 0 {
		return jp
	}
	available := p.cfg.MaxInputTokens - estimateTokens(prompt.AuditJudgeSystem()) - requestOverheadTokens
	listed, unlisted := capFiles(paths, bytesForTokens(available)/fileListShare)
	if unlisted > 0 {
		// Without the full list a file the list does not name cannot be told
		// from one that is absent, so the result must not pass as complete.
		jp.errs = append(jp.errs, fmt.Errorf(
			"file set too large to list every file: %d of %d files left off, so rules were judged on less",
			unlisted, len(listed)+unlisted))
	}
	jp.scope = prompt.AuditScope(listed, unlisted, unread)
	budget := available - estimateTokens(jp.scope+prompt.FindingsBlock(nil, nil))

	var cur []ruleeval.RuleInput
	curTokens := 0
	for _, in := range inputs {
		n := ruleTokens(in) + estimateTokens(prompt.RuleFindings(in.Rule.ID, findings[in.Rule.ID])) +
			schemaTokens(prompt.JudgeSchema([]string{in.Rule.ID}))
		if n > budget {
			jp.errs = append(jp.errs, fmt.Errorf(
				"the findings for rule %s (~%d tokens with the rule and its schema) exceed the input budget of ~%d tokens, so it was not judged",
				in.Rule.ID, n, budget))
			continue
		}
		if len(cur) > 0 && (len(cur) >= p.cfg.MaxRulesPerRequest || curTokens+n > budget) {
			jp.groups = append(jp.groups, cur)
			cur, curTokens = nil, 0
		}
		cur = append(cur, in)
		curTokens += n
	}
	if len(cur) > 0 {
		jp.groups = append(jp.groups, cur)
	}
	return jp
}

// judged is the judgement of a group of rules: verdict, explanation,
// confidence and evidence per rule, and the rules it could not judge.
type judged struct {
	details map[string]wireDetail
	errs    []error
}

func (j judged) merge(o judged) judged {
	if j.details == nil {
		j.details = map[string]wireDetail{}
	}
	for id, d := range o.details {
		j.details[id] = d
	}
	j.errs = append(j.errs, o.errs...)
	return j
}

// judge judges group on its findings, next to scope. A response cut short
// by max_tokens is asked again for each half of the group, down to a
// single rule, which then fails alone.
func (p *Provider) judge(ctx context.Context, scope string, group []ruleeval.RuleInput, findings map[string][]prompt.Finding) (judged, error) {
	ids := ruleIDs(group)
	text, err := p.send(ctx, prompt.AuditJudgeSystem(), scope+prompt.FindingsBlock(ids, findings), []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(prompt.RulesBlock(group)),
	}, prompt.JudgeSchema(ids))
	if errors.Is(err, ErrTruncated) {
		if len(group) == 1 {
			return judged{errs: []error{fmt.Errorf(
				"the judgement of rule %s exceeds max_tokens (%d), so it was not judged", ids[0], p.cfg.MaxTokens)}}, nil
		}
		half := len(group) / 2
		a, err := p.judge(ctx, scope, group[:half], findings)
		if err != nil {
			return judged{}, err
		}
		b, err := p.judge(ctx, scope, group[half:], findings)
		if err != nil {
			return judged{}, err
		}
		return a.merge(b), nil
	}
	if err != nil {
		return judged{}, err
	}
	var got map[string]wireDetail
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		return judged{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	out := judged{details: map[string]wireDetail{}}
	requested := map[string]bool{}
	for _, id := range ids {
		requested[id] = true
	}
	for _, id := range sortedKeys(got) {
		// An unknown verdict makes the whole response suspect, whichever
		// rule it is under.
		switch ruleeval.Verdict(got[id].Verdict) {
		case ruleeval.Compliant, ruleeval.Violated, ruleeval.NotApplicable:
		default:
			return judged{}, fmt.Errorf("%w: rule %s has unknown verdict %q", ErrMalformed, id, got[id].Verdict)
		}
	}
	for _, id := range sortedKeys(got) {
		if !requested[id] {
			out.errs = append(out.errs, fmt.Errorf("provider returned a verdict for unrequested rule %q (ignored)", id))
			continue
		}
		out.details[id] = got[id]
	}
	for _, id := range ids {
		if _, ok := got[id]; !ok {
			out.errs = append(out.errs, fmt.Errorf("provider returned no verdict for rule %s in the judgement of the file set", id))
		}
	}
	return out, nil
}

// toResponse converts a judgement or a detail to the evaluator's shape.
func (d wireDetail) toResponse() *ruleeval.EvalResponse {
	r := &ruleeval.EvalResponse{
		Verdict:     ruleeval.Verdict(d.Verdict),
		Explanation: d.Explanation,
		Confidence:  d.Confidence,
	}
	for _, e := range d.Evidence {
		r.Evidence = append(r.Evidence, ruleeval.Location{File: e.File, Line: e.Line, Excerpt: e.Excerpt})
	}
	return r
}

// ruleIDs returns the rule id of every input, in order.
func ruleIDs(group []ruleeval.RuleInput) []string {
	ids := make([]string, len(group))
	for i, in := range group {
		ids[i] = in.Rule.ID
	}
	return ids
}

// sortedKeys returns the keys of m in order, so that errors come out the
// same on every run.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
