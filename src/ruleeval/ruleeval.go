// Package ruleeval provides AI-powered evaluation of code changes against
// SteerSpec rules. It supports pluggable providers (Claude, OpenAI, Ollama)
// and a static-only mode for structural checks without AI.
package ruleeval

import (
	"context"
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "rule-eval"

// Diagnostic codes emitted by rule-eval.
const (
	CodeViolated      = "RE001" // rule violated
	CodeProviderError = "RE002" // AI provider returned an error
	CodeStaticOnly    = "RE003" // AI evaluation skipped (static-only mode)
)

// validStates is the set of recognised rule lifecycle state codes.
var validStates = map[string]bool{
	"D": true, "A": true, "P": true,
	"I": true, "R": true, "T": true,
}

// Verdict represents the evaluation outcome for a single rule.
type Verdict string

// Verdict values for rule evaluation outcomes.
const (
	Compliant     Verdict = "compliant"
	Violated      Verdict = "violated"
	NotApplicable Verdict = "not_applicable"
)

// RuleInput bundles a rule with its relevant notes for evaluation.
// The caller constructs these from entity.File data; the evaluator
// iterates over them without needing knowledge of file structure.
type RuleInput struct {
	Rule  entity.Rule
	Notes []entity.Note
}

// RuleInputsFromFile extracts RuleInputs from an entity file, grouping
// each rule with the notes that reference it. Sub-entities are recursed.
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
			Rule:  r,
			Notes: notesByRef[r.ID],
		})
	}

	for i := range f.SubEntities {
		inputs = append(inputs, RuleInputsFromFile(&f.SubEntities[i])...)
	}
	return inputs
}

// EvalRequest contains the inputs for evaluating a single rule.
type EvalRequest struct {
	Rule  entity.Rule
	Notes []entity.Note // contextual notes (rationale, examples, applies_to, …)
	Diff  string        // code diff to evaluate
}

// EvalResponse contains the AI provider's evaluation of a single rule.
type EvalResponse struct {
	Verdict     Verdict
	Explanation string
}

// Provider is implemented by AI backends (Anthropic, OpenAI, Ollama, etc.).
type Provider interface {
	Evaluate(ctx context.Context, req EvalRequest) (*EvalResponse, error)
}

// Config holds options for the Evaluator.
type Config struct {
	StaticOnly bool            // skip AI evaluation, structural checks only
	FailOn     map[string]bool // state codes that produce Error severity on violation
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

// Evaluator evaluates code changes against rules.
type Evaluator struct {
	provider Provider
	cfg      Config
}

// New creates an Evaluator with the given provider and options.
// Provider may be nil if StaticOnly is enabled; otherwise it is required.
// Returns an error if any fail_on state code is not a valid lifecycle state.
func New(provider Provider, opts ...Option) (*Evaluator, error) {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if provider == nil && !cfg.StaticOnly {
		return nil, fmt.Errorf("ruleeval: provider is required when StaticOnly is not enabled")
	}
	for s := range cfg.FailOn {
		if !validStates[s] {
			return nil, fmt.Errorf("ruleeval: invalid fail_on state code: %q", s)
		}
	}
	return &Evaluator{provider: provider, cfg: cfg}, nil
}

// Evaluate checks the diff against each rule input and returns diagnostics.
// Provider errors are non-fatal: they emit a warning and evaluation continues.
// If the context is cancelled, partial results collected so far are returned.
func (e *Evaluator) Evaluate(ctx context.Context, inputs []RuleInput, diff string) *result.Result {
	res := &result.Result{}

	if e.cfg.StaticOnly {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     CodeStaticOnly,
			Severity: result.Info,
			Message:  "AI evaluation skipped: no provider configured (static-only mode)",
		})
		return res
	}

	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return res
		}

		req := EvalRequest{
			Rule:  input.Rule,
			Notes: input.Notes,
			Diff:  diff,
		}

		resp, err := e.provider.Evaluate(ctx, req)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     CodeProviderError,
				Severity: result.Warning,
				Message:  fmt.Sprintf("evaluation failed for rule %s: %v", input.Rule.ID, err),
			})
			continue
		}

		if resp.Verdict == Violated {
			sev := result.Warning
			if e.cfg.FailOn[input.Rule.State] {
				sev = result.Error
			}
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     CodeViolated,
				Severity: sev,
				Message:  fmt.Sprintf("rule %s violated: %s", input.Rule.ID, resp.Explanation),
			})
		}
	}

	return res
}
