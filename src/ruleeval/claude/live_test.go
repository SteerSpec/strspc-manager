//go:build live

package claude_test

import (
	"context"
	"os"
	"testing"

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
	if u.Requests != 1 || u.InputTokens == 0 {
		t.Errorf("usage not accumulated: %+v", u)
	}
}
