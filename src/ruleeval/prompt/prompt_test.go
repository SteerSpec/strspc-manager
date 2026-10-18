package prompt_test

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
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
	if prompt.Version != "v1" {
		t.Fatalf("Version = %q, want v1", prompt.Version)
	}
}

func TestSystem_Golden(t *testing.T) {
	s := prompt.System()
	for _, want := range []string{"not_applicable", "applies_to", "local", "@@", "exactly one verdict"} {
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

func TestResponseSchema_Golden(t *testing.T) {
	s := prompt.ResponseSchema()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	golden(t, "response_schema", string(b)+"\n")

	// Every object must be strict: additionalProperties false and all
	// properties required. Verdict is a closed enum.
	var walk func(path string, node map[string]any)
	walk = func(path string, node map[string]any) {
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
					walk(path+"."+name, pm)
				}
			}
		}
		if node["type"] == "array" {
			if items, ok := node["items"].(map[string]any); ok {
				walk(path+"[]", items)
			}
		}
	}
	walk("$", s)

	verdict := s["properties"].(map[string]any)["verdicts"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["verdict"].(map[string]any)
	enum, _ := verdict["enum"].([]string)
	if strings.Join(enum, ",") != "compliant,violated,not_applicable" {
		t.Errorf("verdict enum = %v", enum)
	}

	// Callers may mutate the returned map; each call must be independent.
	s["properties"] = nil
	if prompt.ResponseSchema()["properties"] == nil {
		t.Error("ResponseSchema must return a fresh map on every call")
	}
}
