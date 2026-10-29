// Package ruleeval provides AI-powered evaluation of code changes against
// SteerSpec rules. It supports pluggable providers and a static-only mode
// for structural checks without AI.
//
// A Provider evaluates one rule at a time. Providers may additionally
// implement BatchProvider (one request per entity), UsageReporter and
// Describer; the Evaluator detects these capabilities at runtime.
package ruleeval

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "rule-eval"

// Diagnostic codes emitted by rule-eval.
const (
	// CodeViolated (RE001) reports that the diff violates a rule: Error when
	// the rule's state is in fail_on, Warning otherwise.
	CodeViolated = "RE001"
	// CodeProviderError (RE002) reports that the AI provider failed for a rule
	// or entity, or returned no usable verdict.
	CodeProviderError = "RE002"
	// CodeStaticOnly (RE003) reports that AI evaluation was skipped because the
	// evaluator runs in static-only mode.
	CodeStaticOnly = "RE003"
	// CodeNoDiff (RE004) reports that the diff is empty and there is nothing to
	// evaluate.
	CodeNoDiff = "RE004"
)

// Verdict represents the evaluation outcome for a single rule.
type Verdict string

// Verdict values for rule evaluation outcomes.
const (
	// Compliant means the diff touches the rule's subject and respects it.
	Compliant Verdict = "compliant"
	// Violated means an added or changed line breaks the rule.
	Violated Verdict = "violated"
	// NotApplicable means the diff does not touch the rule's subject at all.
	NotApplicable Verdict = "not_applicable"
)

// RuleInput bundles a rule with its relevant notes for evaluation.
// The caller constructs these from entity.File data; the evaluator
// iterates over them without needing knowledge of file structure.
//
// EntityID groups rules into batch requests. Source and Scope describe the
// configured rule source the rule came from ("global" or "local") and are
// surfaced to the provider so that local rules are judged only against
// files under their source directory.
type RuleInput struct {
	// Rule is the rule to evaluate.
	Rule entity.Rule
	// Notes lists the notes attached to Rule.
	Notes []entity.Note
	// EntityID is the ID of the entity that owns Rule; it groups rules into
	// batch requests.
	EntityID string
	// Source is the configured rule source the rule came from, such as a local
	// path or a github:// reference.
	Source string
	// Scope is the source scope, "global" or "local"; local rules are judged
	// only against files under their source directory.
	Scope string
	// Owner is the qualified ID (<realm-id>:<EUID>) of the entity whose rules
	// array holds an inherited Rule; empty for the entity's own rules.
	Owner string
	// InheritedBy is the EUID of the entity that inherits Rule (ENT-009),
	// empty for an entity's own rules. An inherited rule keeps the
	// inheriting entity's EntityID, so it is judged with that entity's rules.
	InheritedBy string
	// Overrides is the qualified ID of the inherited rule that Rule replaces
	// for its entity (RUL-009), when it replaced one.
	Overrides string
	// OverriddenBy lists the rules that replace Rule in the entities
	// inheriting it, as `<qualified rule id> (<entity title>)`; Rule does not
	// apply where those entities' subject applies.
	OverriddenBy []string
}

// RuleInputsFromEffective builds RuleInputs from the effective rule set of
// the entity entityID, as computed by inherit.Effective. Every input
// carries entityID; inherited rules also carry their owner and inheritor.
// An inherited rule whose ID is already used in the set (two Realms of the
// chain reuse an EUID) is grouped apart, under "<entityID> via <owner>",
// because verdicts are keyed by rule ID within a group.
func RuleInputsFromEffective(entityID string, rules []inherit.Rule) []RuleInput {
	seen := make(map[string]bool, len(rules))
	for _, r := range rules {
		if r.InheritedBy == "" {
			seen[r.Rule.ID] = true
		}
	}
	inputs := make([]RuleInput, 0, len(rules))
	for _, r := range rules {
		in := RuleInput{Rule: r.Rule, Notes: r.Notes, EntityID: entityID, Overrides: r.Overrides}
		if r.InheritedBy != "" {
			in.Owner = r.OwnerRealm + ":" + r.Owner
			in.InheritedBy = r.InheritedBy
			if seen[r.Rule.ID] {
				in.EntityID = entityID + " via " + in.Owner
			}
			seen[r.Rule.ID] = true
		}
		inputs = append(inputs, in)
	}
	return inputs
}

// AnnotateOverriddenBy sets OverriddenBy on the inputs that are their
// entity's own rules (not inherited) in the Realm realmID, from a map of
// qualified rule ID → overriding rules such as inherit.OverriddenBy
// returns. Callers use it on the rules of a configured source whose Realm
// other evaluated entities inherit from.
func AnnotateOverriddenBy(inputs []RuleInput, realmID string, overriddenBy map[string][]string) {
	for i := range inputs {
		if inputs[i].InheritedBy != "" {
			continue
		}
		if by := overriddenBy[realmID+":"+inputs[i].Rule.ID]; len(by) > 0 {
			inputs[i].OverriddenBy = by
		}
	}
}

// RuleInputsFromFile extracts RuleInputs from an entity file, grouping
// each rule with the notes that reference it. Sub-entities are recursed and
// each input carries the ID of the entity that owns it.
func RuleInputsFromFile(f *entity.File) []RuleInput {
	if f == nil {
		return nil
	}

	notesByRef := make(map[string][]entity.Note, len(f.Notes))
	for _, n := range f.Notes {
		notesByRef[n.RuleRef] = append(notesByRef[n.RuleRef], n)
	}

	inputs := make([]RuleInput, 0, len(f.Rules))
	for _, r := range f.Rules {
		inputs = append(inputs, RuleInput{
			Rule:     r,
			Notes:    notesByRef[r.ID],
			EntityID: f.Entity.ID,
		})
	}

	for i := range f.SubEntities {
		inputs = append(inputs, RuleInputsFromFile(&f.SubEntities[i])...)
	}
	return inputs
}

// EvalRequest contains the inputs for evaluating a single rule. EntityID,
// Source and Scope mirror the RuleInput metadata so that a per-rule
// provider can restrict a local-scope rule to its source directory exactly
// as the batch path does.
type EvalRequest struct {
	// Rule is the rule to evaluate.
	Rule entity.Rule
	// Notes are the contextual notes (rationale, examples, applies_to, …).
	Notes []entity.Note
	// Diff is the code diff to evaluate.
	Diff string
	// EntityID is the ID of the entity that owns Rule.
	EntityID string
	// Source is the configured rule source the rule came from.
	Source string
	// Scope is the source scope, "global" or "local".
	Scope string
	// Owner, InheritedBy, Overrides and OverriddenBy carry the inheritance
	// metadata of the RuleInput (see RuleInput).
	Owner        string
	InheritedBy  string
	Overrides    string
	OverriddenBy []string
}

// Input returns the RuleInput equivalent of the request.
func (r EvalRequest) Input() RuleInput {
	return RuleInput{
		Rule: r.Rule, Notes: r.Notes, EntityID: r.EntityID, Source: r.Source, Scope: r.Scope,
		Owner: r.Owner, InheritedBy: r.InheritedBy, Overrides: r.Overrides, OverriddenBy: r.OverriddenBy,
	}
}

// EvalResponse contains the AI provider's evaluation of a single rule.
type EvalResponse struct {
	// Verdict is the evaluation outcome.
	Verdict Verdict `json:"verdict"`
	// Explanation is the provider's reasoning for the verdict.
	Explanation string `json:"explanation"`
	// Confidence is the provider's certainty from 0 to 1, when reported.
	Confidence float64 `json:"confidence,omitempty"`
	// Evidence lists the diff locations that support the verdict, when any.
	Evidence []Location `json:"evidence,omitempty"`
}

// Provider is implemented by AI backends (Anthropic, OpenAI, Ollama, etc.).
type Provider interface {
	// Evaluate judges one rule against the diff in req.
	Evaluate(ctx context.Context, req EvalRequest) (*EvalResponse, error)
}

// Config holds options for the Evaluator.
type Config struct {
	// StaticOnly skips AI evaluation and runs structural checks only.
	StaticOnly bool
	// FailOn lists the state codes that produce Error severity on violation.
	FailOn map[string]bool
	// EvaluateStates lists the state codes that are evaluated at all.
	EvaluateStates map[string]bool
	// Cache is the optional verdict cache.
	Cache Cache
	// Concurrency is the max number of concurrent provider calls (default 1).
	Concurrency int
	// Observer is the optional progress observer.
	Observer Observer
}

// Option configures the Evaluator.
type Option func(*Config)

// WithStaticOnly disables AI evaluation.
func WithStaticOnly(b bool) Option {
	return func(c *Config) { c.StaticOnly = b }
}

// WithFailOn sets the rule states whose violations produce Error-severity
// diagnostics. States not in this set produce Warning-severity diagnostics.
func WithFailOn(states []string) Option {
	return func(c *Config) {
		c.FailOn = make(map[string]bool, len(states))
		for _, s := range states {
			c.FailOn[s] = true
		}
	}
}

// WithEvaluateStates sets the rule states that are sent to the provider.
// Rules in other states are skipped. Defaults to DefaultEvaluateStates.
func WithEvaluateStates(states []string) Option {
	return func(c *Config) {
		c.EvaluateStates = make(map[string]bool, len(states))
		for _, s := range states {
			c.EvaluateStates[s] = true
		}
	}
}

// WithCache enables verdict caching. The provider must implement Describer
// so that the model and prompt version can be mixed into every key; New
// returns an error otherwise.
func WithCache(cache Cache) Option {
	return func(c *Config) { c.Cache = cache }
}

// WithConcurrency sets the maximum number of concurrent provider calls.
// Values below 1 are treated as 1.
func WithConcurrency(n int) Option {
	return func(c *Config) { c.Concurrency = n }
}

// WithObserver registers an Observer for progress events.
func WithObserver(o Observer) Option {
	return func(c *Config) { c.Observer = o }
}

// Evaluator evaluates code changes against rules.
type Evaluator struct {
	provider Provider
	cfg      Config
}

// New creates an Evaluator with the given provider and options.
// Provider may be nil if StaticOnly is enabled; otherwise it is required.
// Returns an error if any fail_on or evaluate state code is not a valid
// lifecycle state.
func New(provider Provider, opts ...Option) (*Evaluator, error) {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if provider == nil && !cfg.StaticOnly {
		return nil, fmt.Errorf("ruleeval: provider is required when StaticOnly is not enabled")
	}
	for s := range cfg.FailOn {
		if !entity.IsValidState(s) {
			return nil, fmt.Errorf("ruleeval: invalid fail_on state code: %q", s)
		}
	}
	if cfg.EvaluateStates == nil {
		WithEvaluateStates(DefaultEvaluateStates)(&cfg)
	}
	for s := range cfg.EvaluateStates {
		if !entity.IsValidState(s) {
			return nil, fmt.Errorf("ruleeval: invalid evaluate state code: %q", s)
		}
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 1
	}
	if cfg.Cache != nil && !cfg.StaticOnly {
		d, ok := provider.(Describer)
		if !ok {
			return nil, fmt.Errorf("ruleeval: WithCache requires a provider implementing Describer (model and prompt version are part of the cache key)")
		}
		if d.Model() == "" || d.PromptVersion() == "" {
			return nil, fmt.Errorf("ruleeval: WithCache requires a non-empty model and prompt version from the provider (got model %q, prompt version %q)", d.Model(), d.PromptVersion())
		}
	}
	return &Evaluator{provider: provider, cfg: cfg}, nil
}

// Usage returns the accumulated provider usage, or the zero Usage when the
// provider does not implement UsageReporter.
func (e *Evaluator) Usage() Usage {
	if r, ok := e.provider.(UsageReporter); ok {
		return r.Usage()
	}
	return Usage{}
}

func (e *Evaluator) observe(ev Event) {
	if e.cfg.Observer != nil {
		e.cfg.Observer.Observe(ev)
	}
}

// outcome is the per-input result collected before diagnostics are emitted
// in input order.
type outcome struct {
	resp  *EvalResponse
	err   error
	fresh bool // obtained from the provider (not the cache)
	done  bool // an outcome was recorded
}

// group is the set of pending inputs (by index) that share an entity.
type group struct {
	entityID string
	idx      []int
}

// Evaluate checks the diff against each rule input and returns diagnostics.
// Provider errors are non-fatal: they emit a warning and evaluation continues.
// If the context is cancelled, partial results collected so far are returned.
//
// Rules whose state is not in the evaluate set are skipped. Rules are
// grouped by EntityID; when the provider implements BatchProvider each
// group is sent as one request, otherwise rules are evaluated one by one.
// Groups run concurrently up to the configured concurrency; diagnostics are
// always emitted in input order.
func (e *Evaluator) Evaluate(ctx context.Context, inputs []RuleInput, diff string) *result.Result {
	res := &result.Result{}

	if e.cfg.StaticOnly {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     CodeStaticOnly,
			Severity: result.Info,
			Message:  "AI evaluation skipped: static-only mode",
		})
		return res
	}

	if strings.TrimSpace(diff) == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     CodeNoDiff,
			Severity: result.Info,
			Message:  "nothing to evaluate: diff is empty",
		})
		return res
	}

	outcomes := make([]outcome, len(inputs))
	keys := make([]string, len(inputs))
	groups := e.plan(inputs, diff, outcomes, keys)
	groupErrs := e.run(ctx, groups, inputs, diff, outcomes)

	for i, input := range inputs {
		o := outcomes[i]
		if !o.done {
			continue // skipped, or never reached because the context was cancelled
		}
		if o.err != nil {
			res.Add(providerError(input.Rule.ID, o.err))
			continue
		}
		if o.resp == nil {
			res.Add(providerError(input.Rule.ID, fmt.Errorf("provider returned nil response without error")))
			continue
		}
		switch o.resp.Verdict {
		case Compliant, NotApplicable:
			// no diagnostic
		case Violated:
			sev := result.Warning
			if e.cfg.FailOn[input.Rule.State] {
				sev = result.Error
			}
			d := result.Diagnostic{
				Module:   module,
				Code:     CodeViolated,
				Severity: sev,
				Message:  fmt.Sprintf("rule %s violated: %s", input.Rule.ID, o.resp.Explanation),
			}
			if len(o.resp.Evidence) > 0 {
				d.Path = o.resp.Evidence[0].File
				d.Line = o.resp.Evidence[0].Line
			}
			res.Add(d)
		default:
			res.Add(providerError(input.Rule.ID, fmt.Errorf("unrecognized verdict %q", o.resp.Verdict)))
			continue
		}
		if o.fresh && e.cfg.Cache != nil {
			if err := e.cfg.Cache.Put(keys[i], o.resp); err != nil {
				e.observe(Event{Kind: EventCacheError, RuleID: input.Rule.ID, Err: err})
			}
		}
	}

	for _, ge := range groupErrs {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     CodeProviderError,
			Severity: result.Warning,
			Message:  fmt.Sprintf("evaluation incomplete for entity %s: %v", ge.entityID, ge.err),
		})
	}

	return res
}

// plan filters inputs by state, consults the cache and groups the
// remaining inputs by entity in first-seen order.
func (e *Evaluator) plan(inputs []RuleInput, diff string, outcomes []outcome, keys []string) []group {
	var model, promptVersion, diffHash string
	if e.cfg.Cache != nil {
		d := e.provider.(Describer) // enforced by New
		model, promptVersion = d.Model(), d.PromptVersion()
		diffHash = DiffHash(diff)
	}

	var groups []group
	byEntity := map[string]int{}
	for i, in := range inputs {
		if !e.cfg.EvaluateStates[in.Rule.State] {
			e.observe(Event{Kind: EventSkipped, EntityID: in.EntityID, RuleID: in.Rule.ID})
			continue
		}
		if e.cfg.Cache != nil {
			keys[i] = CacheKey(in, diffHash, model, promptVersion)
			resp, ok, err := e.cfg.Cache.Get(keys[i])
			switch {
			case err != nil:
				e.observe(Event{Kind: EventCacheError, RuleID: in.Rule.ID, Err: err})
			case ok && resp != nil:
				outcomes[i] = outcome{resp: resp, done: true}
				e.observe(Event{Kind: EventCacheHit, RuleID: in.Rule.ID})
				continue
			default:
				e.observe(Event{Kind: EventCacheMiss, RuleID: in.Rule.ID})
			}
		}
		gi, seen := byEntity[in.EntityID]
		if !seen {
			gi = len(groups)
			byEntity[in.EntityID] = gi
			groups = append(groups, group{entityID: in.EntityID})
		}
		groups[gi].idx = append(groups[gi].idx, i)
	}
	return groups
}

type groupError struct {
	entityID string
	err      error
}

// run evaluates every group, concurrently up to cfg.Concurrency, writing
// into outcomes. It stops scheduling new groups once ctx is cancelled.
// Group errors are returned in group (first-seen entity) order regardless
// of which goroutine finished first, so diagnostics stay deterministic.
func (e *Evaluator) run(ctx context.Context, groups []group, inputs []RuleInput, diff string, outcomes []outcome) []groupError {
	var (
		perGroup = make([][]groupError, len(groups)) // each goroutine owns one slot
		wg       sync.WaitGroup
		sem      = make(chan struct{}, e.cfg.Concurrency)
	)
	for gi, g := range groups {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(gi int, g group) {
			defer wg.Done()
			defer func() { <-sem }()
			perGroup[gi] = e.runGroup(ctx, g, inputs, diff, outcomes)
		}(gi, g)
	}
	wg.Wait()

	var groupErrs []groupError
	for _, errs := range perGroup {
		groupErrs = append(groupErrs, errs...)
	}
	return groupErrs
}

// runGroup evaluates one entity's pending rules. Each goroutine writes only
// to the outcome slots it owns, so no locking is needed on outcomes.
func (e *Evaluator) runGroup(ctx context.Context, g group, inputs []RuleInput, diff string, outcomes []outcome) []groupError {
	ids := make([]string, len(g.idx))
	for n, i := range g.idx {
		ids[n] = inputs[i].Rule.ID
	}

	bp, ok := e.provider.(BatchProvider)
	if !ok {
		for _, i := range g.idx {
			if ctx.Err() != nil {
				return nil
			}
			in := inputs[i]
			resp, err := e.provider.Evaluate(ctx, EvalRequest{
				Rule: in.Rule, Notes: in.Notes, Diff: diff,
				EntityID: in.EntityID, Source: in.Source, Scope: in.Scope,
				Owner: in.Owner, InheritedBy: in.InheritedBy, Overrides: in.Overrides, OverriddenBy: in.OverriddenBy,
			})
			e.observe(Event{Kind: EventProviderCall, EntityID: g.entityID, RuleIDs: []string{in.Rule.ID}, Err: err})
			outcomes[i] = outcome{resp: resp, err: err, fresh: err == nil, done: true}
		}
		return nil
	}

	if ctx.Err() != nil {
		return nil
	}
	req := BatchRequest{EntityID: g.entityID, Diff: diff, Inputs: make([]RuleInput, 0, len(g.idx))}
	for _, i := range g.idx {
		req.Inputs = append(req.Inputs, inputs[i])
	}
	resp, err := bp.EvaluateBatch(ctx, req)
	e.observe(Event{Kind: EventProviderCall, EntityID: g.entityID, RuleIDs: ids, Err: err})
	if err == nil && resp == nil {
		err = fmt.Errorf("provider returned nil batch response without error")
	}
	if err != nil {
		for _, i := range g.idx {
			outcomes[i] = outcome{err: err, done: true}
		}
		return nil
	}
	// Verdicts from a partially evaluated batch (for example a hunk that
	// was too large to send) are not cached, so that a rerun evaluates
	// again and surfaces the same warning instead of silently succeeding.
	cacheable := len(resp.Errors) == 0
	for _, i := range g.idx {
		v, found := resp.Verdicts[inputs[i].Rule.ID]
		if !found || v == nil {
			outcomes[i] = outcome{err: fmt.Errorf("provider returned no verdict"), done: true}
			continue
		}
		outcomes[i] = outcome{resp: v, fresh: cacheable, done: true}
	}
	var gerrs []groupError
	for _, pe := range resp.Errors {
		gerrs = append(gerrs, groupError{entityID: g.entityID, err: pe})
	}
	return gerrs
}

func providerError(ruleID string, err error) result.Diagnostic {
	return result.Diagnostic{
		Module:   module,
		Code:     CodeProviderError,
		Severity: result.Warning,
		Message:  fmt.Sprintf("evaluation failed for rule %s: %v", ruleID, err),
	}
}
