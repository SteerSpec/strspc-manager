package claude_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/internal/replay"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// testFileRule spans files: what it requires of a command file sits in
// another file.
const testFileRule = "Every cobra command MUST have a test file next to it, named after its file with _test.go."

// commandGo is a command file of about 2 KB that sets Long; its padding
// keeps one file per part with room left for the judgement's findings.
func commandGo(name string) string {
	var b strings.Builder
	b.WriteString("package cmd\n\nimport \"github.com/spf13/cobra\"\n\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "// %s note %02d: %s\n", name, i, strings.Repeat("-", 40))
	}
	fmt.Fprintf(&b, "func new%sCmd() *cobra.Command {\n\treturn &cobra.Command{\n\t\tUse:   %q,\n\t\tShort: \"%s entity files\",\n\t\tLong:  \"%s entity files against the SteerSpec schema.\",\n\t}\n}\n",
		title(name), name, name, name)
	return b.String()
}

// commandTestGo is the test file of commandGo(name), about as large.
func commandTestGo(name string) string {
	var b strings.Builder
	b.WriteString("package cmd\n\nimport \"testing\"\n\n")
	for i := 1; i <= 30; i++ {
		fmt.Fprintf(&b, "// %s test note %02d: %s\n", name, i, strings.Repeat("-", 35))
	}
	fmt.Fprintf(&b, "func TestNew%sCmd(t *testing.T) {\n\tif new%sCmd().Use != %q {\n\t\tt.Fatal(\"wrong use\")\n\t}\n}\n", title(name), title(name), name)
	return b.String()
}

func title(s string) string { return strings.ToUpper(s[:1]) + s[1:] }

// lineOf returns the number of the first line of content holding s.
func lineOf(content, s string) int {
	for i, l := range strings.Split(content, "\n") {
		if strings.Contains(l, s) {
			return i + 1
		}
	}
	return 0
}

// isExtractRequest and isJudgeRequest tell the two steps of an audit in
// parts by their system prompts.
func isExtractRequest(body string) bool {
	return strings.Contains(body, "reading one part of a codebase")
}
func isJudgeRequest(body string) bool { return strings.Contains(body, "judging a codebase too large") }

// shows reports whether a request shows the file at path.
func shows(body, path string) bool { return strings.Contains(body, `file path=\"`+path+`\"`) }

// asks reports whether a request carries rule id in its rules block.
func asks(body, id string) bool { return strings.Contains(body, "### "+id+`\n`) }

// findingsText renders an extract response.
func findingsText(m map[string][]prompt.Finding) string {
	for id, fs := range m {
		if fs == nil {
			m[id] = []prompt.Finding{}
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// judgement is one rule's value in a judge response.
type judgement struct {
	Verdict     string         `json:"verdict"`
	Explanation string         `json:"explanation"`
	Confidence  float64        `json:"confidence"`
	Evidence    []locationJSON `json:"evidence"`
}

// judgeText renders a judge response.
func judgeText(m map[string]judgement) string {
	for id, j := range m {
		if j.Evidence == nil {
			j.Evidence = []locationJSON{}
			m[id] = j
		}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// partsBudget sizes MaxInputTokens so that a part of an audit in parts of
// inputs holds room bytes of file slices, past the extract prompt, the
// overhead and the rules.
func partsBudget(inputs []ruleeval.RuleInput, room int) func(*claude.Config) {
	return func(c *claude.Config) {
		c.MaxInputTokens = claude.EstimateTokens(prompt.AuditExtractSystem()) + 256 +
			claude.EstimateTokens(prompt.RulesBlock(inputs)) +
			claude.EstimateTokens(strings.Repeat("x", room))
	}
}

// commandFacts is the scripted extraction of the command fixtures: a
// command file defines its command, a test file tests it.
func commandFacts(body string, files []ruleeval.File) []prompt.Finding {
	var fs []prompt.Finding
	for _, f := range files {
		if !shows(body, f.Path) {
			continue
		}
		name := strings.TrimSuffix(strings.TrimSuffix(f.Path[strings.LastIndex(f.Path, "/")+1:], ".go"), "_test")
		if strings.HasSuffix(f.Path, "_test.go") {
			l := lineOf(f.Content, "func Test")
			fs = append(fs, prompt.Finding{Kind: prompt.FindingFact, File: f.Path, Line: l, Excerpt: "func TestNew" + title(name) + "Cmd(t *testing.T) {", Note: "test of command " + name})
			continue
		}
		l := lineOf(f.Content, "func new")
		fs = append(fs, prompt.Finding{Kind: prompt.FindingFact, File: f.Path, Line: l, Excerpt: "func new" + title(name) + "Cmd() *cobra.Command {", Note: "command " + name + " defined"})
	}
	return fs
}

// testFileJudge is the scripted judgement of testFileRule: a command with
// no test among the findings breaks it.
func testFileJudge(body string, files []ruleeval.File) judgement {
	for _, f := range files {
		if strings.HasSuffix(f.Path, "_test.go") {
			continue
		}
		name := strings.TrimSuffix(f.Path[strings.LastIndex(f.Path, "/")+1:], ".go")
		if !strings.Contains(body, "test of command "+name) {
			return judgement{Verdict: "violated", Explanation: "Command " + name + " has no test file.", Confidence: 0.9,
				Evidence: []locationJSON{{File: f.Path, Line: lineOf(f.Content, "func new"), Excerpt: "func new" + title(name) + "Cmd() *cobra.Command {"}}}
		}
	}
	return judgement{Verdict: "compliant", Explanation: "Every command has a test file.", Confidence: 0.9}
}

func auditPartsScenarios() []scenario {
	cmd := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule)}
	tests := []ruleeval.RuleInput{rule("CMD-002", testFileRule)}
	both := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule), rule("CMD-002", testFileRule)}
	lint := ruleeval.File{Path: "src/cmd/lint.go", Content: commandGo("lint")}
	sync := ruleeval.File{Path: "src/cmd/sync.go", Content: commandGo("sync")}
	lintTest := ruleeval.File{Path: "src/cmd/lint_test.go", Content: commandTestGo("lint")}
	syncTest := ruleeval.File{Path: "src/cmd/sync_test.go", Content: commandTestGo("sync")}
	room := 0
	for _, f := range []ruleeval.File{lint, sync, lintTest, syncTest} {
		lines := strings.Split(strings.TrimSuffix(f.Content, "\n"), "\n")
		room = max(room, len(prompt.FileSlice(f.Path, 1, len(lines), lines))+20)
	}
	commands := []ruleeval.File{lint, sync}
	missing := []ruleeval.File{lint, sync, lintTest}
	tested := []ruleeval.File{lint, sync, lintTest, syncTest}

	// testFiles scripts an audit of files against testFileRule.
	testFiles := func(files []ruleeval.File) func(w http.ResponseWriter, body string, _ int) {
		return func(w http.ResponseWriter, body string, _ int) {
			if isJudgeRequest(body) {
				writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-002": testFileJudge(body, files)}), nil))
				return
			}
			writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-002": commandFacts(body, files)}), nil))
		}
	}
	return []scenario{
		{
			name:   "audit_two_parts",
			inputs: cmd,
			files:  commands,
			cfg:    partsBudget(cmd, room),
			// Each part notes where its command sets Long; the judgement
			// finds every command compliant.
			script: func(w http.ResponseWriter, body string, _ int) {
				if isJudgeRequest(body) {
					writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-001": {
						Verdict: "compliant", Explanation: "Both commands set Long.", Confidence: 0.95,
						Evidence: []locationJSON{{File: lint.Path, Line: lineOf(lint.Content, "Long:"), Excerpt: "Long:"}},
					}}), nil))
					return
				}
				var fs []prompt.Finding
				for _, f := range commands {
					if shows(body, f.Path) {
						fs = append(fs, prompt.Finding{Kind: prompt.FindingFact, File: f.Path, Line: lineOf(f.Content, "Long:"), Excerpt: "Long:", Note: "command sets Long"})
					}
				}
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": fs}), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				v := resp.Verdicts["CMD-001"]
				if v == nil || v.Verdict != ruleeval.Compliant || v.Explanation == "" || len(v.Evidence) != 1 || len(resp.Errors) != 0 {
					t.Fatalf("verdict = %+v, errors = %v", v, resp.Errors)
				}
				if rt.Requests() != 3 {
					t.Errorf("expected one extraction per file and one judgement, transport saw %d", rt.Requests())
				}
			},
		},
		{
			name:   "audit_split_file",
			inputs: cmd,
			files:  []ruleeval.File{{Path: "src/gen/big.go", Content: generatedGo(60)}},
			cfg:    partsBudget(cmd, 900),
			script: func(w http.ResponseWriter, body string, _ int) {
				if isJudgeRequest(body) {
					writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-001": {Verdict: "not_applicable", Explanation: "No command."}}), nil))
					return
				}
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.NotApplicable || len(resp.Errors) != 0 {
					t.Fatalf("verdict = %+v, errors = %v", v, resp.Errors)
				}
				if rt.Requests() < 3 {
					t.Errorf("a file larger than a part should be split and judged, transport saw %d requests", rt.Requests())
				}
			},
		},
		{
			name:   "audit_parts_missing_violated",
			inputs: tests,
			files:  missing,
			cfg:    partsBudget(tests, room),
			// No part sees both a command and its test: only the judgement,
			// on every part's findings, can tell that sync has none.
			script: testFiles(missing),
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				v := resp.Verdicts["CMD-002"]
				if v == nil || v.Verdict != ruleeval.Violated || len(v.Evidence) != 1 || v.Evidence[0].File != sync.Path || v.Evidence[0].Line != lineOf(sync.Content, "func new") {
					t.Fatalf("verdict = %+v, want violated at the sync command", v)
				}
				if len(resp.Errors) != 0 || rt.Requests() != 4 {
					t.Errorf("errors = %v after %d requests, want none after three extractions and a judgement", resp.Errors, rt.Requests())
				}
			},
		},
		{
			name:   "audit_parts_missing_compliant",
			inputs: tests,
			files:  tested,
			cfg:    partsBudget(tests, room),
			script: testFiles(tested),
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-002"]; v == nil || v.Verdict != ruleeval.Compliant || len(resp.Errors) != 0 || rt.Requests() != 5 {
					t.Fatalf("verdict = %+v, errors = %v after %d requests, want compliant", v, resp.Errors, rt.Requests())
				}
			},
		},
		{
			name:   "audit_extract_truncated",
			inputs: both,
			files:  commands,
			cfg:    partsBudget(both, room),
			// Both rules overflow max_tokens together, so each is asked
			// alone; CMD-002 overflows alone too and is not judged.
			script: func(w http.ResponseWriter, body string, _ int) {
				switch {
				case isJudgeRequest(body):
					writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-001": {Verdict: "compliant", Explanation: "Long is set."}}), nil))
				case asks(body, "CMD-002"):
					writeJSON(w, 200, message("max_tokens", `{"CMD-002": [`, nil))
				default:
					writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
				}
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.Compliant {
					t.Fatalf("CMD-001 = %+v, want it judged", v)
				}
				if _, ok := resp.Verdicts["CMD-002"]; ok {
					t.Error("CMD-002 has no findings and must not be judged")
				}
				if len(resp.Errors) != 2 || !strings.Contains(resp.Errors[0].Error(), "rule CMD-002 in part 1 of 2") || !strings.Contains(resp.Errors[0].Error(), "max_tokens") {
					t.Errorf("errors = %v, want CMD-002 reported in both parts", resp.Errors)
				}
				// Per part: both rules, then each alone; then one judgement.
				if rt.Requests() != 7 {
					t.Errorf("transport saw %d requests, want 7", rt.Requests())
				}
			},
		},
		{
			name:   "audit_extract_omits_rule",
			inputs: cmd,
			files:  commands,
			cfg:    partsBudget(cmd, room),
			// The second part answers for another rule than the one asked.
			script: func(w http.ResponseWriter, body string, _ int) {
				if shows(body, sync.Path) {
					writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-999": nil}), nil))
					return
				}
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if len(resp.Verdicts) != 0 || rt.Requests() != 2 {
					t.Fatalf("verdicts = %v after %d requests, want none and no judgement", resp.Verdicts, rt.Requests())
				}
				joined := fmt.Sprint(resp.Errors)
				if !strings.Contains(joined, `unrequested rule "CMD-999"`) || !strings.Contains(joined, "no findings for rule CMD-001 in part 2 of 2") {
					t.Errorf("errors = %v", resp.Errors)
				}
			},
		},
		{
			name:   "audit_findings_too_large",
			inputs: cmd,
			files:  commands,
			cfg:    partsBudget(cmd, room),
			script: func(w http.ResponseWriter, body string, _ int) {
				var fs []prompt.Finding
				for i := range 40 {
					fs = append(fs, prompt.Finding{Kind: prompt.FindingFact, File: lint.Path, Line: i + 1, Excerpt: strings.Repeat("e", 40), Note: "padding"})
				}
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": fs}), nil))
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if len(resp.Verdicts) != 0 || rt.Requests() != 2 || len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0].Error(), "findings for rule CMD-001") || !strings.Contains(resp.Errors[0].Error(), "input budget") {
					t.Fatalf("verdicts = %v, errors = %v after %d requests", resp.Verdicts, resp.Errors, rt.Requests())
				}
			},
		},
		{
			name:   "audit_judge_truncated",
			inputs: both,
			files:  commands,
			cfg:    partsBudget(both, room),
			// The judgement of both rules overflows max_tokens, so each is
			// judged alone; CMD-002 overflows alone too.
			script: func(w http.ResponseWriter, body string, _ int) {
				switch {
				case !isJudgeRequest(body):
					writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil, "CMD-002": nil}), nil))
				case asks(body, "CMD-002"):
					writeJSON(w, 200, message("max_tokens", `{"CMD-002": {`, nil))
				default:
					writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{
						"CMD-001": {Verdict: "not_applicable", Explanation: "No command."},
						"CMD-999": {Verdict: "compliant"},
					}), nil))
				}
			},
			check: func(t *testing.T, resp *ruleeval.BatchResponse, err error, _ *claude.Provider, rt *replay.Transport) {
				if err != nil {
					t.Fatal(err)
				}
				if v := resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.NotApplicable || len(resp.Verdicts) != 1 {
					t.Fatalf("verdicts = %v, want CMD-001 alone", resp.Verdicts)
				}
				joined := fmt.Sprint(resp.Errors)
				if !strings.Contains(joined, "judgement of rule CMD-002 exceeds max_tokens") || !strings.Contains(joined, `unrequested rule "CMD-999"`) {
					t.Errorf("errors = %v", resp.Errors)
				}
				if rt.Requests() != 5 {
					t.Errorf("transport saw %d requests, want two extractions and three judgements", rt.Requests())
				}
			},
		},
		{
			name:   "audit_extract_malformed",
			inputs: cmd,
			files:  commands,
			cfg:    partsBudget(cmd, room),
			script: func(w http.ResponseWriter, _ string, _ int) {
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": {{Kind: "rumour", File: lint.Path, Line: 1}}}), nil))
			},
			check: func(t *testing.T, _ *ruleeval.BatchResponse, err error, _ *claude.Provider, _ *replay.Transport) {
				if !errors.Is(err, claude.ErrMalformed) || !strings.Contains(err.Error(), `unknown kind "rumour"`) {
					t.Fatalf("err = %v, want ErrMalformed for the unknown kind", err)
				}
			},
		},
		{
			name:   "audit_judge_malformed",
			inputs: cmd,
			files:  commands,
			cfg:    partsBudget(cmd, room),
			script: func(w http.ResponseWriter, body string, _ int) {
				if isJudgeRequest(body) {
					writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-001": {Verdict: "maybe"}}), nil))
					return
				}
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
			},
			check: func(t *testing.T, _ *ruleeval.BatchResponse, err error, _ *claude.Provider, _ *replay.Transport) {
				if !errors.Is(err, claude.ErrMalformed) || !strings.Contains(err.Error(), `unknown verdict "maybe"`) {
					t.Fatalf("err = %v, want ErrMalformed for the unknown verdict", err)
				}
			},
		},
	}
}

// TestAuditParts_RequestsOfEachStep inspects the recorded bodies of an
// audit in parts: an extraction shows its own files only, numbered, with
// neither the list of the set nor the judging prompt; the judgement shows
// no code, but every path and every part's findings.
func TestAuditParts_RequestsOfEachStep(t *testing.T) {
	if os.Getenv("STRSPC_SYNTHESIZE") == "1" {
		t.Skip("cassettes are being regenerated")
	}
	var extracts, judges int
	for _, body := range cassetteBodies(t, "audit_parts_missing_violated") {
		switch {
		case isExtractRequest(body):
			extracts++
			n := 0
			for _, p := range []string{"src/cmd/lint.go", "src/cmd/sync.go", "src/cmd/lint_test.go"} {
				if shows(body, p) {
					n++
				}
			}
			if n != 1 || strings.Contains(body, "audit_scope") || strings.Contains(body, "SteerSpec rule auditor. You receive files") {
				t.Errorf("an extraction should show one file and nothing of the set: %.300s", body)
			}
		case isJudgeRequest(body):
			judges++
			for _, want := range []string{
				`- src/cmd/lint.go\n- src/cmd/sync.go\n- src/cmd/lint_test.go\n`,
				`rule id=\"CMD-002\"`, "command lint defined", "command sync defined", "test of command lint",
			} {
				if !strings.Contains(body, want) {
					t.Errorf("the judgement should carry %q", want)
				}
			}
			if strings.Contains(body, "file path=") {
				t.Error("the judgement should show no code")
			}
		default:
			t.Errorf("a request of an audit in parts is neither an extraction nor a judgement: %.200s", body)
		}
	}
	if extracts != 3 || judges != 1 {
		t.Errorf("%d extractions and %d judgements, want 3 and 1", extracts, judges)
	}

	var ranges []string
	for _, body := range cassetteBodies(t, "audit_split_file") {
		if !isExtractRequest(body) {
			continue
		}
		i := strings.Index(body, `lines=\"`)
		if i < 0 {
			t.Fatalf("a part of the split file lacks its range: %.300s", body)
		}
		rest := body[i+len(`lines=\"`):]
		ranges = append(ranges, rest[:strings.Index(rest, `\"`)])
	}
	joined := strings.Join(ranges, ",")
	if len(ranges) < 2 || !strings.Contains(joined, "1-") || !strings.Contains(joined, " of 60") {
		t.Errorf("ranges = %v, want the 60 lines split from line 1", ranges)
	}
}
