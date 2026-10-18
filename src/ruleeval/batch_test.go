package ruleeval_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// batchProvider implements both Provider and BatchProvider and records
// every batch request so tests can assert grouping behaviour.
type batchProvider struct {
	mu        sync.Mutex
	responses map[string]*ruleeval.EvalResponse
	errs      []error          // Errors returned in every BatchResponse
	failFor   map[string]error // entity id → fatal batch error
	batches   []ruleeval.BatchRequest
	single    []ruleeval.EvalRequest
	usage     ruleeval.Usage
}

func (b *batchProvider) Evaluate(_ context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.single = append(b.single, req)
	return b.responses[req.Rule.ID], nil
}

func (b *batchProvider) EvaluateBatch(_ context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.batches = append(b.batches, req)
	if err, ok := b.failFor[req.EntityID]; ok {
		return nil, err
	}
	out := &ruleeval.BatchResponse{Verdicts: map[string]*ruleeval.EvalResponse{}, Errors: b.errs}
	for _, in := range req.Inputs {
		if r, ok := b.responses[in.Rule.ID]; ok {
			out.Verdicts[in.Rule.ID] = r
		}
	}
	return out, nil
}

func (b *batchProvider) Usage() ruleeval.Usage { return b.usage }

func (b *batchProvider) Model() string         { return "test-model" }
func (b *batchProvider) PromptVersion() string { return "vtest" }

// memCache is an in-memory ruleeval.Cache.
type memCache struct {
	mu      sync.Mutex
	entries map[string]*ruleeval.EvalResponse
	getErr  error
	puts    int
}

func newMemCache() *memCache { return &memCache{entries: map[string]*ruleeval.EvalResponse{}} }

func (c *memCache) Get(key string) (*ruleeval.EvalResponse, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.getErr != nil {
		return nil, false, c.getErr
	}
	r, ok := c.entries[key]
	return r, ok, nil
}

func (c *memCache) Put(key string, resp *ruleeval.EvalResponse) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = resp
	c.puts++
	return nil
}

type recordingObserver struct {
	mu     sync.Mutex
	events []ruleeval.Event
}

func (o *recordingObserver) Observe(ev ruleeval.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.events = append(o.events, ev)
}

func (o *recordingObserver) count(kind ruleeval.EventKind) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := 0
	for _, ev := range o.events {
		if ev.Kind == kind {
			n++
		}
	}
	return n
}

func entityInput(entityID, ruleID, state string) ruleeval.RuleInput {
	in := makeInput(ruleID, state)
	in.EntityID = entityID
	return in
}

func TestEvaluate_EmptyDiff_NoProviderCall(t *testing.T) {
	bp := &batchProvider{}
	e, err := ruleeval.New(bp)
	if err != nil {
		t.Fatal(err)
	}
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{makeInput("ENT-001", "I")}, "  \n\t")
	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeNoDiff || d.Severity != result.Info {
		t.Errorf("expected %s Info, got %s %s", ruleeval.CodeNoDiff, d.Code, d.Severity)
	}
	if len(bp.batches)+len(bp.single) != 0 {
		t.Errorf("expected no provider calls, got %d batch / %d single", len(bp.batches), len(bp.single))
	}
}

func TestEvaluate_GroupsByEntity_UsesBatch(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"A-001": {Verdict: ruleeval.Compliant},
		"A-002": {Verdict: ruleeval.Violated, Explanation: "bad", Evidence: []ruleeval.Location{{File: "src/x.go", Line: 12, Excerpt: "x := nil"}}},
		"B-001": {Verdict: ruleeval.NotApplicable},
	}}
	e, err := ruleeval.New(bp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{
		entityInput("A", "A-001", "I"),
		entityInput("B", "B-001", "P"),
		entityInput("A", "A-002", "I"),
	}
	res := e.Evaluate(context.Background(), inputs, "diff --git a/src/x.go b/src/x.go\n")

	if len(bp.single) != 0 {
		t.Errorf("expected no per-rule calls, got %d", len(bp.single))
	}
	if len(bp.batches) != 2 {
		t.Fatalf("expected 2 batches (one per entity), got %d", len(bp.batches))
	}
	byEntity := map[string]int{}
	for _, b := range bp.batches {
		byEntity[b.EntityID] = len(b.Inputs)
		if b.Diff == "" {
			t.Error("batch request missing diff")
		}
	}
	if byEntity["A"] != 2 || byEntity["B"] != 1 {
		t.Errorf("unexpected grouping: %v", byEntity)
	}

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeViolated || d.Severity != result.Error {
		t.Errorf("expected RE001 Error, got %s %s", d.Code, d.Severity)
	}
	if d.Path != "src/x.go" || d.Line != 12 {
		t.Errorf("expected evidence location src/x.go:12, got %s:%d", d.Path, d.Line)
	}
}

func TestEvaluate_BatchError_WarnsPerRuleAndContinues(t *testing.T) {
	bp := &batchProvider{
		responses: map[string]*ruleeval.EvalResponse{"B-001": {Verdict: ruleeval.Violated, Explanation: "x"}},
		failFor:   map[string]error{"A": errors.New("boom")},
	}
	e, err := ruleeval.New(bp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{
		entityInput("A", "A-001", "I"),
		entityInput("A", "A-002", "I"),
		entityInput("B", "B-001", "I"),
	}
	res := e.Evaluate(context.Background(), inputs, "diff")
	if len(res.Diagnostics) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	for _, d := range res.Diagnostics[:2] {
		if d.Code != ruleeval.CodeProviderError || d.Severity != result.Warning {
			t.Errorf("expected RE002 Warning, got %s %s", d.Code, d.Severity)
		}
		if !strings.Contains(d.Message, "boom") {
			t.Errorf("expected error text in message, got %q", d.Message)
		}
	}
	if res.Diagnostics[2].Code != ruleeval.CodeViolated {
		t.Errorf("expected B-001 violation still reported, got %v", res.Diagnostics[2])
	}
}

func TestEvaluate_BatchMissingVerdict_IsProviderError(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{}}
	e, err := ruleeval.New(bp)
	if err != nil {
		t.Fatal(err)
	}
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{entityInput("A", "A-001", "I")}, "diff")
	if len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != ruleeval.CodeProviderError {
		t.Fatalf("expected one RE002 for missing verdict, got %v", res.Diagnostics)
	}
}

func TestEvaluate_BatchPartialErrors_Reported(t *testing.T) {
	bp := &batchProvider{
		responses: map[string]*ruleeval.EvalResponse{"A-001": {Verdict: ruleeval.Compliant}},
		errs:      []error{errors.New("hunk for src/big.go exceeds input budget")},
	}
	cache := newMemCache()
	e, err := ruleeval.New(bp, ruleeval.WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{entityInput("A", "A-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")
	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeProviderError || !strings.Contains(d.Message, "big.go") {
		t.Errorf("expected RE002 mentioning big.go, got %v", d)
	}

	// Verdicts from a partially evaluated batch must not be cached: a rerun
	// has to call the provider again and surface the warning again.
	if cache.puts != 0 {
		t.Errorf("expected no cache writes after a partial batch, got %d", cache.puts)
	}
	res2 := e.Evaluate(context.Background(), inputs, "diff")
	if len(bp.batches) != 2 {
		t.Fatalf("expected the provider to be called again on rerun, got %d calls", len(bp.batches))
	}
	if len(res2.Diagnostics) != 1 || res2.Diagnostics[0].Code != ruleeval.CodeProviderError {
		t.Errorf("rerun must re-emit the RE002 warning, got %v", res2.Diagnostics)
	}
}

func TestEvaluate_EvaluateStates_Default(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"A-001": {Verdict: ruleeval.Violated, Explanation: "draft"},
		"A-002": {Verdict: ruleeval.Violated, Explanation: "published"},
		"A-003": {Verdict: ruleeval.Violated, Explanation: "implemented"},
		"A-004": {Verdict: ruleeval.Violated, Explanation: "abandoned"},
		"A-005": {Verdict: ruleeval.Violated, Explanation: "retired"},
		"A-006": {Verdict: ruleeval.Violated, Explanation: "terminated"},
	}}
	e, err := ruleeval.New(bp)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{
		entityInput("A", "A-001", "D"),
		entityInput("A", "A-002", "P"),
		entityInput("A", "A-003", "I"),
		entityInput("A", "A-004", "A"),
		entityInput("A", "A-005", "R"),
		entityInput("A", "A-006", "T"),
	}
	res := e.Evaluate(context.Background(), inputs, "diff")
	if len(res.Diagnostics) != 3 {
		t.Fatalf("expected 3 diagnostics (D,P,I), got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	if len(bp.batches) != 1 || len(bp.batches[0].Inputs) != 3 {
		t.Fatalf("expected one batch of 3 rules, got %+v", bp.batches)
	}
}

func TestEvaluate_WithEvaluateStates_Custom(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"A-001": {Verdict: ruleeval.Violated},
		"A-003": {Verdict: ruleeval.Violated},
	}}
	e, err := ruleeval.New(bp, ruleeval.WithEvaluateStates([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{entityInput("A", "A-001", "D"), entityInput("A", "A-003", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")
	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %v", res.Diagnostics)
	}
}

func TestNew_InvalidEvaluateState(t *testing.T) {
	if _, err := ruleeval.New(&batchProvider{}, ruleeval.WithEvaluateStates([]string{"Z"})); err == nil {
		t.Fatal("expected error for invalid evaluate state")
	}
}

func TestEvaluate_CacheHitSkipsProvider(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"A-001": {Verdict: ruleeval.Violated, Explanation: "first run", Evidence: []ruleeval.Location{{File: "f.go", Line: 3}}},
	}}
	cache := newMemCache()
	obs := &recordingObserver{}
	e, err := ruleeval.New(bp, ruleeval.WithCache(cache), ruleeval.WithObserver(obs))
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{entityInput("A", "A-001", "I")}

	res1 := e.Evaluate(context.Background(), inputs, "diff")
	if len(res1.Diagnostics) != 1 || res1.Diagnostics[0].Code != ruleeval.CodeViolated {
		t.Fatalf("first run: expected RE001, got %v", res1.Diagnostics)
	}
	if cache.puts != 1 {
		t.Fatalf("expected 1 cache put, got %d", cache.puts)
	}

	res2 := e.Evaluate(context.Background(), inputs, "diff")
	if len(bp.batches) != 1 {
		t.Fatalf("expected provider called once across both runs, got %d", len(bp.batches))
	}
	if len(res2.Diagnostics) != 1 || res2.Diagnostics[0].Path != "f.go" || res2.Diagnostics[0].Line != 3 {
		t.Fatalf("second run: expected cached violation with evidence, got %v", res2.Diagnostics)
	}
	if obs.count(ruleeval.EventCacheMiss) != 1 || obs.count(ruleeval.EventCacheHit) != 1 {
		t.Errorf("expected 1 miss + 1 hit, got %d miss / %d hit", obs.count(ruleeval.EventCacheMiss), obs.count(ruleeval.EventCacheHit))
	}
	if obs.count(ruleeval.EventProviderCall) != 1 {
		t.Errorf("expected 1 provider-call event, got %d", obs.count(ruleeval.EventProviderCall))
	}

	// A different diff must miss the cache.
	e.Evaluate(context.Background(), inputs, "other diff")
	if len(bp.batches) != 2 {
		t.Fatalf("expected a new provider call for a different diff, got %d", len(bp.batches))
	}
}

func TestEvaluate_CacheError_NonFatal(t *testing.T) {
	bp := &batchProvider{responses: map[string]*ruleeval.EvalResponse{"A-001": {Verdict: ruleeval.Compliant}}}
	cache := newMemCache()
	cache.getErr = errors.New("db locked")
	obs := &recordingObserver{}
	e, err := ruleeval.New(bp, ruleeval.WithCache(cache), ruleeval.WithObserver(obs))
	if err != nil {
		t.Fatal(err)
	}
	res := e.Evaluate(context.Background(), []ruleeval.RuleInput{entityInput("A", "A-001", "I")}, "diff")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("cache errors must not produce diagnostics, got %v", res.Diagnostics)
	}
	if len(bp.batches) != 1 {
		t.Fatalf("expected provider fallback on cache error, got %d calls", len(bp.batches))
	}
	if obs.count(ruleeval.EventCacheError) != 1 {
		t.Errorf("expected 1 cache-error event, got %d", obs.count(ruleeval.EventCacheError))
	}
}

func TestEvaluate_Concurrency_PreservesOrder(t *testing.T) {
	responses := map[string]*ruleeval.EvalResponse{}
	var inputs []ruleeval.RuleInput
	for _, ent := range []string{"A", "B", "C", "D", "E", "F"} {
		id := ent + "-001"
		responses[id] = &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Explanation: ent}
		inputs = append(inputs, entityInput(ent, id, "I"))
	}
	bp := &batchProvider{responses: responses}
	e, err := ruleeval.New(bp, ruleeval.WithConcurrency(4))
	if err != nil {
		t.Fatal(err)
	}
	res := e.Evaluate(context.Background(), inputs, "diff")
	if len(res.Diagnostics) != len(inputs) {
		t.Fatalf("expected %d diagnostics, got %d", len(inputs), len(res.Diagnostics))
	}
	for i, d := range res.Diagnostics {
		want := inputs[i].Rule.ID
		if !strings.Contains(d.Message, want) {
			t.Errorf("diagnostic %d: expected rule %s, got %q", i, want, d.Message)
		}
	}
}

func TestEvaluator_Usage(t *testing.T) {
	bp := &batchProvider{usage: ruleeval.Usage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 3, Requests: 1}}
	e, err := ruleeval.New(bp)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Usage(); got.InputTokens != 10 || got.Requests != 1 {
		t.Errorf("unexpected usage %+v", got)
	}

	mp := &mockProvider{}
	e2, _ := ruleeval.New(mp)
	if got := e2.Usage(); got != (ruleeval.Usage{}) {
		t.Errorf("expected zero usage for provider without Usage(), got %+v", got)
	}
}

func TestUsage_Add(t *testing.T) {
	var u ruleeval.Usage
	u.Add(ruleeval.Usage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, Requests: 1})
	u.Add(ruleeval.Usage{InputTokens: 10, Requests: 2})
	want := ruleeval.Usage{InputTokens: 11, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, Requests: 3}
	if u != want {
		t.Errorf("got %+v want %+v", u, want)
	}
}

func TestCacheKey(t *testing.T) {
	base := ruleeval.RuleInput{
		Rule:  entity.Rule{ID: "A-001", Revision: 1, Body: "body"},
		Notes: []entity.Note{{ID: "A-001/01", Type: "rationale", Content: "why"}},
	}
	k1 := ruleeval.CacheKey(base, "d1", "m", "v1")
	if len(k1) != 64 {
		t.Fatalf("expected 64 hex chars, got %d: %q", len(k1), k1)
	}
	if k1 != ruleeval.CacheKey(base, "d1", "m", "v1") {
		t.Error("key must be deterministic")
	}

	variants := map[string]ruleeval.RuleInput{}
	rev := base
	rev.Rule.Revision = 2
	variants["revision"] = rev
	body := base
	body.Rule.Body = "other"
	variants["body"] = body
	note := base
	note.Notes = []entity.Note{{ID: "A-001/01", Type: "rationale", Content: "changed"}}
	variants["note"] = note
	for name, v := range variants {
		if ruleeval.CacheKey(v, "d1", "m", "v1") == k1 {
			t.Errorf("changing %s must change the key", name)
		}
	}
	if ruleeval.CacheKey(base, "d2", "m", "v1") == k1 {
		t.Error("diff hash must affect key")
	}
	if ruleeval.CacheKey(base, "d1", "m2", "v1") == k1 {
		t.Error("model must affect key")
	}
	if ruleeval.CacheKey(base, "d1", "m", "v2") == k1 {
		t.Error("prompt version must affect key")
	}
	src := base
	src.Source = "./rules/"
	if ruleeval.CacheKey(src, "d1", "m", "v1") == k1 {
		t.Error("source must affect key")
	}
	scoped := src
	scoped.Scope = "local"
	if ruleeval.CacheKey(scoped, "d1", "m", "v1") == ruleeval.CacheKey(src, "d1", "m", "v1") {
		t.Error("scope must affect key: a local rule made global must be re-evaluated")
	}

	// Note order must not matter.
	two := base
	two.Notes = []entity.Note{
		{ID: "A-001/02", Type: "example", Content: "e"},
		{ID: "A-001/01", Type: "rationale", Content: "why"},
	}
	swapped := base
	swapped.Notes = []entity.Note{two.Notes[1], two.Notes[0]}
	if ruleeval.CacheKey(two, "d", "m", "v") != ruleeval.CacheKey(swapped, "d", "m", "v") {
		t.Error("note order must not affect key")
	}
	// State must not affect the key (severity concern, not verdict).
	st := base
	st.Rule.State = "I"
	if ruleeval.CacheKey(st, "d1", "m", "v1") != k1 {
		t.Error("state must not affect key")
	}
}

func TestRuleInputsFromFile_EntityID(t *testing.T) {
	f := &entity.File{
		Entity: entity.Entity{ID: "PARENT"},
		Rules:  []entity.Rule{{ID: "PARENT-001", State: "I"}},
		SubEntities: []entity.File{{
			Entity: entity.Entity{ID: "CHILD"},
			Rules:  []entity.Rule{{ID: "CHILD-001", State: "I"}},
		}},
	}
	inputs := ruleeval.RuleInputsFromFile(f)
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}
	if inputs[0].EntityID != "PARENT" || inputs[1].EntityID != "CHILD" {
		t.Errorf("unexpected entity ids: %q %q", inputs[0].EntityID, inputs[1].EntityID)
	}
}
