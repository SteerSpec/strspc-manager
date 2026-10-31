package result

import (
	"slices"
	"strings"
)

// CodeInfo describes one diagnostic code for the people who read it. Module,
// Severity and Summary repeat the code's row in the Diagnostic Codes section
// of docs/SPEC.md; Title and Fix exist only here. The JSON form is published
// as docs/diagnostic-codes.json.
type CodeInfo struct {
	// Code is the diagnostic code: "RL001", "RD003", etc.
	Code string `json:"code"`
	// Module is the module that emits the code, spelled as in
	// Diagnostic.Module: "rule-lint", "rule-diff", etc.
	Module string `json:"module"`
	// Severity is the spec's text. It is not a Severity value because a code
	// can have two, as in "Info / Error".
	Severity string `json:"severity"`
	// Title names the problem in a few words.
	Title string `json:"title"`
	// Summary is the spec's "When it fires" cell, Markdown included.
	Summary string `json:"summary"`
	// Fix tells the reader how to resolve the finding.
	Fix string `json:"fix"`
	// Retired reports that the check was removed and the code stays reserved.
	Retired bool `json:"retired"`
}

// Codes returns every diagnostic code, sorted by code. The slice is a copy,
// so callers may modify it.
func Codes() []CodeInfo {
	codes := slices.Clone(codeTable)
	slices.SortFunc(codes, func(a, b CodeInfo) int {
		return strings.Compare(a.Code, b.Code)
	})
	return codes
}

// LookupCode returns the entry for code. The match is exact: "RL001", not
// "rl001".
func LookupCode(code string) (CodeInfo, bool) {
	for _, info := range codeTable {
		if info.Code == code {
			return info, true
		}
	}
	return CodeInfo{}, false
}
