// Package ruleeval provides AI-powered evaluation of code changes against
// SteerSpec rules. It supports pluggable providers (Claude, OpenAI, Ollama)
// and a static-only mode for structural checks without AI.
package ruleeval

import (
	"context"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Verdict represents the evaluation outcome for a single rule.
type Verdict string

// Verdict values for rule evaluation outcomes.
const (
	Compliant     Verdict = "compliant"
	Violated      Verdict = "violated"
	NotApplicable Verdict = "not_applicable"
)

// EvalRequest contains the inputs for evaluating a single rule.
type EvalRequest struct {
	Rule entity.Rule
	Diff string // code diff to evaluate
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
	StaticOnly bool // skip AI evaluation, structural checks only
}

// Option configures the Evaluator.
type Option func(*Config)

// WithStaticOnly disables AI evaluation.
func WithStaticOnly(b bool) Option {
	return func(c *Config) { c.StaticOnly = b }
}

// Evaluator evaluates code changes against rules.
type Evaluator struct {
	provider Provider
	cfg      Config
}

// New creates an Evaluator with the given provider and options.
// Provider may be nil if StaticOnly is enabled; otherwise it is required.
func New(provider Provider, opts ...Option) *Evaluator {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if provider == nil && !cfg.StaticOnly {
		panic("ruleeval: provider is required when StaticOnly is not enabled")
	}
	return &Evaluator{provider: provider, cfg: cfg}
}

// Evaluate checks code against the given rules and returns diagnostics.
func (e *Evaluator) Evaluate(_ context.Context, _ []entity.Rule, _ string) *result.Result {
	// TODO: implement evaluation loop
	return &result.Result{}
}
