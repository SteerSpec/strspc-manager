package claude_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/internal/replay"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// lintGo is a cobra command that sets Long, on line 9.
const lintGo = `package cmd

import "github.com/spf13/cobra"

func newLintCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "lint",
		Short: "Lint entity files",
		Long:  "Validate entity files against the SteerSpec schema and business rules.",
	}
}
`

// lintGoNoLong is lintGo without its Long description.
var lintGoNoLong = strings.Replace(lintGo, "\t\tLong:  \"Validate entity files against the SteerSpec schema and business rules.\",\n", "", 1)

// generatedGo is a Go file of n numbered declarations.
func generatedGo(n int) string {
	var b strings.Builder
	b.WriteString("package gen\n")
	for i := 2; i <= n; i++ {
		fmt.Fprintf(&b, "var generated%03d = %q\n", i, strings.Repeat("x", 20))
	}
	return b.String()
}

// isAuditRequest reports whether a request carries the audit system prompt.
func isAuditRequest(body string) bool { return strings.Contains(body, "SteerSpec rule auditor") }

// auditBudget sizes MaxInputTokens so that a part of an audit of inputs
// holds room bytes of file slices, past the system prompt, the overhead,
// the rules and a scope listing paths.
func auditBudget(inputs []ruleeval.RuleInput, paths []string, room int) func(*claude.Config) {
	return func(c *claude.Config) {
		c.MaxInputTokens = claude.EstimateTokens(prompt.AuditSystem()) + 256 +
			claude.EstimateTokens(prompt.RulesBlock(inputs)) +
			claude.EstimateTokens(prompt.PartialFilesBlock("", paths, 0)) +
			claude.EstimateTokens(strings.Repeat("x", room))
	}
}

func auditScenarios() []scenario {
	cmd := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule)}
	lint := ruleeval.File{Path: "src/cmd/lint.go", Content: lintGo}
	sync := ruleeval.File{Path: "src/cmd/sync.go", Content: strings.ReplaceAll(lintGo, "lint", "sync")}
	oneSlice := len(prompt.FileSlice(lint.Path, 1, 11, strings.Split(strings.TrimSuffix(lintGo, "\n"), "\n")))
	compliant := func(w http.ResponseWriter, _ string, _ int) {
		writeJSON(w, 200, message("end_turn", verdictsText("CMD-001", "compliant"), nil))
	}
	return []scenario{
		{
			name:   "audit_compliant",
			inputs: cmd,
			files:  []ruleeval.File{lint},
			script: compliant,
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.Compliant || rt.Requests() != 1 {
					t.Fatalf("verdict = %+v after %d requests, want compliant after one", v, rt.Requests())
				}
			},
		},
		{
			name:   "audit_missing_violated",
			inputs: cmd,
			files:  []ruleeval.File{{Path: lint.Path, Content: lintGoNoLong}},
			// Something the rule requires is missing: the auditor answers
			// violated and cites the line of the command that lacks it.
			script: func(w http.ResponseWriter, body string, _ int) {
				if isDetailRequest(body) {
					writeJSON(w, 200, message("end_turn", detailText(
						"newLintCmd returns a cobra.Command without Long.", 0.9,
						locationJSON{File: "src/cmd/lint.go", Line: 6, Excerpt: "return &cobra.Command{"}), nil))
					return
				}
				writeJSON(w, 200, message("end_turn", verdictsText("CMD-001", "violated"), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				v := resp.Verdicts["CMD-001"]
				if v == nil || v.Verdict != ruleeval.Violated || len(v.Evidence) != 1 || v.Evidence[0].Line != 6 {
					t.Fatalf("verdict = %+v, want violated at line 6", v)
				}
				if rt.Requests() != 2 {
					t.Errorf("expected a verdict and a detail request, transport saw %d", rt.Requests())
				}
			},
		},
		{
			name:   "audit_compliant_detailed",
			detail: ruleeval.DetailCompliant,
			inputs: cmd,
			files:  []ruleeval.File{lint},
			script: func(w http.ResponseWriter, body string, _ int) {
				if isDetailRequest(body) {
					writeJSON(w, 200, message("end_turn", verdictDetailText("compliant",
						"newLintCmd sets Long.", 0.95,
						locationJSON{File: "src/cmd/lint.go", Line: 9, Excerpt: `Long:  "Validate entity files`}), nil))
					return
				}
				compliant(w, body, 1)
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				v := resp.Verdicts["CMD-001"]
				if v == nil || v.Verdict != ruleeval.Compliant || len(v.Evidence) != 1 || v.Evidence[0].Line != 9 || rt.Requests() != 2 {
					t.Fatalf("verdict = %+v after %d requests, want compliant at line 9 after two", v, rt.Requests())
				}
			},
		},
		{
			name:   "audit_single_rule",
			single: true,
			inputs: cmd,
			files:  []ruleeval.File{lint},
			script: compliant,
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, _ *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.Compliant {
					t.Fatalf("Evaluate verdict = %+v, want compliant", v)
				}
			},
		},
		{
			name:   "audit_two_parts",
			inputs: cmd,
			files:  []ruleeval.File{lint, sync},
			cfg:    auditBudget(cmd, []string{lint.Path, sync.Path}, oneSlice+20),
			script: compliant,
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.Compliant || len(resp.Errors) != 0 {
					t.Fatalf("verdict = %+v, errors = %v", v, resp.Errors)
				}
				if rt.Requests() != 2 {
					t.Errorf("expected one request per file, transport saw %d", rt.Requests())
				}
			},
		},
		{
			name:   "audit_split_file",
			inputs: cmd,
			files:  []ruleeval.File{{Path: "src/gen/big.go", Content: generatedGo(60)}},
			cfg:    auditBudget(cmd, []string{"src/gen/big.go"}, 900),
			script: func(w http.ResponseWriter, _ string, _ int) {
				writeJSON(w, 200, message("end_turn", verdictsText("CMD-001", "not_applicable"), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.NotApplicable || len(resp.Errors) != 0 {
					t.Fatalf("verdict = %+v, errors = %v", v, resp.Errors)
				}
				if rt.Requests() < 2 {
					t.Errorf("a file larger than a part should be split, transport saw %d requests", rt.Requests())
				}
			},
		},
	}
}

// TestAudit_RequestShowsNumberedFiles inspects the recorded bodies: an
// audit sends the audit system prompt and numbered lines, never the diff
// prompt, and a detail request uses the audit wording.
func TestAudit_RequestShowsNumberedFiles(t *testing.T) {
	for _, body := range cassetteBodies(t, "audit_missing_violated") {
		if !isAuditRequest(body) || strings.Contains(body, "SteerSpec rule evaluator") {
			t.Error("an audit request must carry the audit system prompt only")
		}
		if !strings.Contains(body, `file path=\"src/cmd/lint.go\" lines=\"1-10 of 10\"`) || !strings.Contains(body, `\n 6\t\treturn `) {
			t.Errorf("the file should be shown whole with numbered lines: %.400s", body)
		}
		if isDetailRequest(body) && (!strings.Contains(body, "against this code") || !strings.Contains(body, "as its file tag names it")) {
			t.Error("the detail request should use the audit instruction and schema")
		}
	}
}

// TestAudit_PartsListTheSet inspects the recorded bodies of split audits:
// every part lists the whole set, and a split file is shown in consecutive
// line ranges.
func TestAudit_PartsListTheSet(t *testing.T) {
	for i, body := range cassetteBodies(t, "audit_two_parts") {
		if !strings.Contains(body, `- src/cmd/lint.go\n- src/cmd/sync.go\n`) {
			t.Errorf("part %d does not list the set", i)
		}
	}
	var ranges []string
	for _, body := range cassetteBodies(t, "audit_split_file") {
		i := strings.Index(body, `lines=\"`)
		if i < 0 || !strings.Contains(body, `- src/gen/big.go\n`) {
			t.Fatalf("a part of the split file lacks its range or the set: %.300s", body)
		}
		ranges = append(ranges, body[i+len(`lines=\"`):i+len(`lines=\"`)+strings.Index(body[i+len(`lines=\"`):], `\"`)])
	}
	joined := strings.Join(ranges, ",")
	if !strings.Contains(joined, "1-") || !strings.Contains(joined, " of 60") {
		t.Errorf("ranges = %v, want the 60 lines split from line 1", ranges)
	}
}

// TestAuditPromptVersion_IsDistinct checks an audit is cached apart from a
// diff evaluation and follows both prompt revisions and the effort.
func TestAuditPromptVersion_IsDistinct(t *testing.T) {
	p, err := claude.New(claude.Config{APIKey: "k", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	got := p.AuditPromptVersion()
	if got == p.PromptVersion() || !strings.Contains(got, prompt.AuditVersion) || !strings.Contains(got, prompt.Version) || !strings.HasSuffix(got, "/effort=high") {
		t.Errorf("AuditPromptVersion = %q, PromptVersion = %q", got, p.PromptVersion())
	}
	var _ ruleeval.Auditor = p
}
