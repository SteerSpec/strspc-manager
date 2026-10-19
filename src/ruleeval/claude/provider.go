// Package claude implements ruleeval.Provider and ruleeval.BatchProvider on
// top of the Anthropic Messages API using structured outputs. Each request
// asks for the verdicts of a group of rules as an object keyed by rule id;
// a second, per-rule request then collects explanation and evidence for
// every rule judged violated.
//
// One request carries the shared system prompt and the diff (both marked
// as prompt-cache breakpoints) followed by up to MaxRulesPerRequest rules.
// Diffs larger than the input budget are split on file and hunk boundaries
// and the verdicts are merged, a violation in any chunk winning. Requests
// are retried by the SDK on 429/5xx (honouring Retry-After); credential
// and model errors are reported as ruleeval.ErrUnavailable.
package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// Defaults applied by New when the corresponding Config field is zero.
const (
	DefaultModel              = "claude-sonnet-5-5"
	DefaultEffort             = "medium"
	DefaultMaxTokens          = 8192
	DefaultMaxInputTokens     = 60000
	DefaultMaxRulesPerRequest = 40
	DefaultConcurrency        = 3
	DefaultMaxRetries         = 4

	// EnvAPIKey is the environment variable consulted when Config.APIKey is
	// empty.
	EnvAPIKey = "ANTHROPIC_API_KEY"

	// requestOverheadTokens reserves room for message framing and the
	// response schema when computing the diff budget.
	requestOverheadTokens = 256
)

// Sentinel errors. All of them are wrapped with context; test with errors.Is.
var (
	// ErrRefused is returned when the model declines to evaluate
	// (stop_reason "refusal").
	ErrRefused = errors.New("claude: model refused to evaluate the request")
	// ErrTruncated is returned when the response hit MaxTokens before the
	// structured output was complete.
	ErrTruncated = errors.New("claude: response truncated by max_tokens")
	// ErrMalformed is returned when the response is not the expected JSON.
	ErrMalformed = errors.New("claude: malformed structured response")
	// ErrContextExceeded is returned when the request did not fit the
	// model's context window despite chunking.
	ErrContextExceeded = errors.New("claude: request exceeded the model context window")
)

var validEfforts = map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}

// Config configures the provider. Zero fields take the Default* values.
type Config struct {
	APIKey             string // defaults to $ANTHROPIC_API_KEY
	Model              string
	BaseURL            string // custom endpoint; empty for the Anthropic API
	Effort             string // low | medium | high | xhigh | max
	MaxTokens          int64  // response token cap
	MaxInputTokens     int    // budget per request; drives diff chunking
	MaxRulesPerRequest int
	Concurrency        int // concurrent requests within one batch
	MaxRetries         int // SDK retries for 429/5xx

	// HTTPClient overrides the transport (tests use a record/replay one).
	HTTPClient *http.Client
}

func (c Config) withDefaults() Config {
	if c.APIKey == "" {
		c.APIKey = os.Getenv(EnvAPIKey)
	}
	if c.Model == "" {
		c.Model = DefaultModel
	}
	if c.Effort == "" {
		c.Effort = DefaultEffort
	}
	if c.MaxTokens <= 0 {
		c.MaxTokens = DefaultMaxTokens
	}
	if c.MaxInputTokens <= 0 {
		c.MaxInputTokens = DefaultMaxInputTokens
	}
	if c.MaxRulesPerRequest <= 0 {
		c.MaxRulesPerRequest = DefaultMaxRulesPerRequest
	}
	if c.Concurrency <= 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.MaxRetries <= 0 {
		c.MaxRetries = DefaultMaxRetries
	}
	return c
}

// Provider evaluates rules with Claude. It is safe for concurrent use.
type Provider struct {
	cfg    Config
	client anthropic.Client

	mu    sync.Mutex
	usage ruleeval.Usage
}

// Compile-time capability checks.
var (
	_ ruleeval.Provider      = (*Provider)(nil)
	_ ruleeval.BatchProvider = (*Provider)(nil)
	_ ruleeval.UsageReporter = (*Provider)(nil)
	_ ruleeval.Describer     = (*Provider)(nil)
)

// New creates a Provider. It fails with ruleeval.ErrUnavailable when no API
// key is configured and with a plain error for an invalid effort level.
func New(cfg Config) (*Provider, error) {
	cfg = cfg.withDefaults()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("%w: no API key (set %s or evaluator.api_key)", ruleeval.ErrUnavailable, EnvAPIKey)
	}
	if !validEfforts[cfg.Effort] {
		return nil, fmt.Errorf("claude: invalid effort %q (want low, medium, high, xhigh or max)", cfg.Effort)
	}

	opts := []option.RequestOption{
		option.WithAPIKey(cfg.APIKey),
		option.WithMaxRetries(cfg.MaxRetries),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	if cfg.HTTPClient != nil {
		opts = append(opts, option.WithHTTPClient(cfg.HTTPClient))
	}
	return &Provider{cfg: cfg, client: anthropic.NewClient(opts...)}, nil
}

// Model returns the configured model id.
func (p *Provider) Model() string { return p.cfg.Model }

// PromptVersion returns the prompt revision qualified by the effort level,
// e.g. "v1/effort=medium". Effort changes how carefully the model judges
// the same prompt, so it is part of the cache identity: raising effort
// must not reuse a verdict produced at a lower one.
func (p *Provider) PromptVersion() string { return prompt.Version + "/effort=" + p.cfg.Effort }

// Usage returns the tokens and completed requests accumulated so far.
// SDK-internal retries are not counted as requests.
func (p *Provider) Usage() ruleeval.Usage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usage
}

// Evaluate evaluates a single rule by delegating to EvaluateBatch. The
// entity, source and scope metadata travel with the rule so that a
// local-scope rule is restricted exactly as in the batch path.
func (p *Provider) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	resp, err := p.EvaluateBatch(ctx, ruleeval.BatchRequest{
		EntityID: req.EntityID,
		Inputs:   []ruleeval.RuleInput{req.Input()},
		Diff:     req.Diff,
	})
	if err != nil {
		return nil, err
	}
	if len(resp.Errors) > 0 {
		return nil, fmt.Errorf("claude: evaluation incomplete: %w", errors.Join(resp.Errors...))
	}
	v, ok := resp.Verdicts[req.Rule.ID]
	if !ok {
		return nil, fmt.Errorf("claude: no verdict returned for rule %s", req.Rule.ID)
	}
	return v, nil
}

// work is one request: one diff chunk against one group of rules.
type work struct {
	chunk int
	group int
}

// EvaluateBatch evaluates all rules in req against req.Diff. Rules are
// split into groups of MaxRulesPerRequest and the diff into chunks that fit
// the input budget; every chunk×group pair is one request. Any request
// failure aborts the batch (the evaluator reports it per rule). Hunks too
// large to send are skipped and reported in BatchResponse.Errors.
func (p *Provider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	out := &ruleeval.BatchResponse{Verdicts: map[string]*ruleeval.EvalResponse{}}
	if len(req.Inputs) == 0 {
		return out, nil
	}

	// Tokens left once the system prompt and framing are paid for are shared
	// between the rules block and the diff. Rules get at most half so that
	// a token-heavy group can never starve the diff of its budget.
	available := p.cfg.MaxInputTokens - estimateTokens(prompt.System()) - requestOverheadTokens
	rulesBudget := available / 2
	groups, tooLarge := groupRules(req.Inputs, p.cfg.MaxRulesPerRequest, rulesBudget)
	for _, in := range tooLarge {
		out.Errors = append(out.Errors, fmt.Errorf(
			"rule %s (~%d tokens with notes) exceeds the rules budget of ~%d tokens and was not evaluated",
			in.Rule.ID, ruleTokens(in), rulesBudget))
	}
	if len(groups) == 0 {
		return out, nil
	}
	rulesBlocks := make([]string, len(groups))
	maxRulesTokens := 0
	for i, g := range groups {
		rulesBlocks[i] = prompt.RulesBlock(g)
		if n := estimateTokens(rulesBlocks[i]); n > maxRulesTokens {
			maxRulesTokens = n
		}
	}
	budgetTokens := available - maxRulesTokens
	chunks, oversize := chunkDiff(req.Diff, budgetTokens*3)
	for _, o := range oversize {
		out.Errors = append(out.Errors, fmt.Errorf(
			"hunk in %s (%d bytes, ~%d tokens) exceeds the input budget of ~%d tokens and was not evaluated",
			o.File, o.Bytes, estimateTokensForBytes(o.Bytes), budgetTokens))
	}
	if len(chunks) == 0 {
		return out, nil
	}

	var items []work
	for c := range chunks {
		for g := range groups {
			items = append(items, work{chunk: c, group: g})
		}
	}
	results := make([]*wireResponse, len(items))
	errs := make([]error, len(items))

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.cfg.Concurrency)
	for i, it := range items {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int, it work) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i], errs[i] = p.call(ctx, chunks[it.chunk], groups[it.group], rulesBlocks[it.group])
			if errs[i] != nil {
				cancel() // no point finishing the batch; the evaluator reports it
			}
		}(i, it)
	}
	wg.Wait()

	if err := firstError(errs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for i, it := range items {
		out.Errors = append(out.Errors, results[i].Errors...)
		requested := map[string]bool{}
		for _, in := range groups[it.group] {
			requested[in.Rule.ID] = true
		}
		seen := map[string]bool{}
		for _, v := range results[i].Verdicts {
			if !requested[v.RuleID] {
				out.Errors = append(out.Errors, fmt.Errorf("provider returned a verdict for unrequested rule %q (ignored)", v.RuleID))
				continue
			}
			seen[v.RuleID] = true
			mergeVerdict(out.Verdicts, v.RuleID, v.toResponse())
		}
		// Every chunk must answer for every rule it was asked about. An
		// omission (an empty verdicts array is schema-valid) means part of
		// the diff was never judged, so report it rather than let another
		// chunk's verdict stand as complete.
		for _, in := range groups[it.group] {
			if !seen[in.Rule.ID] {
				out.Errors = append(out.Errors, fmt.Errorf(
					"provider returned no verdict for rule %s in diff chunk %d of %d", in.Rule.ID, it.chunk+1, len(chunks)))
			}
		}
	}
	return out, nil
}

// firstError picks the error to report for a batch: ErrUnavailable first
// so the caller can stop early, then the first real failure, and only as a
// last resort a cancellation (siblings of a failed request are cancelled
// and would otherwise mask the cause).
func firstError(errs []error) error {
	var first, cancelled error
	for _, err := range errs {
		switch {
		case err == nil:
		case errors.Is(err, ruleeval.ErrUnavailable):
			return err
		case errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded):
			if cancelled == nil {
				cancelled = err
			}
		case first == nil:
			first = err
		}
	}
	if first != nil {
		return first
	}
	return cancelled
}

// ruleTokens estimates the prompt cost of one rule with its notes.
func ruleTokens(in ruleeval.RuleInput) int {
	return estimateTokens(prompt.RulesBlock([]ruleeval.RuleInput{in}))
}

// groupRules packs inputs, in order, into groups of at most maxCount rules
// whose estimated prompt size stays within tokenBudget. A single rule that
// exceeds the budget on its own cannot be sent and is returned in tooLarge.
func groupRules(inputs []ruleeval.RuleInput, maxCount, tokenBudget int) (groups [][]ruleeval.RuleInput, tooLarge []ruleeval.RuleInput) {
	var cur []ruleeval.RuleInput
	curTokens := 0
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
			cur, curTokens = nil, 0
		}
	}
	for _, in := range inputs {
		n := ruleTokens(in)
		if n > tokenBudget {
			tooLarge = append(tooLarge, in)
			continue
		}
		if len(cur) >= maxCount || curTokens+n > tokenBudget {
			flush()
		}
		cur = append(cur, in)
		curTokens += n
	}
	flush()
	return groups, tooLarge
}

// wireResponse is the merged result of one chunk×group request: a verdict
// per rule, with details for the violated ones. Errors lists violated rules
// whose detail request failed recoverably; their verdict is kept.
type wireResponse struct {
	Verdicts []wireVerdict
	Errors   []error
}

type wireVerdict struct {
	RuleID      string
	Verdict     string
	Explanation string
	Confidence  float64
	Evidence    []wireLocation
}

// wireDetail is the structured detail response (see prompt.DetailSchema).
type wireDetail struct {
	Explanation string         `json:"explanation"`
	Confidence  float64        `json:"confidence"`
	Evidence    []wireLocation `json:"evidence"`
}

type wireLocation struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Excerpt string `json:"excerpt"`
}

func (v wireVerdict) toResponse() *ruleeval.EvalResponse {
	r := &ruleeval.EvalResponse{
		Verdict:     ruleeval.Verdict(v.Verdict),
		Explanation: v.Explanation,
		Confidence:  v.Confidence,
	}
	for _, e := range v.Evidence {
		r.Evidence = append(r.Evidence, ruleeval.Location{File: e.File, Line: e.Line, Excerpt: e.Excerpt})
	}
	return r
}

// call evaluates one diff chunk against one group of rules: a verdict
// request keyed by rule id, then one detail request per violated rule.
// Ids the model was not asked about are kept so that EvaluateBatch can
// report them; they never trigger a detail request.
func (p *Provider) call(ctx context.Context, chunk string, group []ruleeval.RuleInput, rulesBlock string) (*wireResponse, error) {
	ids := make([]string, len(group))
	byID := make(map[string]ruleeval.RuleInput, len(group))
	for i, in := range group {
		ids[i] = in.Rule.ID
		byID[in.Rule.ID] = in
	}
	text, err := p.send(ctx, chunk, []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(rulesBlock),
	}, prompt.VerdictSchema(ids))
	if err != nil {
		return nil, err
	}
	var verdicts map[string]string
	if err := json.Unmarshal([]byte(text), &verdicts); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	// Structured outputs should guarantee the enum, but a proxy or schema
	// drift must not turn an unknown value into a silently ignored verdict.
	for id, v := range verdicts {
		switch ruleeval.Verdict(v) {
		case ruleeval.Compliant, ruleeval.Violated, ruleeval.NotApplicable:
		default:
			return nil, fmt.Errorf("%w: rule %s has unknown verdict %q", ErrMalformed, id, v)
		}
	}
	out := &wireResponse{}
	seen := map[string]bool{}
	for _, id := range ids { // requested ids first, in order
		v, ok := verdicts[id]
		if !ok {
			continue
		}
		seen[id] = true
		wv := wireVerdict{RuleID: id, Verdict: v}
		if ruleeval.Verdict(v) == ruleeval.Violated {
			d, err := p.detail(ctx, chunk, byID[id])
			switch {
			case err == nil:
				wv.Explanation, wv.Confidence, wv.Evidence = d.Explanation, d.Confidence, d.Evidence
			case errors.Is(err, ruleeval.ErrUnavailable) || ctx.Err() != nil:
				return nil, err
			default:
				// The violation stands even without its explanation: dropping
				// it would let an Implemented-rule violation pass as a
				// warning. The error keeps the batch out of the cache.
				out.Errors = append(out.Errors, fmt.Errorf("rule %s was judged violated but its details could not be retrieved: %w", id, err))
			}
		}
		out.Verdicts = append(out.Verdicts, wv)
	}
	for id, v := range verdicts { // then anything unrequested, for reporting
		if !seen[id] {
			out.Verdicts = append(out.Verdicts, wireVerdict{RuleID: id, Verdict: v})
		}
	}
	return out, nil
}

// detail asks for the explanation, confidence and evidence of a rule the
// verdict request judged violated. The diff block is byte-identical to the
// verdict request's so the prompt cache prefix is reused.
func (p *Provider) detail(ctx context.Context, chunk string, in ruleeval.RuleInput) (*wireDetail, error) {
	text, err := p.send(ctx, chunk, []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(prompt.RulesBlock([]ruleeval.RuleInput{in})),
		anthropic.NewTextBlock(prompt.DetailInstruction(in.Rule.ID)),
	}, prompt.DetailSchema())
	if err != nil {
		return nil, err
	}
	var d wireDetail
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	return &d, nil
}

// send performs one Messages request (system prompt, cached diff block, then
// blocks) with the structured-output schema and returns the response text.
// Usage is accumulated for every completed response.
func (p *Provider) send(ctx context.Context, chunk string, blocks []anthropic.ContentBlockParamUnion, schema map[string]any) (string, error) {
	content := append([]anthropic.ContentBlockParamUnion{{OfText: &anthropic.TextBlockParam{
		Text:         prompt.DiffBlock(chunk),
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}}}, blocks...)
	params := anthropic.MessageNewParams{
		Model:     p.cfg.Model,
		MaxTokens: p.cfg.MaxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         prompt.System(),
			CacheControl: anthropic.NewCacheControlEphemeralParam(),
		}},
		Messages: []anthropic.MessageParam{anthropic.NewUserMessage(content...)},
		OutputConfig: anthropic.OutputConfigParam{
			Effort: anthropic.OutputConfigEffort(p.cfg.Effort),
			Format: anthropic.JSONOutputFormatParam{Schema: schema},
		},
	}

	msg, err := p.client.Messages.New(ctx, params)
	if err != nil {
		return "", classify(err)
	}

	p.mu.Lock()
	p.usage.Add(ruleeval.Usage{
		InputTokens:      msg.Usage.InputTokens,
		OutputTokens:     msg.Usage.OutputTokens,
		CacheReadTokens:  msg.Usage.CacheReadInputTokens,
		CacheWriteTokens: msg.Usage.CacheCreationInputTokens,
		Requests:         1,
	})
	p.mu.Unlock()

	switch msg.StopReason {
	case anthropic.StopReasonRefusal:
		return "", fmt.Errorf("%w (category %q: %s)", ErrRefused, msg.StopDetails.Category, msg.StopDetails.Explanation)
	case anthropic.StopReasonMaxTokens:
		return "", fmt.Errorf("%w (max_tokens=%d)", ErrTruncated, p.cfg.MaxTokens)
	case anthropic.StopReasonModelContextWindowExceeded:
		return "", ErrContextExceeded
	}

	for _, block := range msg.Content {
		if tb, ok := block.AsAny().(anthropic.TextBlock); ok && tb.Text != "" {
			return tb.Text, nil
		}
	}
	return "", fmt.Errorf("%w: no text block in response (stop_reason %q)", ErrMalformed, msg.StopReason)
}

// classify maps SDK errors: credential, permission and model errors become
// ruleeval.ErrUnavailable; everything else (including exhausted retries)
// is returned with context.
func classify(err error) error {
	var apierr *anthropic.Error
	if errors.As(err, &apierr) {
		switch apierr.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
			return fmt.Errorf("%w: HTTP %d %s", ruleeval.ErrUnavailable, apierr.StatusCode, apierr.Type())
		case http.StatusBadRequest:
			// An organisation-level key without a workspace, or a key the
			// endpoint does not accept, comes back as 400 invalid_request;
			// no request will ever succeed, so report the provider as
			// unavailable instead of failing every rule one by one.
			if msg := strings.ToLower(err.Error()); strings.Contains(msg, "workspace") || strings.Contains(msg, "api key") {
				return fmt.Errorf("%w: HTTP %d %s: %v", ruleeval.ErrUnavailable, apierr.StatusCode, apierr.Type(), err)
			}
			return fmt.Errorf("claude: HTTP %d %s: %w", apierr.StatusCode, apierr.Type(), err)
		default:
			return fmt.Errorf("claude: HTTP %d %s: %w", apierr.StatusCode, apierr.Type(), err)
		}
	}
	return fmt.Errorf("claude: request failed: %w", err)
}
