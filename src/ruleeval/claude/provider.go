// Package claude implements ruleeval.Provider and ruleeval.BatchProvider on
// top of the Anthropic Messages API using structured outputs. Each request
// asks for the verdicts of a group of rules as an object keyed by rule id;
// a second, per-rule request then collects explanation and evidence for
// every rule judged violated, and with ruleeval.DetailCompliant for every
// rule judged compliant.
//
// One request carries the shared system prompt and the diff (both marked
// as prompt-cache breakpoints) followed by up to MaxRulesPerRequest rules.
// Diffs larger than the input budget are split on file and hunk boundaries
// and the verdicts are merged, a violation in any chunk winning. Each
// request that shows only part of a diff also lists the files of the whole
// diff, so that a rule spanning files is not judged on half of them.
//
// A request whose Files are set audits the code as it stands instead
// (ruleeval.Auditor), with numbered lines. A file set that fits one request
// is judged like a diff. A larger one is audited in parts, a map-reduce:
// each part only collects the findings its files hold for each rule, and
// one request per group of rules then judges every rule on the findings of
// all parts and the paths of the whole set, so that something a rule
// requires is reported missing when no part found it. That judgement gives
// the explanation and evidence of every verdict at once, whatever the
// detail level. With a ruleeval.BlobStore in the request, what each file
// holds for each rule is kept, and each judgement on what it was judged
// on, so that a rerun reads only the files that changed and judges only
// the rules whose findings did.
//
// Requests are retried by the SDK on 429/5xx (honouring Retry-After);
// credential and model errors are reported as ruleeval.ErrUnavailable.
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
	// DefaultModel is the Claude model called when Config.Model is empty.
	DefaultModel = "claude-haiku-5-5"
	// DefaultEffort is the reasoning effort used when Config.Effort is empty.
	DefaultEffort = "medium"
	// DefaultMaxTokens caps the response size when Config.MaxTokens is zero.
	DefaultMaxTokens = 8192
	// DefaultMaxInputTokens is the per-request input budget, which drives diff
	// chunking, when Config.MaxInputTokens is zero. It was 60000 while
	// EstimateTokens counted 3 bytes per token, so a part held about 180 KB of
	// diff and real requests on Go code reached about 84k tokens. At 2 bytes
	// per token, 100000 keeps parts about that size (some 190 KB), so a diff
	// is not split more than before.
	DefaultMaxInputTokens = 100000
	// DefaultMaxRulesPerRequest bounds how many rules share one request when
	// Config.MaxRulesPerRequest is zero.
	DefaultMaxRulesPerRequest = 40
	// DefaultConcurrency is the number of concurrent requests within one batch
	// when Config.Concurrency is zero.
	DefaultConcurrency = 3
	// DefaultMaxRetries is the number of SDK retries on 429 and 5xx responses
	// when Config.MaxRetries is zero.
	DefaultMaxRetries = 4

	// EnvAPIKey is the environment variable consulted when Config.APIKey is
	// empty.
	EnvAPIKey = "ANTHROPIC_API_KEY"

	// requestOverheadTokens reserves room for message framing and the
	// response schema when computing the diff budget.
	requestOverheadTokens = 256
	// fileListShare is the fraction of the diff budget, as a divisor, that the
	// list of changed files may take from a partial diff.
	fileListShare = 4
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
	// APIKey defaults to $ANTHROPIC_API_KEY.
	APIKey string
	// Model is the Claude model ID to call.
	Model string
	// BaseURL is a custom endpoint; empty for the Anthropic API.
	BaseURL string
	// Effort is one of low | medium | high | xhigh | max.
	Effort string
	// MaxTokens is the response token cap.
	MaxTokens int64
	// MaxInputTokens is the budget per request; it drives diff chunking.
	MaxInputTokens int
	// MaxRulesPerRequest bounds how many rules of one entity share a single
	// request.
	MaxRulesPerRequest int
	// Concurrency is the number of concurrent requests within one batch.
	Concurrency int
	// MaxRetries is the number of SDK retries for 429/5xx.
	MaxRetries int

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

// PromptVersion returns the prompt revision qualified by the effort level
// and the split configuration, e.g. "v8/effort=medium/split=100000,40,2/1".
// Effort changes how carefully the model judges the same prompt, so it is
// part of the cache identity: raising effort must not reuse a verdict
// produced at a lower one. The split configuration (MaxInputTokens,
// MaxRulesPerRequest and the bytes-per-token ratio) decides whether a diff
// is split, and a part cannot report what may sit in another, so a verdict
// judged in parts must not be reused where the diff would fit one request.
func (p *Provider) PromptVersion() string {
	return prompt.Version + "/effort=" + p.cfg.Effort + p.splitIdentity()
}

// AuditPromptVersion returns the audit prompt revision with the prompt
// revision it shares the rules block and verdict schema with, qualified as
// PromptVersion is, e.g. "audit-a1+v8/effort=medium/split=100000,40,2/1".
// The split configuration matters even more to an audit, where something
// missing from a set that fits one request is a violation that a part may
// not report. The provider audits every request whose Files are set
// (ruleeval.Auditor).
func (p *Provider) AuditPromptVersion() string {
	return "audit-" + prompt.AuditVersion + "+" + prompt.Version + "/effort=" + p.cfg.Effort + p.splitIdentity()
}

// splitIdentity renders the configuration that decides how a request is
// split into parts.
func (p *Provider) splitIdentity() string {
	return fmt.Sprintf("/split=%d,%d,%d/%d", p.cfg.MaxInputTokens, p.cfg.MaxRulesPerRequest, bytesPerTokenNum, bytesPerTokenDen)
}

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
		Files:    req.Files,
		Detail:   req.Detail,
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

// work is one request: one diff chunk, or part of a file set, against one
// group of rules.
type work struct {
	chunk int
	group int
}

// EvaluateBatch evaluates all rules in req against req.Diff, or when
// req.Files is set audits them against those files. Rules are split into
// groups of MaxRulesPerRequest and the diff or file set into chunks that fit
// the input budget; every chunk×group pair is one request. Any request
// failure aborts the batch (the evaluator reports it per rule). Hunks or
// lines too large to send are skipped and reported in BatchResponse.Errors.
// Whenever a request cannot show the whole diff, it lists its files. A file
// set too large for one request is audited in parts (auditInParts).
func (p *Provider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	out := &ruleeval.BatchResponse{Verdicts: map[string]*ruleeval.EvalResponse{}}
	if len(req.Inputs) == 0 {
		return out, nil
	}

	m := modeFor(req)
	// The verdict schema is a few tokens per rule, left to
	// requestOverheadTokens.
	rp := p.planRules(m.system, nil, req.Inputs)
	var (
		chunks   []string
		block    func(string) string
		planErrs []error
	)
	if m.audit {
		var unread []prompt.Unread
		chunks, block, unread, planErrs = planAudit(req.Files, rp.budget)
		if len(chunks) != 1 || len(unread) > 0 {
			// More than one part, none when every file is too large for a
			// request under the audit prompt, or one that leaves something
			// out: auditInParts plans the parts again under its own, shorter
			// prompt, tells the judgement what no part shows and reports
			// it. A single request thus always shows every file whole.
			return p.auditInParts(ctx, req, out)
		}
	} else {
		chunks, block, planErrs = planDiff(req.Diff, rp.budget)
	}
	out.Errors = append(out.Errors, rp.errs...)
	if len(rp.groups) == 0 {
		return out, nil
	}
	out.Errors = append(out.Errors, planErrs...)
	if len(chunks) == 0 {
		return out, nil
	}
	groups := rp.groups

	var items []work
	for c := range chunks {
		for g := range groups {
			items = append(items, work{chunk: c, group: g})
		}
	}
	results := make([]*wireResponse, len(items))
	if err := p.runAll(ctx, len(items), func(ctx context.Context, i int) error {
		var err error
		it := items[i]
		results[i], err = p.call(ctx, m, block(chunks[it.chunk]), groups[it.group], rp.blocks[it.group], req.Detail)
		return err
	}); err != nil {
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
					"provider returned no verdict for rule %s in %s %d of %d", in.Rule.ID, m.part, it.chunk+1, len(chunks)))
			}
		}
	}
	return out, nil
}

// runAll calls fn for every index below n, at most Concurrency at a time.
// The first failure cancels the calls still running and stops scheduling
// new ones; the error returned is picked by firstError, or is the context's
// own when it was cancelled from outside.
func (p *Provider) runAll(ctx context.Context, n int, fn func(ctx context.Context, i int) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errs := make([]error, n)
	var wg sync.WaitGroup
	sem := make(chan struct{}, p.cfg.Concurrency)
	for i := range n {
		if ctx.Err() != nil {
			break
		}
		sem <- struct{}{}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			if errs[i] = fn(ctx, i); errs[i] != nil {
				cancel() // no point finishing the batch; the evaluator reports it
			}
		}(i)
	}
	wg.Wait()
	if err := firstError(errs); err != nil {
		return err
	}
	return ctx.Err()
}

// rulePlan is how the rules of a batch are sent: in groups, each rendered
// as its rules block, leaving budget tokens per request for the diff or
// files. errs reports the rules too large to send at all.
type rulePlan struct {
	groups [][]ruleeval.RuleInput
	blocks []string
	budget int
	errs   []error
}

// planRules groups inputs for requests under the given system prompt and
// response schema. Tokens left once the system prompt and framing are paid
// for are shared between the rules, with their part of the schema, and the
// diff or files. Rules get at most half, so that a token-heavy group can
// never starve the code of its budget. The API turns the schema into a
// prompt of its own, which grows with every rule; a nil schema is left to
// requestOverheadTokens, as the verdict schema of a diff is.
func (p *Provider) planRules(system string, schema func(ids []string) map[string]any, inputs []ruleeval.RuleInput) rulePlan {
	available := p.cfg.MaxInputTokens - estimateTokens(system) - requestOverheadTokens
	rulesBudget := available / 2
	schemaCost := func(ids []string) int {
		if schema == nil {
			return 0
		}
		return schemaTokens(schema(ids))
	}
	cost := func(in ruleeval.RuleInput) int {
		return ruleTokens(in) + schemaCost([]string{in.Rule.ID})
	}
	groups, tooLarge := groupRules(inputs, p.cfg.MaxRulesPerRequest, rulesBudget, cost)
	rp := rulePlan{groups: groups, blocks: make([]string, len(groups))}
	for _, in := range tooLarge {
		rp.errs = append(rp.errs, fmt.Errorf(
			"rule %s (~%d tokens with notes) exceeds the rules budget of ~%d tokens and was not evaluated",
			in.Rule.ID, cost(in), rulesBudget))
	}
	maxRulesTokens := 0
	for i, g := range groups {
		rp.blocks[i] = prompt.RulesBlock(g)
		if n := estimateTokens(rp.blocks[i]) + schemaCost(ruleIDs(g)); n > maxRulesTokens {
			maxRulesTokens = n
		}
	}
	rp.budget = available - maxRulesTokens
	return rp
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

// schemaTokens estimates the prompt cost of a response schema, sent as
// JSON.
func schemaTokens(schema map[string]any) int {
	b, err := json.Marshal(schema)
	if err != nil {
		// The schemas are plain maps of strings, slices and maps, which
		// always marshal.
		return 0
	}
	return estimateTokens(string(b))
}

// groupRules packs inputs, in order, into groups of at most maxCount rules
// whose estimated prompt size, cost per rule, stays within tokenBudget. A
// single rule that exceeds the budget on its own cannot be sent and is
// returned in tooLarge.
func groupRules(inputs []ruleeval.RuleInput, maxCount, tokenBudget int, cost func(ruleeval.RuleInput) int) (groups [][]ruleeval.RuleInput, tooLarge []ruleeval.RuleInput) {
	var cur []ruleeval.RuleInput
	curTokens := 0
	flush := func() {
		if len(cur) > 0 {
			groups = append(groups, cur)
			cur, curTokens = nil, 0
		}
	}
	for _, in := range inputs {
		n := cost(in)
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
// per rule, with details for the detailed ones. Errors lists detailed rules
// whose detail request failed recoverably; their verdict is kept.
type wireResponse struct {
	// Verdicts holds one entry per evaluated rule.
	Verdicts []wireVerdict
	// Errors lists recoverable detail-request failures.
	Errors []error
}

type wireVerdict struct {
	// RuleID is the rule the verdict is for.
	RuleID string
	// Verdict is the raw verdict string returned by the model.
	Verdict string
	// Explanation is the model's reasoning, filled by the detail step.
	Explanation string
	// Confidence is the model's certainty from 0 to 1, filled by the detail
	// step.
	Confidence float64
	// Evidence lists the diff locations cited by the detail step.
	Evidence []wireLocation
}

// wireDetail is the structured detail response (see prompt.DetailSchema).
type wireDetail struct {
	// Verdict is the verdict the model holds after re-reading the diff.
	Verdict string `json:"verdict"`
	// Explanation is the model's reasoning for the verdict.
	Explanation string `json:"explanation"`
	// Confidence is the model's certainty from 0 (guess) to 1 (certain).
	Confidence float64 `json:"confidence"`
	// Evidence lists the diff locations supporting the verdict.
	Evidence []wireLocation `json:"evidence"`
}

// empty reports whether d says neither why nor how surely: a blank
// explanation or a confidence of 0 or less. The schema requires both
// fields but cannot require them filled, and such a detail cannot be
// checked by anyone, so it is not taken for a judgement
// (strspc-manager#338).
func (d wireDetail) empty() bool {
	return strings.TrimSpace(d.Explanation) == "" || d.Confidence <= 0
}

type wireLocation struct {
	// File is the file path from the diff header.
	File string `json:"file"`
	// Line is the line number in the new version of the file.
	Line int `json:"line"`
	// Excerpt is a short quote of the line cited.
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

// call evaluates one rendered diff or files block against one group of rules: a
// verdict request keyed by rule id, then one detail request per rule whose
// verdict level covers. Ids the model was not asked about are kept so that
// EvaluateBatch can report them; they never trigger a detail request.
func (p *Provider) call(ctx context.Context, m mode, diffBlock string, group []ruleeval.RuleInput, rulesBlock string, level ruleeval.Detail) (*wireResponse, error) {
	ids := make([]string, len(group))
	byID := make(map[string]ruleeval.RuleInput, len(group))
	for i, in := range group {
		ids[i] = in.Rule.ID
		byID[in.Rule.ID] = in
	}
	text, err := p.send(ctx, m.system, diffBlock, []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(rulesBlock),
	}, prompt.VerdictSchema(ids))
	if err != nil {
		return nil, err
	}
	var verdicts map[string]string
	if err := json.Unmarshal([]byte(text), &verdicts); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
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
		if given := ruleeval.Verdict(v); level.Covers(given) {
			d, err := p.detail(ctx, m, diffBlock, byID[id], given)
			switch {
			case err == nil:
				wv.Explanation, wv.Confidence, wv.Evidence = d.Explanation, d.Confidence, d.Evidence
				// The detail step re-reads the diff with the single rule in
				// focus and may withdraw the verdict (strspc-manager#87). A
				// verdict withdrawn to compliant or not_applicable keeps its
				// explanation so the caller can see why, but no evidence: the
				// lines cited for a withdrawn violation broke nothing. A
				// compliant verdict withdrawn to violated keeps the lines that
				// break the rule.
				if w := ruleeval.Verdict(d.Verdict); w != given {
					wv.Verdict = string(w)
					if w != ruleeval.Violated {
						wv.Evidence = nil
					}
					p.mu.Lock()
					p.usage.Withdrawn++
					p.mu.Unlock()
				}
			case errors.Is(err, ruleeval.ErrUnavailable) || ctx.Err() != nil:
				return nil, err
			default:
				// The verdict stands even without its explanation: dropping
				// a violation would let an Implemented-rule violation pass as
				// a warning. The error keeps the batch out of the cache, so a
				// rerun asks again for the reasons a caller asked for.
				out.Errors = append(out.Errors, fmt.Errorf("rule %s was judged %s but its details could not be retrieved: %w", id, given, err))
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
// verdict request judged given. The diff block is byte-identical to the
// verdict request's so the prompt cache prefix is reused.
func (p *Provider) detail(ctx context.Context, m mode, diffBlock string, in ruleeval.RuleInput, given ruleeval.Verdict) (*wireDetail, error) {
	text, err := p.send(ctx, m.system, diffBlock, []anthropic.ContentBlockParamUnion{
		anthropic.NewTextBlock(prompt.RulesBlock([]ruleeval.RuleInput{in})),
		anthropic.NewTextBlock(m.instruction(in.Rule.ID, given)),
	}, m.detailSchema())
	if err != nil {
		return nil, err
	}
	var d wireDetail
	if err := json.Unmarshal([]byte(text), &d); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	// The verdict is what lets the detail step withdraw a verdict, so a
	// response without one (an older detail shape from a proxy, say) must
	// not pass as a confirmation.
	switch ruleeval.Verdict(d.Verdict) {
	case ruleeval.Violated, ruleeval.Compliant, ruleeval.NotApplicable:
	default:
		return nil, fmt.Errorf("%w: detail for rule %s has unknown verdict %q", ErrMalformed, in.Rule.ID, d.Verdict)
	}
	// An empty detail must not confirm a verdict, nor withdraw one.
	if d.empty() {
		return nil, fmt.Errorf("%w: detail for rule %s has no explanation or no confidence", ErrMalformed, in.Rule.ID)
	}
	return &d, nil
}

// send performs one Messages request (system prompt, cached diff or files
// block, then blocks) with the structured-output schema and returns the
// response text.
// Usage is accumulated for every completed response.
func (p *Provider) send(ctx context.Context, system, diffBlock string, blocks []anthropic.ContentBlockParamUnion, schema map[string]any) (string, error) {
	content := append([]anthropic.ContentBlockParamUnion{{OfText: &anthropic.TextBlockParam{
		Text:         diffBlock,
		CacheControl: anthropic.NewCacheControlEphemeralParam(),
	}}}, blocks...)
	params := anthropic.MessageNewParams{
		Model:     p.cfg.Model,
		MaxTokens: p.cfg.MaxTokens,
		System: []anthropic.TextBlockParam{{
			Text:         system,
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
			return fmt.Errorf("%w: HTTP %d %s: %w", ruleeval.ErrUnavailable, apierr.StatusCode, apierr.Type(), err)
		case http.StatusBadRequest:
			// An organisation-level key without a workspace, or a key the
			// endpoint does not accept, comes back as 400 invalid_request;
			// no request will ever succeed, so report the provider as
			// unavailable instead of failing every rule one by one.
			if msg := strings.ToLower(err.Error()); strings.Contains(msg, "workspace") || strings.Contains(msg, "api key") {
				return fmt.Errorf("%w: HTTP %d %s: %w", ruleeval.ErrUnavailable, apierr.StatusCode, apierr.Type(), err)
			}
			return fmt.Errorf("claude: HTTP %d %s: %w", apierr.StatusCode, apierr.Type(), err)
		default:
			return fmt.Errorf("claude: HTTP %d %s: %w", apierr.StatusCode, apierr.Type(), err)
		}
	}
	return fmt.Errorf("claude: request failed: %w", err)
}
