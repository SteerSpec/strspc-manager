//go:build live

package claude_test

import (
	"context"
	"os"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
)

// TestLive_Smoke calls the real Anthropic API. Run with:
//
//	ANTHROPIC_API_KEY=... go test -tags live ./src/ruleeval/claude/ -run TestLive -v
func TestLive_Smoke(t *testing.T) {
	if os.Getenv(claude.EnvAPIKey) == "" {
		t.Skipf("%s not set", claude.EnvAPIKey)
	}
	p, err := claude.New(claude.Config{})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{
		EntityID: "CMD",
		Inputs:   []ruleeval.RuleInput{rule("CMD-001", cobraLongRule)},
		Diff:     diffMissingLong,
	})
	if err != nil {
		t.Fatal(err)
	}
	v := resp.Verdicts["CMD-001"]
	if v == nil {
		t.Fatalf("no verdict returned: %+v", resp)
	}
	t.Logf("verdict=%s confidence=%.2f explanation=%q evidence=%+v", v.Verdict, v.Confidence, v.Explanation, v.Evidence)
	if v.Verdict != ruleeval.Violated {
		t.Errorf("expected the missing Long description to be flagged, got %s", v.Verdict)
	}
	u := p.Usage()
	t.Logf("usage=%+v", u)
	// A violated verdict costs two requests: the verdicts, then its details.
	if u.Requests != 2 || u.InputTokens == 0 {
		t.Errorf("usage not accumulated: %+v", u)
	}
}

// TestLive_ExtendedRuleIsNotJudged checks that a diff breaking only the
// extended rule is not reported against the rule extending it.
func TestLive_ExtendedRuleIsNotJudged(t *testing.T) {
	if os.Getenv(claude.EnvAPIKey) == "" {
		t.Skipf("%s not set", claude.EnvAPIKey)
	}
	p, err := claude.New(claude.Config{})
	if err != nil {
		t.Fatal(err)
	}
	in := rule("CMD-002", "The Long description of a cobra command MUST end with a full stop.")
	in.Extended = []inherit.Extended{{ID: "core:CMD-001", Revision: 1, Body: cobraLongRule}}
	resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{
		EntityID: "CMD",
		Inputs:   []ruleeval.RuleInput{in},
		Diff:     diffMissingLong,
	})
	if err != nil {
		t.Fatal(err)
	}
	v := resp.Verdicts["CMD-002"]
	if v == nil {
		t.Fatalf("no verdict returned: %+v", resp)
	}
	t.Logf("verdict=%s explanation=%q", v.Verdict, v.Explanation)
	if v.Verdict == ruleeval.Violated {
		t.Errorf("the extended rule's violation was reported against CMD-002: %s", v.Explanation)
	}
}
