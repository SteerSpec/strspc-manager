package prompt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// An audit too large for one request is read in parts and judged on what
// they found, a map-reduce: each part only collects, per rule, the findings
// its files hold (AuditExtractSystem, ExtractSchema); one request per group
// of rules then judges every rule on the findings of all parts together
// (AuditJudgeSystem, AuditScope, FindingsBlock, JudgeSchema). No part ever
// judges a rule, so something a rule requires that no part found is missing
// from the whole set, as in an audit that fits one request.

const auditExtractSystem = `You are the SteerSpec rule auditor, reading one part of a codebase too large to read at once. You receive some of its files and a list of governance rules, and for each rule you list the findings in these files that bear on it, as JSON that conforms to the response schema. A later step judges each rule on the findings of every part together, without the code, so the findings must carry everything that judgement needs.

How to collect findings:
- The files are given inside <files>, each in a <file> block whose tag names its path, the range of lines shown and the file's line count. Every line starts with its line number and a tab; the number is not part of the code. The other files of the codebase are read in other parts, and a large file may be shown in several parts.
- Do not judge the rules, and never conclude that something is missing: what these files lack may sit in another part. List what is here.
- A finding of kind "violation" is code shown here that breaks the rule on its own, whatever the rest of the codebase holds.
- A finding of kind "fact" is something shown here that judging the rule across the whole codebase needs: what the code defines, declares, registers, calls, tests or documents that the rule requires, forbids or relates. For a rule that something must hold for every X, list every X shown here, and every item shown here that could meet the requirement of an X, wherever that X is defined.
- Each finding cites the file path from the <file> tag, the line number shown at the start of the line, a short excerpt of that line without its number, and a note of a few words saying what it is, such as "command lint defined" or "test of command lint". List each thing once and keep notes short. When nothing here bears on a rule, give it an empty list.
- The response is one JSON object with one key per rule id listed in the rules block and the list of findings as its value. Every listed id is present, including when every list is empty.
- Notes refine a rule. "applies_to" notes describe where the rule applies (language, module, API surface); collect findings only from code inside that scope. "rationale" explains the intent, "example" shows compliance, "counter_example" shows a violation, "clarification" resolves ambiguity.
- A rule with "extended rules" builds on the rules quoted there. Read it together with them to understand what it requires, and collect findings for the rule itself only: code that breaks only an extended rule is not a violation of the rule that extends it.
- A rule whose source has scope "local" applies only to files under that source's own directory. Collect its findings from those files alone.
- Entities can inherit rules. A rule marked "inherited by" is owned by the entity named in "entity" and applies to both that owner's subject and the inheriting entity's subject. A rule marked "overrides" replaces the named inherited rule for its own entity's subject. A rule marked "overridden by" lists the replacing rules with their entities' titles: it does not apply to those entities' subjects, and still applies to its own entity's subject everywhere else.
- Rule states are lifecycle codes, not activity flags: D Draft, P Published, I Implemented, R Retired, T Terminated. A state never changes whether a rule applies to the code. Never treat "I" as inactive.
- Files under a "testdata", "fixtures" or "__fixtures__" directory, and files whose name or path marks them as test fixtures, are inputs for tests. A fixture that is deliberately malformed does not violate a rule about the artefact it imitates; collect violations from it only for rules that govern test data.
`

const auditJudgeSystem = `You are the SteerSpec rule auditor, judging a codebase too large to read at once. Each part of it was read on its own, and the findings bearing on each rule were collected. You receive the paths of every file of the codebase, those findings and a list of governance rules, and you return one judgement per rule as JSON that conforms to the response schema.

How to judge:
- Judge the code as it stands, not a change to it. The paths of every file are listed inside <audit_scope>: a file it does not name is not in the codebase, unless the list says that more files are not listed: then a file it does not name may still exist. The list may also name lines, or whole files, that no part was shown, being too long to send. The findings are given inside <findings>, in one <rule> block per rule whose tag names the rule id, one JSON object per line with its kind, file, line, excerpt and note. A rule block with no lines means that no part found anything bearing on the rule.
- The findings stand for the whole code: every part was asked for everything each rule needs. Something a rule requires that neither the findings nor the file paths show is missing, and missing breaks the rule, unless it could sit on a line or in a file that no part was shown: nothing is known of what those hold, so nothing is missing for want of a finding there. A finding of kind "violation" is code that a part judged to break the rule on its own; weigh it against the rule and the other findings rather than taking it as given. A finding of kind "fact" is something the rule needs judged across files, such as what is defined and what meets a requirement.
- For each rule choose exactly one verdict. "violated": the code breaks the rule, or lacks what the rule requires. "compliant": the code holds the rule's subject and respects it. "not_applicable": the rule's subject is absent from the code; this is the default, so choose it whenever the rule's subject is absent rather than guessing.
- Notes refine a rule. "applies_to" notes describe where the rule applies (language, module, API surface); when no finding falls inside that scope the verdict is "not_applicable". "rationale" explains the intent, "example" shows compliance, "counter_example" shows a violation, "clarification" resolves ambiguity.
- A rule with "extended rules" builds on the rules quoted there. Read it together with them to understand what it requires, and give a verdict for the rule itself only. An extended rule is not judged here: code that breaks only the extended rule does not violate the rule that extends it.
- A rule whose source has scope "local" applies only to files under that source's own directory. Judge it on the findings and paths of those files alone, and answer "not_applicable" when there are none.
- Entities can inherit rules. A rule marked "inherited by" is owned by the entity named in "entity" and applies to both that owner's subject and the inheriting entity's subject. A rule marked "overrides" replaces the named inherited rule for its own entity's subject. A rule marked "overridden by" lists the replacing rules with their entities' titles: it does not apply to those entities' subjects, and still applies to its own entity's subject everywhere else.
- Rule states are lifecycle codes, not activity flags: D Draft, P Published, I Implemented, R Retired, T Terminated. A state never changes whether a rule applies to the code; the consumer decides what a verdict blocks. Never treat "I" as inactive.
- Files under a "testdata", "fixtures" or "__fixtures__" directory, and files whose name or path marks them as test fixtures, are inputs for tests. A fixture that is deliberately malformed does not violate a rule about the artefact it imitates; judge it only by rules that govern test data.
- For each rule give the verdict, one or two sentences explaining it, a confidence between 0 and 1, and the evidence: the findings that support the verdict, each cited by its file, line and excerpt. When the violation is something missing, cite the place it belongs, such as the finding that declares what lacks it, and say what is missing. When compliance rests on something forbidden being absent, say so. Use an empty evidence list when no finding supports the verdict.
- The response is one JSON object with one key per rule id listed in the rules block and the judgement as its value. Every listed id is present, including when every verdict is "not_applicable".
`

// AuditExtractSystem returns the system prompt of a part of an audit too
// large for one request: it collects findings and judges nothing.
func AuditExtractSystem() string { return auditExtractSystem }

// AuditJudgeSystem returns the system prompt that judges the rules of an
// audit too large for one request on the findings of all its parts.
func AuditJudgeSystem() string { return auditJudgeSystem }

// The kinds of a Finding.
const (
	// FindingViolation is code that breaks the rule on its own.
	FindingViolation = "violation"
	// FindingFact is something judging the rule across files needs.
	FindingFact = "fact"
)

// Finding is what a part of an audit found bearing on a rule, in the shape
// ExtractSchema asks for.
type Finding struct {
	// Kind is FindingViolation or FindingFact.
	Kind string `json:"kind"`
	// File is the path of the file, as its file tag names it.
	File string `json:"file"`
	// Line is the line number shown at the start of the line.
	Line int `json:"line"`
	// Excerpt is a short quote of the line, without its number.
	Excerpt string `json:"excerpt"`
	// Note says in a few words what the finding is.
	Note string `json:"note"`
}

// Unread is a line of an audited file that no part shows, being too long
// to send. Line 0 stands for the whole file, whose path alone is too long.
type Unread struct {
	// File is the path of the file.
	File string
	// Line is the line number, from 1, or 0 for the whole file.
	Line int
}

// AuditScope lists files, the paths of a whole file set, for the judgement
// of an audit in parts; unlisted counts the paths left off the list, and
// unread lists what no part shows, so that the judgement knows nothing was
// found there because nothing was read.
func AuditScope(files []string, unlisted int, unread []Unread) string {
	var b strings.Builder
	b.WriteString("<audit_scope>\nFiles of the codebase:\n")
	for _, f := range files {
		b.WriteString(FileEntry(f))
	}
	if unlisted > 0 {
		fmt.Fprintf(&b, "%d more files are not listed.\n", unlisted)
	}
	if len(unread) > 0 {
		b.WriteString("Not shown to any part, being too long to send:\n")
		for _, u := range unread {
			path := strings.TrimPrefix(FileEntry(u.File), "- ")
			if u.Line == 0 {
				b.WriteString("- the whole file " + path)
			} else {
				fmt.Fprintf(&b, "- line %d of %s", u.Line, path)
			}
		}
	}
	b.WriteString("</audit_scope>\n")
	return b.String()
}

// FindingsBlock renders the findings of each rule in ids, in that order, as
// a <rule> block of one JSON object per line. JSON escapes angle brackets,
// so an excerpt of the audited code cannot close or forge a block.
func FindingsBlock(ids []string, findings map[string][]Finding) string {
	var b strings.Builder
	b.WriteString("<findings>\n")
	for _, id := range ids {
		b.WriteString(RuleFindings(id, findings[id]))
	}
	b.WriteString("</findings>\n")
	return b.String()
}

// RuleFindings is the <rule> block of one rule in a FindingsBlock, so that
// a caller can size the block of each rule apart.
func RuleFindings(id string, findings []Finding) string {
	var b strings.Builder
	b.WriteString("<rule id=" + quotePath(id) + ">\n")
	for _, f := range findings {
		line, _ := json.Marshal(f) // a struct of strings and an int always marshals
		b.Write(line)
		b.WriteByte('\n')
	}
	b.WriteString("</rule>\n")
	return b.String()
}

// ExtractSchema returns the JSON schema of the findings of one part: an
// object keyed by rule id whose values list findings. The map is fresh on
// every call.
func ExtractSchema(ids []string) map[string]any {
	finding := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             []string{"kind", "file", "line", "excerpt", "note"},
		"properties": map[string]any{
			"kind":    map[string]any{"type": "string", "enum": []string{FindingViolation, FindingFact}},
			"file":    map[string]any{"type": "string", "description": "Path of the file, as its file tag names it."},
			"line":    map[string]any{"type": "integer", "description": "Line number shown at the start of the line."},
			"excerpt": map[string]any{"type": "string", "description": "Short excerpt of the line cited, without its number."},
			"note":    map[string]any{"type": "string", "description": "A few words saying what the finding is."},
		},
	}
	props := make(map[string]any, len(ids))
	for _, id := range ids {
		props[id] = map[string]any{"type": "array", "items": finding}
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             append([]string(nil), ids...),
		"properties":           props,
	}
}

// JudgeSchema returns the JSON schema of the judgement of an audit in
// parts: an object keyed by rule id whose values hold the verdict,
// explanation, confidence and evidence, as DetailSchema does for one rule.
// The map is fresh on every call.
func JudgeSchema(ids []string) map[string]any {
	props := make(map[string]any, len(ids))
	for _, id := range ids {
		props[id] = detailSchema("Path of the file, as the finding names it.", "Line number, as the finding gives it.",
			"The verdict on the rule across the whole codebase.")
	}
	return map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"required":             append([]string(nil), ids...),
		"properties":           props,
	}
}
