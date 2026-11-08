package prompt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// AuditVersion identifies the revision of the parts of the prompt only an
// audit uses, those of an audit in parts included (AuditExtractSystem,
// AuditJudgeSystem and their blocks and schemas). Bump it whenever a change
// to them could change verdicts.
const AuditVersion = "a2"

const auditSystem = `You are the SteerSpec rule auditor. You receive files of a codebase and a list of governance rules, and you return one verdict per rule as JSON that conforms to the response schema.

How to audit:
- Judge the code as it stands, not a change to it. The files are given inside <files>, each in a <file> block whose tag names its path. Every line of a file starts with its line number and a tab; the number is not part of the code. A rule is broken when the code shown breaks it, and also when something the rule requires is missing from the code.
- For each rule choose exactly one verdict. "violated": the code breaks the rule, or lacks what the rule requires. "compliant": the code holds the rule's subject and respects it. "not_applicable": the rule's subject is absent from the code; this is the default, so choose it whenever the rule's subject is absent rather than guessing.
- Notes refine a rule. "applies_to" notes describe where the rule applies (language, module, API surface); when no code shown falls inside that scope the verdict is "not_applicable". "rationale" explains the intent, "example" shows compliance, "counter_example" shows a violation, "clarification" resolves ambiguity.
- A rule with "extended rules" builds on the rules quoted there. Read it together with them to understand what it requires, and give a verdict for the rule itself only. An extended rule is not judged here: code that breaks only the extended rule does not violate the rule that extends it.
- A rule whose source has scope "local" applies only to files under that source's own directory. Judge it on those files alone, and answer "not_applicable" when none is shown.
- Entities can inherit rules. A rule marked "inherited by" is owned by the entity named in "entity" and applies to both that owner's subject and the inheriting entity's subject. A rule marked "overrides" replaces the named inherited rule for its own entity's subject. A rule marked "overridden by" lists the replacing rules with their entities' titles: it does not apply to those entities' subjects, and still applies to its own entity's subject everywhere else.
- The verdict response is one JSON object with one key per rule id listed in the rules block and the verdict as its value. Every listed id is present, including when every verdict is "not_applicable".
- Rule states are lifecycle codes, not activity flags: D Draft, P Published, I Implemented, R Retired, T Terminated. A state never changes whether a rule applies to the code; the consumer decides what a verdict blocks. Never treat "I" as inactive.
- Files under a "testdata", "fixtures" or "__fixtures__" directory, and files whose name or path marks them as test fixtures, are inputs for tests. A fixture that is deliberately malformed does not violate a rule about the artefact it imitates; judge it only by rules that govern test data.
- When asked to detail a rule judged "violated", re-read the code against the rule first. If the verdict does not hold on that reading, answer with the verdict you now hold ("compliant" or "not_applicable") and explain why; otherwise confirm "violated" and cite evidence: the file path from the <file> tag, the line number shown at the start of the line, and a short excerpt of the offending line without its number. When the violation is something missing, cite the place it belongs, such as the line that declares what lacks it, and say what is missing. Use an empty evidence list when there is nothing to cite, and set "confidence" between 0 and 1 to reflect how certain you are.
- When asked to detail a rule judged "compliant", re-read the code against the rule first in the same way. If the verdict does not hold on that reading, answer with the verdict you now hold: "violated", citing the offending lines as above, or "not_applicable". Otherwise confirm "compliant", explain why the code satisfies the rule, and cite the lines that satisfy it, with the file path, the line number and a short excerpt as above. When compliance rests on something forbidden being absent, say so; cite what shows it, or use an empty evidence list when nothing does. Set "confidence" as above.
`

// AuditSystem returns the system prompt of an audit.
func AuditSystem() string { return auditSystem }

// FileSlice renders lines first to first+len(lines)-1 of the file at path,
// which has total lines, as a <file> block: the tag names the path and the
// range, and each line opens with its number, right-aligned to the width of
// total, and a tab. The path comes from the code under audit, so it is
// escaped like a FileEntry. A line holding a tag cannot forge a block,
// since it starts with its number.
func FileSlice(path string, first, total int, lines []string) string {
	var b strings.Builder
	b.WriteString(FileSliceHeader(path, first, first+len(lines)-1, total))
	width := len(strconv.Itoa(total))
	for i, l := range lines {
		fmt.Fprintf(&b, "%*d\t%s\n", width, first+i, l)
	}
	b.WriteString("</file>\n")
	return b.String()
}

// FileSliceHeader is the opening tag of a FileSlice showing lines first to
// last of total. A file with no lines shows "lines 0 of 0".
func FileSliceHeader(path string, first, last, total int) string {
	lines := fmt.Sprintf("%d-%d of %d", first, last, total)
	if total == 0 {
		lines = "0 of 0"
	}
	return "<file path=" + quotePath(path) + " lines=\"" + lines + "\">\n"
}

// FileLineBytes is the size of line n of a FileSlice of a file with total
// lines, holding text.
func FileLineBytes(text string, total int) int {
	return len(strconv.Itoa(total)) + 1 + len(text) + 1
}

// quotePath renders a path as a quoted attribute value with no tag
// delimiters.
func quotePath(path string) string {
	return strings.NewReplacer("<", `\x3c`, ">", `\x3e`).Replace(strconv.Quote(path))
}

// FilesBlock wraps rendered FileSlices in <files> tags.
func FilesBlock(slices string) string {
	return "<files>\n" + slices + "</files>\n"
}

// PartialFilesBlock wraps a part of a file set too large for one request. A
// part cannot show the rest of the set, so an <audit_scope> block ahead of
// it lists files, the paths of the whole set; unlisted counts the paths left
// off that list.
//
// Deprecated: an audit too large for one request is now read in parts that
// only collect findings, shown in a FilesBlock each, and judged on them
// with AuditScope and FindingsBlock. Nothing sends this block any more.
func PartialFilesBlock(slices string, files []string, unlisted int) string {
	var b strings.Builder
	b.WriteString("<audit_scope>\nOnly part of the file set is shown below. Files of the whole set:\n")
	for _, f := range files {
		b.WriteString(FileEntry(f))
	}
	if unlisted > 0 {
		fmt.Fprintf(&b, "%d more files are not listed.\n", unlisted)
	}
	b.WriteString("</audit_scope>\n")
	b.WriteString(FilesBlock(slices))
	return b.String()
}

// AuditDetailSchema returns DetailSchema worded for an audit: evidence
// cites a file by the path its file tag names and a line by the number it
// opens with. Its descriptions hold no angle brackets, which the SDK sends
// escaped twice. The map is fresh on every call.
func AuditDetailSchema() map[string]any {
	return detailSchema("Path of the file, as its file tag names it.", "Line number shown at the start of the line.",
		"The verdict after re-reading the code: the verdict being detailed confirms it, any other value withdraws it.")
}

// AuditDetailInstruction is the user turn that follows the files and the
// single rule when asking for the details of verdict v, already given.
func AuditDetailInstruction(ruleID string, v ruleeval.Verdict) string {
	return "Rule " + ruleID + " was judged \"" + string(v) + "\" against this code. Re-read the code against the rule: confirm or withdraw that verdict, then give the explanation, your confidence and the evidence locations."
}
