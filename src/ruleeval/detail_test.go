package ruleeval_test

import (
	"context"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// TestDetail_Covers checks which verdicts each level details: violated
// always, compliant only with DetailCompliant, not_applicable never.
func TestDetail_Covers(t *testing.T) {
	for _, tc := range []struct {
		d    ruleeval.Detail
		v    ruleeval.Verdict
		want bool
	}{
		{ruleeval.DetailViolated, ruleeval.Violated, true},
		{ruleeval.DetailViolated, ruleeval.Compliant, false},
		{ruleeval.DetailViolated, ruleeval.NotApplicable, false},
		{ruleeval.DetailCompliant, ruleeval.Violated, true},
		{ruleeval.DetailCompliant, ruleeval.Compliant, true},
		{ruleeval.DetailCompliant, ruleeval.NotApplicable, false},
		{ruleeval.DetailCompliant, ruleeval.Verdict("maybe"), false},
	} {
		if got := tc.d.Covers(tc.v); got != tc.want {
			t.Errorf("%v.Covers(%q) = %v, want %v", tc.d, tc.v, got, tc.want)
		}
	}
}

// TestDetail_String checks the names cache keys mix in.
func TestDetail_String(t *testing.T) {
	for d, want := range map[ruleeval.Detail]string{
		ruleeval.DetailViolated:  "violated",
		ruleeval.DetailCompliant: "compliant",
		ruleeval.Detail(9):       "Detail(9)",
	} {
		if got := d.String(); got != want {
			t.Errorf("Detail(%d).String() = %q, want %q", int(d), got, want)
		}
	}
}

// TestWithDetail_InvalidLevel checks that New refuses an unknown level.
func TestWithDetail_InvalidLevel(t *testing.T) {
	if _, err := ruleeval.New(&mockProvider{}, ruleeval.WithDetail(ruleeval.Detail(9))); err == nil {
		t.Fatal("New accepted an unknown detail level")
	}
}

// TestWithDetail_ForwardsToBatch checks that a batch request carries the
// configured level, and the default when none is set.
func TestWithDetail_ForwardsToBatch(t *testing.T) {
	inputs := []ruleeval.RuleInput{{Rule: makeRule("CMD-001", "I"), EntityID: "CMD"}}
	for _, d := range []ruleeval.Detail{ruleeval.DetailViolated, ruleeval.DetailCompliant} {
		p := &batchProvider{responses: map[string]*ruleeval.EvalResponse{"CMD-001": {Verdict: ruleeval.Compliant}}}
		opts := []ruleeval.Option{}
		if d != ruleeval.DetailViolated {
			opts = append(opts, ruleeval.WithDetail(d))
		}
		ev, err := ruleeval.New(p, opts...)
		if err != nil {
			t.Fatal(err)
		}
		ev.Evaluate(context.Background(), inputs, "diff --git a/x b/x\n")
		if len(p.batches) != 1 || p.batches[0].Detail != d {
			t.Errorf("batches = %+v, want one request with Detail %v", p.batches, d)
		}
	}
}

// TestWithDetail_ForwardsToEvaluate checks that a per-rule request carries
// the configured level.
func TestWithDetail_ForwardsToEvaluate(t *testing.T) {
	p := &mockProvider{}
	ev, err := ruleeval.New(p, ruleeval.WithDetail(ruleeval.DetailCompliant))
	if err != nil {
		t.Fatal(err)
	}
	ev.Evaluate(context.Background(), []ruleeval.RuleInput{{Rule: makeRule("CMD-001", "I"), EntityID: "CMD"}}, "diff --git a/x b/x\n")
	if len(p.calls) != 1 || p.calls[0].Detail != ruleeval.DetailCompliant {
		t.Errorf("calls = %+v, want one request with DetailCompliant", p.calls)
	}
}

// TestWithDetail_CachesApart checks that a detailed verdict is not served
// from an undetailed one's cache entry, nor the other way round.
func TestWithDetail_CachesApart(t *testing.T) {
	inputs := []ruleeval.RuleInput{{Rule: makeRule("CMD-001", "I"), EntityID: "CMD"}}
	diff := "diff --git a/x b/x\n"
	cache := newMemCache()
	p := &batchProvider{responses: map[string]*ruleeval.EvalResponse{"CMD-001": {Verdict: ruleeval.Compliant}}}
	run := func(opts ...ruleeval.Option) {
		t.Helper()
		ev, err := ruleeval.New(p, append([]ruleeval.Option{ruleeval.WithCache(cache)}, opts...)...)
		if err != nil {
			t.Fatal(err)
		}
		ev.Evaluate(context.Background(), inputs, diff)
	}
	run()
	run(ruleeval.WithDetail(ruleeval.DetailCompliant))
	if len(p.batches) != 2 || len(cache.entries) != 2 {
		t.Fatalf("a detailed run must miss the undetailed entry: %d requests, %d entries", len(p.batches), len(cache.entries))
	}
	run()
	run(ruleeval.WithDetail(ruleeval.DetailCompliant))
	if len(p.batches) != 2 {
		t.Errorf("each level must hit its own entry on a rerun, provider saw %d requests", len(p.batches))
	}
}
