//go:build live

package claude_test

import (
	"context"
	"os"
	"strings"
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

// TestLive_PartialDiffListsTheOtherChunk checks the <diff_scope> instruction: a
// chunk may not report a change as missing when the file list names its file,
// and the list must not hide a change that really is missing.
func TestLive_PartialDiffListsTheOtherChunk(t *testing.T) {
	if os.Getenv(claude.EnvAPIKey) == "" {
		t.Skipf("%s not set", claude.EnvAPIKey)
	}
	const body = "A change to src/result/codes_table.go MUST also change docs/SPEC.md."
	tests := []struct {
		name     string
		diff     string
		want     ruleeval.Verdict
		requests int // one verdict request per chunk, plus a detail request if violated
	}{
		{name: "SPEC in the other chunk", diff: specDiff + codeDiff, want: ruleeval.Compliant, requests: 2},
		{
			name:     "SPEC not in the diff",
			diff:     strings.ReplaceAll(specDiff, "docs/SPEC.md", "docs/README.md") + codeDiff,
			want:     ruleeval.Violated,
			requests: 3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg claude.Config
			twoChunks(&cfg)
			p, err := claude.New(cfg)
			if err != nil {
				t.Fatal(err)
			}
			resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{
				EntityID: "MOD",
				Inputs:   []ruleeval.RuleInput{rule("MOD-006", body)},
				Diff:     tt.diff,
			})
			if err != nil {
				t.Fatal(err)
			}
			if n := p.Usage().Requests; n < tt.requests {
				t.Fatalf("%d request(s), want %d: the diff did not split, so this proves nothing", n, tt.requests)
			}
			v := resp.Verdicts["MOD-006"]
			if v == nil {
				t.Fatalf("no verdict returned: %+v", resp)
			}
			t.Logf("verdict=%s explanation=%q", v.Verdict, v.Explanation)
			if v.Verdict != tt.want {
				t.Errorf("verdict = %s, want %s: %s", v.Verdict, tt.want, v.Explanation)
			}
		})
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
