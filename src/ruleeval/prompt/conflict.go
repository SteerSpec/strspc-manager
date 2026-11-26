package prompt

import (
	"fmt"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// ConflictVersion identifies the conflict prompt revision. Bump it whenever
// ConflictSystem, ConflictBlock or ConflictSchema changes, so that a cached
// answer is never reused across revisions.
const ConflictVersion = "c1"

const conflictSystem = `You are the SteerSpec override reviewer. You receive two governance rules: a Draft rule of an entity, and the inherited rule it proposes to override. You return one JSON object that conforms to the response schema.

How to judge:
- The two rules conflict when code that follows the Draft rule would violate the inherited rule. The Draft does not replace the inherited rule until it is Published, so in the meantime such code is flagged against the inherited rule.
- A Draft that narrows, tightens or restates the inherited rule, so that code following it also follows the inherited rule, does not conflict.
- A Draft that loosens the inherited rule, allows what it forbids, or requires what it forbids conflicts.
- Judge the texts as written, with their notes. Do not guess at code you have not seen.
- Set "conflicts" to true or false, and explain in one or two sentences, naming what the Draft allows or requires that the inherited rule does not.`

// ConflictSystem returns the system prompt of a conflict check.
func ConflictSystem() string { return conflictSystem }

// ConflictBlock renders the Draft override, with the inherited rule quoted
// under "overridden rule" (see RulesBlock).
func ConflictBlock(in ruleeval.RuleInput) string {
	var b strings.Builder
	b.WriteString("<rules>\n")
	writeRule(&b, in)
	b.WriteString("</rules>\n")
	return b.String()
}

// ConflictInstruction is the user turn that follows the rules block.
func ConflictInstruction(in ruleeval.RuleInput) string {
	return fmt.Sprintf("Does code that follows the Draft rule %s violate %s, the rule it proposes to override?", in.Rule.ID, in.Overridden.ID)
}

// ConflictSchema is the structured-output schema of a conflict check.
func ConflictSchema() map[string]any {
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"conflicts", "explanation"},
		"properties": map[string]any{
			"conflicts":   map[string]any{"type": "boolean", "description": "True when code that follows the Draft rule would violate the overridden rule."},
			"explanation": map[string]any{"type": "string", "description": "One or two sentences justifying the answer."},
		},
	}
}
