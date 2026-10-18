// Package prompt builds the system prompt, diff and rules blocks, and the
// structured-output schema shared by all AI providers in ruleeval.
//
// Version changes whenever the wording of System, DiffBlock, RulesBlock or
// the shape of ResponseSchema changes in a way that could alter verdicts.
// It is mixed into evaluation cache keys.
package prompt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// Version identifies the prompt revision. Bump it whenever a change to this
// package could change verdicts so that cached results are invalidated.
const Version = "v1"

// noteOrder lists the note types sent to the evaluator, in the order they
// are rendered. Other note types (changelog, reference, supersedes, …) are
// bookkeeping and are omitted.
var noteOrder = []string{"applies_to", "rationale", "example", "counter_example", "clarification"}

const system = `You are the SteerSpec rule evaluator. You receive a unified diff and a list of governance rules, and you return one verdict per rule as JSON that conforms to the response schema.

How to evaluate:
- Judge only the added or changed lines of the diff: lines beginning with "+" inside a hunk. Removed lines and unchanged context are provided so you can understand the change; never report a verdict on them alone.
- For each rule choose exactly one verdict. "violated": an added or changed line breaks the rule. "compliant": the diff touches the rule's subject and respects it. "not_applicable": the diff does not touch the rule's subject at all; this is the default, so choose it whenever the rule's subject is absent from the diff rather than guessing.
- Notes refine a rule. "applies_to" notes describe where the rule applies (language, module, API surface); when the diff falls outside that scope the verdict is "not_applicable". "rationale" explains the intent, "example" shows compliance, "counter_example" shows a violation, "clarification" resolves ambiguity.
- A rule whose source has scope "local" applies only to files under that source's own directory. Files elsewhere in the diff make such a rule "not_applicable".
- Cite evidence for every "violated" verdict: the file path from the "diff --git" (or "+++") header, the line number in the new file computed from the nearest "@@ -a,b +c,d @@" hunk header, and a short excerpt of the offending line. Use an empty evidence list when there is nothing to cite.
- Set "confidence" between 0 and 1 to reflect how certain you are.
- Return exactly one verdict for every rule id listed in the rules block, in any order, and no verdict for ids that were not listed.
`

// System returns the system prompt.
func System() string { return system }

// DiffBlock wraps a unified diff in <diff> tags. The diff is normalised to
// end with exactly one newline so that identical diffs render identically.
func DiffBlock(diff string) string {
	diff = strings.TrimRight(diff, "\n")
	return "<diff>\n" + diff + "\n</diff>\n"
}

// RulesBlock renders the rules to evaluate. Each rule carries its id,
// owning entity, state, revision, source and scope, followed by its body
// and its notes grouped by type in a fixed order.
func RulesBlock(rules []ruleeval.RuleInput) string {
	var b strings.Builder
	b.WriteString("<rules>\n")
	for i, r := range rules {
		if i > 0 {
			b.WriteString("\n")
		}
		writeRule(&b, r)
	}
	b.WriteString("</rules>\n")
	return b.String()
}

func writeRule(b *strings.Builder, r ruleeval.RuleInput) {
	fmt.Fprintf(b, "### %s\n", r.Rule.ID)
	if r.EntityID != "" {
		fmt.Fprintf(b, "entity: %s\n", r.EntityID)
	}
	if r.Rule.State != "" {
		fmt.Fprintf(b, "state: %s\n", r.Rule.State)
	}
	fmt.Fprintf(b, "revision: %s\n", strconv.Itoa(r.Rule.Revision))
	if r.Source != "" {
		scope := r.Scope
		if scope == "" {
			scope = "global"
		}
		fmt.Fprintf(b, "source: %s (scope: %s)\n", r.Source, scope)
	}
	fmt.Fprintf(b, "rule: %s\n", strings.TrimSpace(r.Rule.Body))

	byType := map[string][]entity.Note{}
	for _, n := range r.Notes {
		byType[n.Type] = append(byType[n.Type], n)
	}
	for _, typ := range noteOrder {
		notes := byType[typ]
		if len(notes) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s:\n", typ)
		for _, n := range notes {
			fmt.Fprintf(b, "  - %s\n", indentContinuation(strings.TrimSpace(n.Content)))
		}
	}
}

// indentContinuation keeps multi-line note content inside its list item.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n    ")
}

// ResponseSchema returns the JSON schema for the structured response. It is
// strict: every object forbids additional properties and requires all of
// its properties, so providers can enforce it with structured outputs.
// A fresh map is returned on every call.
func ResponseSchema() map[string]any {
	evidence := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"file", "line", "excerpt"},
		"properties": map[string]any{
			"file":    map[string]any{"type": "string", "description": "Path of the file as written in the diff header."},
			"line":    map[string]any{"type": "integer", "description": "Line number in the new version of the file."},
			"excerpt": map[string]any{"type": "string", "description": "Short excerpt of the offending line."},
		},
	}
	verdict := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"rule_id", "verdict", "explanation", "confidence", "evidence"},
		"properties": map[string]any{
			"rule_id":     map[string]any{"type": "string", "description": "Rule id exactly as listed in the rules block."},
			"verdict":     map[string]any{"type": "string", "enum": []string{"compliant", "violated", "not_applicable"}},
			"explanation": map[string]any{"type": "string", "description": "One or two sentences justifying the verdict."},
			"confidence":  map[string]any{"type": "number", "description": "Certainty from 0 (guess) to 1 (certain)."},
			"evidence":    map[string]any{"type": "array", "items": evidence},
		},
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"verdicts"},
		"properties": map[string]any{
			"verdicts": map[string]any{"type": "array", "items": verdict},
		},
	}
}
