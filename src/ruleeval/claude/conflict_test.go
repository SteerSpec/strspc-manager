package claude_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
)

// conflictServer answers every request with text and records the bodies.
func conflictServer(t *testing.T, text string) (*claude.Provider, *[]string) {
	t.Helper()
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		writeJSON(w, 200, message("end_turn", text, nil))
	}))
	t.Cleanup(srv.Close)
	p, err := claude.New(claude.Config{APIKey: "k", BaseURL: srv.URL, HTTPClient: srv.Client(), MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	return p, &bodies
}

func draftOverride() ruleeval.RuleInput {
	return ruleeval.RuleInput{
		Rule:       entity.Rule{ID: "SAV-001", State: "D", Body: "Savings accounts MAY have no owner."},
		EntityID:   "SAV",
		Overridden: &inherit.Extended{ID: "core:ACCT-001", Revision: 1, Body: "Accounts MUST have an owner."},
	}
}

func TestCheckConflict(t *testing.T) {
	p, bodies := conflictServer(t, `{"conflicts":true,"explanation":"It allows an account with no owner."}`)
	resp, err := p.CheckConflict(context.Background(), ruleeval.ConflictRequest{Rule: draftOverride()})
	if err != nil {
		t.Fatal(err)
	}
	if !resp.Conflicts || resp.Explanation == "" {
		t.Errorf("resp = %+v, want a conflict", resp)
	}
	if len(*bodies) != 1 {
		t.Fatalf("%d requests, want 1", len(*bodies))
	}
	for _, want := range []string{"proposes to override: core:ACCT-001", "Accounts MUST have an owner.", `"conflicts"`} {
		if !strings.Contains((*bodies)[0], want) {
			t.Errorf("request lacks %q", want)
		}
	}
	if u := p.Usage(); u.Requests != 1 {
		t.Errorf("Usage.Requests = %d, want 1", u.Requests)
	}
}

func TestCheckConflict_Malformed(t *testing.T) {
	for _, text := range []string{`not json`, `{"conflicts":false,"explanation":" "}`} {
		p, _ := conflictServer(t, text)
		if _, err := p.CheckConflict(context.Background(), ruleeval.ConflictRequest{Rule: draftOverride()}); !errors.Is(err, claude.ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", text, err)
		}
	}
}

func TestCheckConflict_NeedsTheOverriddenRule(t *testing.T) {
	p, bodies := conflictServer(t, `{}`)
	in := draftOverride()
	in.Overridden = nil
	if _, err := p.CheckConflict(context.Background(), ruleeval.ConflictRequest{Rule: in}); err == nil {
		t.Error("CheckConflict without the overridden rule succeeded")
	}
	if len(*bodies) != 0 {
		t.Errorf("%d requests sent, want none", len(*bodies))
	}
}

func TestConflictPromptVersion(t *testing.T) {
	p, err := claude.New(claude.Config{APIKey: "k", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.ConflictPromptVersion(); got != "conflict-c1/effort=high" {
		t.Errorf("ConflictPromptVersion = %q", got)
	}
	var _ ruleeval.ConflictChecker = p
}
