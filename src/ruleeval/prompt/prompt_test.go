package prompt_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

var update = flag.Bool("update", false, "rewrite golden files under testdata/")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with -update to create)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s mismatch with golden file; run `go test ./src/ruleeval/prompt -update` after reviewing.\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func sampleRules() []ruleeval.RuleInput {
	return []ruleeval.RuleInput{
		{
			Rule:     entity.Rule{ID: "CMD-001", Revision: 2, State: "I", Body: "Every cobra command MUST set a Long description."},
			EntityID: "CMD",
			Source:   "./rules/",
			Scope:    "local",
			Notes: []entity.Note{
				{ID: "CMD-001/03", RuleRef: "CMD-001", Type: "changelog", Content: "r2: tightened wording"},
				{ID: "CMD-001/01", RuleRef: "CMD-001", Type: "rationale", Content: "Long descriptions feed --help."},
				{ID: "CMD-001/02", RuleRef: "CMD-001", Type: "applies_to", Content: "Go files under src/cmd/ that call cobra.Command."},
				{ID: "CMD-001/04", RuleRef: "CMD-001", Type: "example", Content: "Long: \"Validate entity files against the schema.\""},
				{ID: "CMD-001/05", RuleRef: "CMD-001", Type: "counter_example", Content: "&cobra.Command{Use: \"lint\", Short: \"Lint\"}"},
				{ID: "CMD-001/06", RuleRef: "CMD-001", Type: "clarification", Content: "Hidden commands are exempt."},
				{ID: "CMD-001/07", RuleRef: "CMD-001", Type: "rationale", Content: "Second rationale note."},
			},
		},
		{
			Rule:     entity.Rule{ID: "ERR-002", Revision: 1, State: "P", Body: "Errors returned to callers MUST be wrapped with context."},
			EntityID: "ERR",
			Source:   "github://SteerSpec/strspc-rules@v1.6.0/rules/core",
			Scope:    "global",
		},
		{
			Rule: entity.Rule{ID: "BARE-001", Revision: 1, State: "D", Body: "A rule without entity or source metadata."},
		},
	}
}

var sampleDiff = strings.Join([]string{
	"diff --git a/src/cmd/lint.go b/src/cmd/lint.go",
	"index 1111111..2222222 100644",
	"--- a/src/cmd/lint.go",
	"+++ b/src/cmd/lint.go",
	"@@ -10,6 +10,9 @@ func newLintCmd() *cobra.Command {",
	" \tcmd := &cobra.Command{",
	" \t\tUse:   \"lint\",",
	" \t\tShort: \"Lint entity files\",",
	"+\t\tRunE: func(cmd *cobra.Command, args []string) error {",
	"+\t\t\treturn run(args)",
	"+\t\t},",
	" \t}",
	" \treturn cmd",
	" }",
}, "\n")

func TestVersion(t *testing.T) {
	if prompt.Version != "v7" {
		t.Fatalf("Version = %q, want v7", prompt.Version)
	}
}

func TestSystem_Golden(t *testing.T) {
	s := prompt.System()
	for _, want := range []string{"not_applicable", "applies_to", "local", "@@", "exactly one verdict",
		// v2: regressions caused by removals count, pre-existing problems do not,
		// and a removed line is cited at the nearest surviving new-file line.
		`beginning with "-"`, "removal", "pre-existing", "nearest surviving line",
		// v7: a part of a split diff cannot prove that a listed file lacks a change.
		"<diff_scope>", "listed file", "more files are not listed"} {
		if !strings.Contains(s, want) {
			t.Errorf("system prompt should mention %q", want)
		}
	}
	golden(t, "system", s)
}

func TestDiffBlock_Golden(t *testing.T) {
	got := prompt.DiffBlock(sampleDiff)
	if !strings.HasPrefix(got, "<diff>\n") || !strings.HasSuffix(got, "\n</diff>\n") {
		t.Errorf("diff block must be wrapped in <diff> tags on their own lines, got %q", got)
	}
	if prompt.DiffBlock(sampleDiff+"\n") != got {
		t.Error("a trailing newline in the diff must not change the block")
	}
	golden(t, "diff_block", got)
}

func TestPartialDiffBlock(t *testing.T) {
	got := prompt.PartialDiffBlock(sampleDiff, []string{"docs/SPEC.md", "src/cmd/lint.go"}, 0)
	golden(t, "partial_diff_block", got)
	if !strings.HasSuffix(got, prompt.DiffBlock(sampleDiff)) {
		t.Error("the part must render exactly as a complete diff does")
	}
	if strings.Contains(got, "more files") {
		t.Error("a complete list must not claim that files are missing from it")
	}
	if got := prompt.PartialDiffBlock(sampleDiff, []string{"docs/SPEC.md"}, 3); !strings.Contains(got, "- docs/SPEC.md\n3 more files are not listed.\n</diff_scope>") {
		t.Errorf("a capped list must say so, got %q", got)
	}
}

func TestRulesBlock_Golden(t *testing.T) {
	got := prompt.RulesBlock(sampleRules())
	if strings.Contains(got, "changelog") || strings.Contains(got, "tightened") {
		t.Error("changelog notes must not be sent to the evaluator")
	}
	order := []string{"applies_to", "rationale", "example", "counter_example", "clarification"}
	last := -1
	for _, typ := range order {
		i := strings.Index(got, typ+":")
		if i < 0 {
			t.Errorf("missing note group %q", typ)
			continue
		}
		if i < last {
			t.Errorf("note group %q out of order", typ)
		}
		last = i
	}
	if strings.Count(got, "### ") != 3 {
		t.Errorf("expected 3 rule headings, got %d", strings.Count(got, "### "))
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.TrimRight(line, " \t") != line {
			t.Errorf("trailing whitespace in line %q", line)
		}
	}
	golden(t, "rules_block", got)
}

func TestRulesBlock_Empty(t *testing.T) {
	got := prompt.RulesBlock(nil)
	if !strings.HasPrefix(got, "<rules>") || !strings.HasSuffix(got, "</rules>\n") {
		t.Errorf("empty rules block should still be wrapped, got %q", got)
	}
}

func TestVerdictSchema_Golden(t *testing.T) {
	s := prompt.VerdictSchema([]string{"CMD-001", "ERR-002"})
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "verdict_schema", string(b)+"\n")
	assertStrict(t, "$", s)

	req, _ := s["required"].([]string)
	if strings.Join(req, ",") != "CMD-001,ERR-002" {
		t.Errorf("required = %v, want every rule id", req)
	}
	v := s["properties"].(map[string]any)["CMD-001"].(map[string]any)
	enum, _ := v["enum"].([]string)
	if strings.Join(enum, ",") != "compliant,violated,not_applicable" {
		t.Errorf("verdict enum = %v", enum)
	}

	// Callers may mutate the returned map; each call must be independent.
	s["properties"] = nil
	if prompt.VerdictSchema([]string{"CMD-001"})["properties"] == nil {
		t.Error("VerdictSchema must return a fresh map on every call")
	}
}

func TestDetailSchema_Golden(t *testing.T) {
	s := prompt.DetailSchema()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "detail_schema", string(b)+"\n")
	assertStrict(t, "$", s)

	s["properties"] = nil
	if prompt.DetailSchema()["properties"] == nil {
		t.Error("DetailSchema must return a fresh map on every call")
	}
}

func TestDetailInstruction(t *testing.T) {
	got := prompt.DetailInstruction("CMD-001")
	if !strings.Contains(got, "CMD-001") || !strings.Contains(got, "violated") {
		t.Errorf("instruction should name the rule and the verdict: %q", got)
	}
}

// assertStrict checks that every object in a schema is strict:
// additionalProperties false and all properties required.
func assertStrict(t *testing.T, path string, node map[string]any) {
	t.Helper()
	if node["type"] == "object" {
		if ap, ok := node["additionalProperties"].(bool); !ok || ap {
			t.Errorf("%s: additionalProperties must be false", path)
		}
		props, _ := node["properties"].(map[string]any)
		req, _ := node["required"].([]string)
		if len(req) != len(props) {
			t.Errorf("%s: required (%d) must list every property (%d)", path, len(req), len(props))
		}
		for name, p := range props {
			if pm, ok := p.(map[string]any); ok {
				assertStrict(t, path+"."+name, pm)
			}
		}
	}
	if node["type"] == "array" {
		if items, ok := node["items"].(map[string]any); ok {
			assertStrict(t, path+"[]", items)
		}
	}
}

func TestRulesBlock_ExtendedRules(t *testing.T) {
	got := prompt.RulesBlock([]ruleeval.RuleInput{{
		Rule:     entity.Rule{ID: "SAV-001", State: "I", Body: "Savings MUST cap."},
		EntityID: "SAV",
		Notes:    []entity.Note{{ID: "SAV-001/01", RuleRef: "SAV-001", Type: "extends", Content: "core:ACCT-002"}},
		Extended: []inherit.Extended{{ID: "core:ACCT-002", Revision: 1, Body: "Accounts MUST log.\nEvery change."}},
	}})
	want := "extends:\n  - core:ACCT-002\nextended rules:\n  - core:ACCT-002: Accounts MUST log.\n    Every change.\n"
	if !strings.Contains(got, want) {
		t.Errorf("rules block missing %q in:\n%s", want, got)
	}
}

func TestRulesBlock_InheritanceLines(t *testing.T) {
	got := prompt.RulesBlock([]ruleeval.RuleInput{
		{Rule: entity.Rule{ID: "ACCT-002", State: "I", Body: "Accounts MUST log."}, EntityID: "SAV", Owner: "core:ACCT", InheritedBy: "SAV"},
		{Rule: entity.Rule{ID: "SAV-001", State: "D", Body: "Savings MUST cap."}, EntityID: "SAV", Overrides: "core:ACCT-001"},
		{Rule: entity.Rule{ID: "ACCT-001", State: "I", Body: "Accounts MUST pay."}, EntityID: "ACCT", OverriddenBy: []string{"prod:LOAN-001 (Loans)", "prod:SAV-001 (Savings)"}},
	})
	for _, want := range []string{
		"### ACCT-002\nentity: core:ACCT\nstate: I\nrevision: 0\ninherited by: SAV\n",
		"### SAV-001\nentity: SAV\nstate: D\nrevision: 0\noverrides: core:ACCT-001\n",
		"### ACCT-001\nentity: ACCT\nstate: I\nrevision: 0\noverridden by: prod:LOAN-001 (Loans), prod:SAV-001 (Savings)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("rules block missing %q in:\n%s", want, got)
		}
	}
}
