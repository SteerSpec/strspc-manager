// Package prompt builds the system prompt, diff and rules blocks, and the
// structured-output schema shared by all AI providers in ruleeval.
//
// Version changes whenever the wording of System, DiffBlock, PartialDiffBlock,
// RulesBlock or the shape of VerdictSchema or DetailSchema changes in a way
// that could alter verdicts.
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
const Version = "v7"

const system = `You are the SteerSpec rule evaluator. You receive a unified diff and a list of governance rules, and you return one verdict per rule as JSON that conforms to the response schema.

How to evaluate:
- Judge the change itself: the lines beginning with "+" (added or rewritten) and the lines beginning with "-" (removed) inside each hunk. A removal that breaks a rule is a violation just as an addition is: deleting a required element from code that remains is a regression introduced by this diff. Unchanged context lines are provided so you can understand the change; a pre-existing problem that the diff does not touch is never a violation of this diff.
- A diff too large for one request is split, and each part is judged on its own. A part opens with a <diff_scope> block listing every file the whole diff changes. A listed file changed even when no hunk of it is shown here, and a file that is shown may be shown only in part. Never answer "violated" because something is missing from what you can see when it could sit in a listed file: if a rule needs a change in a file the list names, this part cannot tell that the change is missing, so answer "compliant" or "not_applicable". A file the list does not name really is unchanged and its absence can be a violation, unless the list ends by saying that more files are not listed. Anything the hunks shown here break is judged as usual.
- For each rule choose exactly one verdict. "violated": an added or changed line breaks the rule. "compliant": the diff touches the rule's subject and respects it. "not_applicable": the diff does not touch the rule's subject at all; this is the default, so choose it whenever the rule's subject is absent from the diff rather than guessing.
- Notes refine a rule. "applies_to" notes describe where the rule applies (language, module, API surface); when the diff falls outside that scope the verdict is "not_applicable". "rationale" explains the intent, "example" shows compliance, "counter_example" shows a violation, "clarification" resolves ambiguity.
- A rule with "extended rules" builds on the rules quoted there. Read it together with them to understand what it requires, and give a verdict for the rule itself only. An extended rule is not judged here: a change that breaks only the extended rule does not violate the rule that extends it.
- A rule whose source has scope "local" applies only to files under that source's own directory. Files elsewhere in the diff make such a rule "not_applicable".
- Entities can inherit rules. A rule marked "inherited by" is owned by the entity named in "entity" and applies to both that owner's subject and the inheriting entity's subject. A rule marked "overrides" replaces the named inherited rule for its own entity's subject. A rule marked "overridden by" lists the replacing rules with their entities' titles: it does not apply to those entities' subjects, and still applies to its own entity's subject everywhere else.
- The verdict response is one JSON object with one key per rule id listed in the rules block and the verdict as its value. Every listed id is present, including when every verdict is "not_applicable".
- Rule states are lifecycle codes, not activity flags: D Draft, P Published, I Implemented, R Retired, T Terminated. A state never changes whether a rule applies to the diff; the consumer decides which states block a merge. Never treat "I" as inactive.
- Files under a "testdata", "fixtures" or "__fixtures__" directory, and files whose name or path marks them as test fixtures, are inputs for tests. A fixture that is deliberately malformed does not violate a rule about the artefact it imitates; judge it only by rules that govern test data.
- When asked to detail a rule judged "violated", re-read the diff against the rule first. If the verdict does not hold on that reading, answer with the verdict you now hold ("compliant" or "not_applicable") and explain why; otherwise confirm "violated" and cite evidence: the file path from the "diff --git" (or "+++") header, the line number in the new file computed from the nearest "@@ -a,b +c,d @@" hunk header, and a short excerpt of the offending line. When the violation is a removal, the removed line has no new-file line number: cite the line in the new file where the removed content used to be (the nearest surviving line in that hunk, or the hunk's starting line "c" when nothing survives) and quote the removed line as the excerpt. Use an empty evidence list when there is nothing to cite, and set "confidence" between 0 and 1 to reflect how certain you are.
`

// System returns the system prompt.
func System() string { return system }

// DiffBlock wraps a unified diff in <diff> tags. The diff is normalised to
// end with exactly one newline so that identical diffs render identically.
func DiffBlock(diff string) string {
	diff = strings.TrimRight(diff, "\n")
	return "<diff>\n" + diff + "\n</diff>\n"
}

// PartialDiffBlock wraps a part of a diff too large for one request. A part
// cannot show what changed elsewhere, so a <diff_scope> block ahead of it
// lists files, the paths the whole diff changes; unlisted counts the paths
// left off that list. Paths come from the diff under review, so they are
// escaped: a crafted file name must not forge a list entry or end the block.
func PartialDiffBlock(chunk string, files []string, unlisted int) string {
	var b strings.Builder
	b.WriteString("<diff_scope>\nOnly part of the diff is shown below. Files changed by the whole diff:\n")
	for _, f := range files {
		b.WriteString(FileEntry(f))
	}
	if unlisted > 0 {
		fmt.Fprintf(&b, "%d more files are not listed.\n", unlisted)
	}
	b.WriteString("</diff_scope>\n")
	b.WriteString(DiffBlock(chunk))
	return b.String()
}

// FileEntry renders one line of the PartialDiffBlock file list: the path on
// a single line with no tag delimiters. An ordinary path is listed unchanged.
func FileEntry(path string) string {
	q := strconv.Quote(path)
	return "- " + strings.NewReplacer("<", `\x3c`, ">", `\x3e`).Replace(q[1:len(q)-1]) + "\n"
}

// RulesBlock renders the rules to evaluate. Each rule carries its id,
// owning entity, state, revision, source and scope, and for inheritance the
// inheriting entity, the rule it overrides and the rules overriding it,
// followed by its body, its notes grouped by type in a fixed order, and the
// rules it extends.
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
	switch {
	case r.Owner != "":
		fmt.Fprintf(b, "entity: %s\n", r.Owner)
	case r.EntityID != "":
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
	if r.InheritedBy != "" {
		fmt.Fprintf(b, "inherited by: %s\n", r.InheritedBy)
	}
	if r.Overrides != "" {
		fmt.Fprintf(b, "overrides: %s\n", r.Overrides)
	}
	if len(r.OverriddenBy) > 0 {
		fmt.Fprintf(b, "overridden by: %s\n", strings.Join(r.OverriddenBy, ", "))
	}
	fmt.Fprintf(b, "rule: %s\n", strings.TrimSpace(r.Rule.Body))

	byType := map[string][]entity.Note{}
	for _, n := range r.Notes {
		byType[n.Type] = append(byType[n.Type], n)
	}
	for _, typ := range ruleeval.PromptNoteTypes {
		notes := byType[typ]
		if len(notes) == 0 {
			continue
		}
		fmt.Fprintf(b, "%s:\n", typ)
		for _, n := range notes {
			fmt.Fprintf(b, "  - %s\n", indentContinuation(strings.TrimSpace(n.Content)))
		}
	}
	if len(r.Extended) > 0 {
		b.WriteString("extended rules:\n")
		for _, e := range r.Extended {
			fmt.Fprintf(b, "  - %s: %s\n", e.ID, indentContinuation(strings.TrimSpace(e.Body)))
		}
	}
}

// indentContinuation keeps multi-line note content inside its list item.
func indentContinuation(s string) string {
	return strings.ReplaceAll(s, "\n", "\n    ")
}

// Verdicts are the closed set of verdict values the schemas allow.
var Verdicts = []string{"compliant", "violated", "not_applicable"}

// VerdictSchema returns the JSON schema of the verdict response for ids: one
// required property per rule id whose value is a verdict. Keying the object
// by rule id lets the output grammar guarantee one verdict per listed rule,
// which an array of {rule_id, verdict} objects cannot (the model would
// sometimes stop after a single placeholder element). The grammar stays
// small enough for forty rules because each value is a bare enum; details
// for violated rules are requested separately (see DetailSchema). The map is
// fresh on every call.
func VerdictSchema(ids []string) map[string]any {
	props := make(map[string]any, len(ids))
	for _, id := range ids {
		props[id] = map[string]any{"type": "string", "enum": append([]string(nil), Verdicts...)}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             append([]string(nil), ids...),
		"properties":           props,
	}
}

// DetailSchema returns the JSON schema of the detail response for one
// violated rule: explanation, confidence and evidence locations. The map is
// fresh on every call.
func DetailSchema() map[string]any {
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
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"verdict", "explanation", "confidence", "evidence"},
		"properties": map[string]any{
			"verdict":     map[string]any{"type": "string", "enum": []string{"violated", "compliant", "not_applicable"}, "description": "The verdict after re-reading the diff: \"violated\" confirms it, any other value withdraws it."},
			"explanation": map[string]any{"type": "string", "description": "One or two sentences justifying the verdict."},
			"confidence":  map[string]any{"type": "number", "description": "Certainty from 0 (guess) to 1 (certain)."},
			"evidence":    map[string]any{"type": "array", "items": evidence},
		},
	}
}

// DetailInstruction is the user turn that follows the diff and the single
// rule when asking for the details of a verdict already given.
func DetailInstruction(ruleID string) string {
	return "Rule " + ruleID + " was judged \"violated\" against this diff. Re-read the diff against the rule: confirm or withdraw that verdict, then give the explanation, your confidence and the evidence locations."
}
