package qa_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// exception is a case the suite leaves out, with the reason. Keep the table
// minimal: a Go case in cases_test.go is better than a row here.
type exception struct {
	code   string
	reason string
	// bug marks a case whose current behaviour contradicts the spec, so no
	// expected.json may record it. The code may still fire elsewhere. Delete
	// the row and add the case when the bug is fixed.
	bug bool
}

// exceptions lists what no scenario and no Go case asserts. Every code of the
// registry has a scenario or a Go case, so today it holds only engine bugs.
var exceptions = []exception{
	{
		code: "RD007",
		bug:  true,
		reason: "rulediff.CompareDir also compares files that did not change, so an identical file in base and head " +
			"draws RD007 and RD009, and so does a realm.json present on both sides, where SPEC §7.2 validates " +
			"\"each changed entity file\". The scenarios keep every file of base/ and head/ changed.",
	},
	{
		code: "RD008",
		bug:  true,
		reason: "rulediff.CompareNew skips the hash check, so a new file with a wrong rule_set.hash draws no RD008, " +
			"though §6.3 says the code fires when the hash differs from that of the head file and §7.2 item 8 " +
			"makes no exception. head/ of diff-rule-set holds no new file with a bad hash.",
	},
	{
		code: "RM006",
		bug:  true,
		reason: "entity.WithExcludeDirs matches a sub-realm by base name at any depth, so with sub_realms [\"team\"] the " +
			"directory realm/archive/team/ is skipped by rule-lint, realm-lint and realm-resolve, while rule-resolve " +
			"still returns its files for evaluation: a duplicate EUID there draws no RM006 and a broken entity no " +
			"RL code. No fixture nests a directory named after a sub-realm.",
	},
	{
		code: "RSV004",
		bug:  true,
		reason: "ruleresolve.LocalSource skips the check when rule_set.hash is the empty string, not only when it is " +
			"null, so such a file resolves although its hash differs from the file's; RL011 and RD008 report it. " +
			"hash-integrity holds no empty hash.",
	},
}

// goCaseCodes returns the codes the Go cases assert.
func goCaseCodes() map[string]bool {
	codes := make(map[string]bool)
	for _, c := range goCases {
		for _, w := range c.want {
			code, _, _ := strings.Cut(w, " ")
			codes[code] = true
		}
	}
	return codes
}

// TestEveryCodeIsCovered fails when a code of the registry has neither a
// scenario, a Go case nor a stated reason, so a new code cannot ship untested.
func TestEveryCodeIsCovered(t *testing.T) {
	scenarios := scenarioCodes(t)
	cases := goCaseCodes()
	excused := make(map[string]string, len(exceptions))
	for _, e := range exceptions {
		if e.reason == "" {
			t.Errorf("exception for %s states no reason", e.code)
		}
		if _, ok := result.LookupCode(e.code); !ok {
			t.Errorf("exception for %s, which the registry does not hold", e.code)
		}
		if !e.bug {
			excused[e.code] = e.reason
		}
	}
	for _, info := range result.Codes() {
		_, inScenario := scenarios[info.Code]
		reason, isExcused := excused[info.Code]
		covered := inScenario || cases[info.Code]
		switch {
		case info.Retired && covered:
			t.Errorf("%s is retired and must never fire again", info.Code)
		case info.Retired:
		case !covered && !isExcused:
			t.Errorf("%s (%s) has no scenario and no Go case: add one, or an exception with its reason", info.Code, info.Title)
		case covered && isExcused:
			t.Errorf("%s fires, so its exception %q is stale", info.Code, reason)
		}
	}
}

// TestRecordedCodesMatchTheRegistry fails on a code of a scenario or a Go case
// that the registry doesn't hold, or that fired from the wrong module or at a
// severity the registry doesn't give it.
func TestRecordedCodesMatchTheRegistry(t *testing.T) {
	for _, code := range sortedKeys(goCaseCodes()) {
		if _, ok := result.LookupCode(code); !ok {
			t.Errorf("a Go case asserts %s, which the registry does not hold", code)
		}
	}
	scenarios := scenarioCodes(t)
	for _, code := range sortedKeys(scenarios) {
		info, ok := result.LookupCode(code)
		if !ok {
			t.Errorf("expected.json holds %s, which the registry does not hold", code)
			continue
		}
		for _, f := range scenarios[code] {
			if f.Module != info.Module {
				t.Errorf("%s: the registry says module %s", f, info.Module)
			}
			if !severityAllows(info.Severity, f.Severity) {
				t.Errorf("%s: the registry says severity %s", f, info.Severity)
			}
		}
	}
}

// severityAllows reports whether got is one of the severities of registry, a
// cell such as "Info / Error".
func severityAllows(registry, got string) bool {
	return slices.ContainsFunc(strings.Split(registry, " / "), func(s string) bool {
		return strings.EqualFold(s, got)
	})
}
