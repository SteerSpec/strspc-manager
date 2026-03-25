package ruleeval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// mockProvider returns pre-configured responses keyed by rule ID.
type mockProvider struct {
	responses map[string]*ruleeval.EvalResponse
	errors    map[string]error
	calls     []ruleeval.EvalRequest // records all calls for inspection
}

func (m *mockProvider) Evaluate(_ context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	m.calls = append(m.calls, req)
	if err, ok := m.errors[req.Rule.ID]; ok {
		return nil, err
	}
	if resp, ok := m.responses[req.Rule.ID]; ok {
		return resp, nil
	}
	return &ruleeval.EvalResponse{Verdict: ruleeval.Compliant}, nil
}

func makeRule(id, state string) entity.Rule {
	return entity.Rule{
		ID:    id,
		State: state,
		Body:  "test rule " + id,
	}
}

func makeInput(id, state string) ruleeval.RuleInput {
	return ruleeval.RuleInput{Rule: makeRule(id, state)}
}

func TestNew_NilProviderWithoutStaticOnly(t *testing.T) {
	_, err := ruleeval.New(nil)
	if err == nil {
		t.Fatal("expected error when provider is nil and StaticOnly is false")
	}
}

func TestNew_NilProviderWithStaticOnly(t *testing.T) {
	e, err := ruleeval.New(nil, ruleeval.WithStaticOnly(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil Evaluator")
	}
}

func TestNew_InvalidFailOnState(t *testing.T) {
	mp := &mockProvider{}
	_, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I", "X"}))
	if err == nil {
		t.Fatal("expected error for invalid state code")
	}
}

func TestNew_ValidFailOnStates(t *testing.T) {
	mp := &mockProvider{}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I", "P"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil Evaluator")
	}
}

func TestEvaluate_AllCompliant(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Compliant},
			"ENT-002": {Verdict: ruleeval.Compliant},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{
		makeInput("ENT-001", "I"),
		makeInput("ENT-002", "I"),
	}
	res := e.Evaluate(context.Background(), inputs, "diff content")

	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
}

func TestEvaluate_Violated_InFailOn(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Violated, Explanation: "missing null check"},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeViolated {
		t.Errorf("expected code %s, got %s", ruleeval.CodeViolated, d.Code)
	}
	if d.Severity != result.Error {
		t.Errorf("expected Error severity, got %s", d.Severity)
	}
	if !res.OK() {
		// Expected: OK() returns false because there's an Error
	} else {
		t.Error("expected result to not be OK")
	}
}

func TestEvaluate_Violated_NotInFailOn(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Violated, Explanation: "style concern"},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	// Rule is in state "D" (Draft), not in fail_on
	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "D")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeViolated {
		t.Errorf("expected code %s, got %s", ruleeval.CodeViolated, d.Code)
	}
	if d.Severity != result.Warning {
		t.Errorf("expected Warning severity, got %s", d.Severity)
	}
	if !res.OK() {
		t.Error("expected result to be OK (only warnings)")
	}
}

func TestEvaluate_NotApplicable(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.NotApplicable},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics, got %d", len(res.Diagnostics))
	}
}

func TestEvaluate_ProviderError(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-002": {Verdict: ruleeval.Compliant},
		},
		errors: map[string]error{
			"ENT-001": errors.New("connection timeout"),
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{
		makeInput("ENT-001", "I"),
		makeInput("ENT-002", "I"),
	}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic (provider error), got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeProviderError {
		t.Errorf("expected code %s, got %s", ruleeval.CodeProviderError, d.Code)
	}
	if d.Severity != result.Warning {
		t.Errorf("expected Warning severity, got %s", d.Severity)
	}
	// Second rule should still have been evaluated
	if len(mp.calls) != 2 {
		t.Errorf("expected 2 provider calls, got %d", len(mp.calls))
	}
}

func TestEvaluate_StaticOnly(t *testing.T) {
	e, err := ruleeval.New(nil, ruleeval.WithStaticOnly(true))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic (static-only), got %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeStaticOnly {
		t.Errorf("expected code %s, got %s", ruleeval.CodeStaticOnly, d.Code)
	}
	if d.Severity != result.Info {
		t.Errorf("expected Info severity, got %s", d.Severity)
	}
}

func TestEvaluate_MixedOutcomes(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Compliant},
			"ENT-002": {Verdict: ruleeval.Violated, Explanation: "bad pattern"},
			"ENT-003": {Verdict: ruleeval.NotApplicable},
			"ENT-004": {Verdict: ruleeval.Violated, Explanation: "draft concern"},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{
		makeInput("ENT-001", "I"), // compliant → no diag
		makeInput("ENT-002", "I"), // violated + in fail_on → Error
		makeInput("ENT-003", "I"), // not applicable → no diag
		makeInput("ENT-004", "D"), // violated + not in fail_on → Warning
	}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}

	// First: ENT-002 violated as Error
	if res.Diagnostics[0].Severity != result.Error {
		t.Errorf("expected Error for ENT-002, got %s", res.Diagnostics[0].Severity)
	}
	// Second: ENT-004 violated as Warning
	if res.Diagnostics[1].Severity != result.Warning {
		t.Errorf("expected Warning for ENT-004, got %s", res.Diagnostics[1].Severity)
	}
	if res.OK() {
		t.Error("expected result to not be OK (has Error)")
	}
}

func TestEvaluate_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Violated, Explanation: "first"},
			"ENT-002": {Verdict: ruleeval.Violated, Explanation: "second"},
		},
	}

	wrapping := &cancellingProvider{inner: mp, cancelAfter: 1, cancel: cancel}

	e, err := ruleeval.New(wrapping, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{
		makeInput("ENT-001", "I"),
		makeInput("ENT-002", "I"),
	}
	res := e.Evaluate(ctx, inputs, "diff")

	// Should have at most 1 diagnostic (first rule evaluated, then context cancelled)
	if len(res.Diagnostics) > 1 {
		t.Errorf("expected at most 1 diagnostic after cancellation, got %d", len(res.Diagnostics))
	}
}

// cancellingProvider cancels the context after N calls.
type cancellingProvider struct {
	inner       ruleeval.Provider
	cancelAfter int
	cancel      context.CancelFunc
	count       int
}

func (p *cancellingProvider) Evaluate(ctx context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	resp, err := p.inner.Evaluate(ctx, req)
	p.count++
	if p.count >= p.cancelAfter {
		p.cancel()
	}
	return resp, err
}

func TestRuleInputsFromFile(t *testing.T) {
	f := &entity.File{
		Rules: []entity.Rule{
			{ID: "ENT-001", State: "I", Body: "rule one"},
			{ID: "ENT-002", State: "P", Body: "rule two"},
		},
		Notes: []entity.Note{
			{ID: "ENT-001/01", RuleRef: "ENT-001", Type: "rationale", Content: "why rule one"},
			{ID: "ENT-001/02", RuleRef: "ENT-001", Type: "example", Content: "good example"},
			{ID: "ENT-002/01", RuleRef: "ENT-002", Type: "applies_to", Content: "Go files"},
		},
	}

	inputs := ruleeval.RuleInputsFromFile(f)
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}

	if inputs[0].Rule.ID != "ENT-001" {
		t.Errorf("expected ENT-001, got %s", inputs[0].Rule.ID)
	}
	if len(inputs[0].Notes) != 2 {
		t.Errorf("expected 2 notes for ENT-001, got %d", len(inputs[0].Notes))
	}

	if inputs[1].Rule.ID != "ENT-002" {
		t.Errorf("expected ENT-002, got %s", inputs[1].Rule.ID)
	}
	if len(inputs[1].Notes) != 1 {
		t.Errorf("expected 1 note for ENT-002, got %d", len(inputs[1].Notes))
	}
}

func TestRuleInputsFromFile_SubEntities(t *testing.T) {
	f := &entity.File{
		Rules: []entity.Rule{
			{ID: "ENT-001", State: "I", Body: "parent rule"},
		},
		SubEntities: []entity.File{
			{
				Rules: []entity.Rule{
					{ID: "SUB-001", State: "I", Body: "child rule"},
				},
				Notes: []entity.Note{
					{ID: "SUB-001/01", RuleRef: "SUB-001", Type: "rationale", Content: "child rationale"},
				},
			},
		},
	}

	inputs := ruleeval.RuleInputsFromFile(f)
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs (parent + child), got %d", len(inputs))
	}
	if inputs[1].Rule.ID != "SUB-001" {
		t.Errorf("expected SUB-001, got %s", inputs[1].Rule.ID)
	}
	if len(inputs[1].Notes) != 1 {
		t.Errorf("expected 1 note for SUB-001, got %d", len(inputs[1].Notes))
	}
}

func TestRuleInputsFromFile_Nil(t *testing.T) {
	inputs := ruleeval.RuleInputsFromFile(nil)
	if inputs != nil {
		t.Fatalf("expected nil for nil file, got %v", inputs)
	}
}

func TestEvalRequest_NotesPassedToProvider(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Compliant},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	notes := []entity.Note{
		{ID: "ENT-001/01", RuleRef: "ENT-001", Type: "rationale", Content: "because reasons"},
		{ID: "ENT-001/02", RuleRef: "ENT-001", Type: "example", Content: "good code"},
	}
	inputs := []ruleeval.RuleInput{
		{Rule: makeRule("ENT-001", "I"), Notes: notes},
	}

	e.Evaluate(context.Background(), inputs, "the diff")

	if len(mp.calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(mp.calls))
	}
	call := mp.calls[0]
	if len(call.Notes) != 2 {
		t.Errorf("expected 2 notes in request, got %d", len(call.Notes))
	}
	if call.Diff != "the diff" {
		t.Errorf("expected diff 'the diff', got %q", call.Diff)
	}
}

func TestEvaluate_EmptyInputs(t *testing.T) {
	mp := &mockProvider{}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	res := e.Evaluate(context.Background(), nil, "diff")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected 0 diagnostics for empty inputs, got %d", len(res.Diagnostics))
	}
}

func TestEvaluate_NilResponse(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": nil, // explicit nil
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeProviderError {
		t.Errorf("expected code %s, got %s", ruleeval.CodeProviderError, d.Code)
	}
	if d.Severity != result.Warning {
		t.Errorf("expected Warning severity, got %s", d.Severity)
	}
}

func TestEvaluate_UnknownVerdict(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Verdict("maybe")},
		},
	}
	e, err := ruleeval.New(mp, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d: %v", len(res.Diagnostics), res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeProviderError {
		t.Errorf("expected code %s, got %s", ruleeval.CodeProviderError, d.Code)
	}
	if d.Severity != result.Warning {
		t.Errorf("expected Warning severity, got %s", d.Severity)
	}
}

func TestEvaluate_NoFailOn(t *testing.T) {
	mp := &mockProvider{
		responses: map[string]*ruleeval.EvalResponse{
			"ENT-001": {Verdict: ruleeval.Violated, Explanation: "bad"},
		},
	}
	// No WithFailOn — all violations should be Warning
	e, err := ruleeval.New(mp)
	if err != nil {
		t.Fatal(err)
	}

	inputs := []ruleeval.RuleInput{makeInput("ENT-001", "I")}
	res := e.Evaluate(context.Background(), inputs, "diff")

	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(res.Diagnostics))
	}
	if res.Diagnostics[0].Severity != result.Warning {
		t.Errorf("expected Warning when no fail_on configured, got %s", res.Diagnostics[0].Severity)
	}
}
