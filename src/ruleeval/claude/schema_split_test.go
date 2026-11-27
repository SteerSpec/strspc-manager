package claude_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// grammarTooLarge is the message the API returns when the output schema of
// a request compiles to a grammar over its limit (strspc-CLI#322).
const grammarTooLarge = "The compiled grammar is too large, which would cause performance issues. Simplify your tool schemas or reduce the number of strict tools."

// grammarServer answers like the Messages API but refuses, with the
// grammar-too-large 400, any request whose output schema is keyed by more
// than limit rule ids. Verdict requests are answered compliant, extractions
// with no findings and judgements compliant, so a split request is
// answered the same as a whole one. It records the rule ids of each
// request it answered or refused.
type grammarServer struct {
	limit int

	mu       sync.Mutex
	answered [][]string
	refused  [][]string
	// refusedKinds counts the refused requests by the type of their
	// per-rule values: string (verdicts), array (extraction), object
	// (judgement).
	refusedKinds map[string]int
}

func (g *grammarServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var req struct {
		OutputConfig struct {
			Format struct {
				Schema struct {
					Properties map[string]json.RawMessage `json:"properties"`
				} `json:"schema"`
			} `json:"format"`
		} `json:"output_config"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 400, apiError(400, "invalid_request_error", err.Error()))
		return
	}
	props := req.OutputConfig.Format.Schema.Properties
	if _, ok := props["verdict"]; ok { // a detail request for one rule
		writeJSON(w, 200, message("end_turn", verdictDetailText("compliant", "Holds.", 0.9), nil))
		return
	}
	ids := make([]string, 0, len(props))
	kind := ""
	for id, raw := range props {
		ids = append(ids, id)
		var v struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &v)
		kind = v.Type
	}
	g.mu.Lock()
	if len(ids) > g.limit {
		g.refused = append(g.refused, ids)
		if g.refusedKinds == nil {
			g.refusedKinds = map[string]int{}
		}
		g.refusedKinds[kind]++
		g.mu.Unlock()
		writeJSON(w, 400, apiError(400, "invalid_request_error", grammarTooLarge))
		return
	}
	g.answered = append(g.answered, ids)
	g.mu.Unlock()
	out := map[string]any{}
	for id, raw := range props {
		var v struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal(raw, &v)
		switch v.Type {
		case "string": // verdict request
			out[id] = "compliant"
		case "array": // extraction
			out[id] = []any{}
		default: // judgement
			out[id] = map[string]any{"verdict": "compliant", "explanation": "Holds.", "confidence": 0.9, "evidence": []any{}}
		}
	}
	b, _ := json.Marshal(out)
	writeJSON(w, 200, message("end_turn", string(b), nil))
}

func (g *grammarServer) provider(t *testing.T, opts ...func(*claude.Config)) *claude.Provider {
	t.Helper()
	t.Setenv(claude.EnvBaseURL, "")
	srv := httptest.NewTLSServer(g)
	t.Cleanup(srv.Close)
	cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(), MaxRetries: 1, Concurrency: 1}
	for _, o := range opts {
		o(&cfg)
	}
	p, err := claude.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func manyRules(n int) []ruleeval.RuleInput {
	ins := make([]ruleeval.RuleInput, n)
	for i := range ins {
		ins[i] = rule(fmt.Sprintf("CMD-%03d", i+1), "every command must have a test")
	}
	return ins
}

// checkAllCompliant fails unless every input got a compliant verdict and
// the batch reported no error.
func checkAllCompliant(t *testing.T, resp *ruleeval.BatchResponse, err error, inputs []ruleeval.RuleInput) {
	t.Helper()
	if err != nil {
		t.Fatalf("err = %v, want the group split until the schema fits", err)
	}
	if len(resp.Errors) != 0 {
		t.Errorf("errors = %v", resp.Errors)
	}
	for _, in := range inputs {
		if v := resp.Verdicts[in.Rule.ID]; v == nil || v.Verdict != ruleeval.Compliant {
			t.Errorf("%s = %+v, want compliant", in.Rule.ID, v)
		}
	}
}

// A diff evaluation whose verdict schema is refused as too large splits the
// group until each part is accepted.
func TestEvaluateBatch_SplitsAGroupWhoseSchemaIsTooLarge(t *testing.T) {
	g := &grammarServer{limit: 2}
	inputs := manyRules(5)
	resp, err := g.provider(t).EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Diff: diffWithLong})
	checkAllCompliant(t, resp, err, inputs)
	if len(g.refused) == 0 {
		t.Error("the server never refused a request: the test does not exercise the split")
	}
	for _, ids := range g.answered {
		if len(ids) > 2 {
			t.Errorf("a request over the limit was answered: %v", ids)
		}
	}
}

// An audit in one request splits the same way.
func TestAudit_SplitsAGroupWhoseSchemaIsTooLarge(t *testing.T) {
	g := &grammarServer{limit: 2}
	inputs := manyRules(5)
	files := []ruleeval.File{{Path: "src/cmd/lint.go", Content: commandGo("lint")}}
	resp, err := g.provider(t).EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Files: files})
	checkAllCompliant(t, resp, err, inputs)
	if len(g.refused) == 0 {
		t.Error("the server never refused a request: the test does not exercise the split")
	}
}

// An audit in parts splits both its extractions and its judgement.
func TestAuditInParts_SplitsAGroupWhoseSchemaIsTooLarge(t *testing.T) {
	g := &grammarServer{limit: 2}
	inputs := manyRules(5)
	files := []ruleeval.File{
		{Path: "src/cmd/lint.go", Content: commandGo("lint")},
		{Path: "src/cmd/sync.go", Content: commandGo("sync")},
		{Path: "src/cmd/lint_test.go", Content: commandTestGo("lint")},
	}
	p := g.provider(t, partsBudget(inputs, len(commandGo("lint"))+200))
	resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Files: files})
	checkAllCompliant(t, resp, err, inputs)
	if g.refusedKinds["array"] == 0 || g.refusedKinds["object"] == 0 {
		t.Errorf("refused %v, want both an extraction and a judgement refused", g.refusedKinds)
	}
}

// A single rule whose schema is still too large is reported for that rule;
// the others are evaluated and the batch does not fail.
func TestEvaluateBatch_ReportsASingleRuleWhoseSchemaIsTooLarge(t *testing.T) {
	g := &grammarServer{limit: 0}
	inputs := manyRules(2)
	resp, err := g.provider(t).EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Diff: diffWithLong})
	if err != nil {
		t.Fatalf("err = %v, want the rules reported one by one", err)
	}
	if len(resp.Verdicts) != 0 {
		t.Errorf("verdicts = %v, want none", resp.Verdicts)
	}
	for _, in := range inputs {
		found := false
		for _, e := range resp.Errors {
			if strings.Contains(e.Error(), in.Rule.ID) && errors.Is(e, claude.ErrSchemaTooLarge) {
				found = true
			}
		}
		if !found {
			t.Errorf("no ErrSchemaTooLarge error names %s: %v", in.Rule.ID, resp.Errors)
		}
	}
	for _, e := range resp.Errors {
		if errors.Is(e, ruleeval.ErrUnavailable) {
			t.Errorf("a schema too large is not the provider being unavailable: %v", e)
		}
	}
}

// The SDK's error text carries the response's workspace header, so a 400
// from a workspace-scoped key reads "(Workspace-ID: …)" whatever the API
// said. Only the API's own message decides how a 400 is classified
// (strspc-CLI#322 was reported as the provider being unavailable).
func TestEvaluateBatch_ClassifiesA400ByItsMessage(t *testing.T) {
	for _, tc := range []struct {
		name, msg   string
		unavailable bool
		tooLarge    bool
	}{
		{"grammar", grammarTooLarge, false, true},
		{"other", "messages.0.content.0.text: text content blocks must be non-empty", false, false},
		{"workspace key", "This API key is not scoped to a workspace, so this request must include the anthropic-workspace-id header.", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(claude.EnvBaseURL, "")
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("anthropic-workspace-id", "wrkspc_01synthetic")
				writeJSON(w, 400, apiError(400, "invalid_request_error", tc.msg))
			}))
			defer srv.Close()
			p, err := claude.New(claude.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(), MaxRetries: 1})
			if err != nil {
				t.Fatal(err)
			}
			resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: manyRules(1), Diff: diffWithLong})
			got := err
			if got == nil && len(resp.Errors) > 0 {
				got = resp.Errors[0]
			}
			if got == nil || !strings.Contains(got.Error(), "Workspace-ID") {
				t.Fatalf("err = %v, want the SDK's text with the workspace header", got)
			}
			if errors.Is(got, ruleeval.ErrUnavailable) != tc.unavailable {
				t.Errorf("err = %v, unavailable = %v, want %v", got, !tc.unavailable, tc.unavailable)
			}
			if errors.Is(got, claude.ErrSchemaTooLarge) != tc.tooLarge {
				t.Errorf("err = %v, schema too large = %v, want %v", got, !tc.tooLarge, tc.tooLarge)
			}
		})
	}
}

// The verdict schema of a diff counts against the rules' half of the budget
// (strspc-manager-z7d): forty rules whose text alone fits in one request
// but whose schema does not are split, with no request refused.
func TestEvaluateBatch_CountsTheVerdictSchemaInTheBudget(t *testing.T) {
	g := &grammarServer{limit: 1000}
	inputs := manyRules(40)
	rulesOnly := claude.EstimateTokens(prompt.RulesBlock(inputs))
	p := g.provider(t, func(c *claude.Config) {
		c.MaxRulesPerRequest = len(inputs)
		// The rules get half of what is left: room for their text with a
		// quarter to spare, not for their schema as well.
		c.MaxInputTokens = claude.EstimateTokens(prompt.System()) + 256 + 2*(rulesOnly+rulesOnly/4)
	})
	resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Diff: diffWithLong})
	checkAllCompliant(t, resp, err, inputs)
	if len(g.answered) < 2 {
		t.Errorf("answered %d verdict requests, want the rules split by their schema", len(g.answered))
	}
}
