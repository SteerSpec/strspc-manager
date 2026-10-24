package result_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// MOD-006: every diagnostic code a module emits MUST be described in
// docs/SPEC.md. This test collects the codes from the module sources and
// checks them against the "Diagnostic Codes" section of the spec, in both
// directions: a code emitted but not documented fails, and so does a code
// documented but no longer emitted, unless the registry in
// testdata/reserved_codes.txt marks it retired (MOD-003).

// codeModules are the source directories, relative to the repository root,
// whose diagnostic codes the spec must describe. Each is walked recursively.
var codeModules = []string{
	"src/rulelint",
	"src/rulediff",
	"src/realmlint",
	"src/realmresolve",
	"src/ruleresolve",
	"src/ruleeval",
	"src/evalcache",
}

// minExpectedCodes guards against a regexp that silently matches nothing.
const minExpectedCodes = 50

var (
	// codeRe matches a code assigned to a Diagnostic field (`Code: "RL001"`)
	// or to a named constant (`CodeViolated = "RE001"`).
	codeRe = regexp.MustCompile(`(?i:code)\w*\s*(?::|=|:=)\s*"([A-Z]+[0-9]{3})"`)

	// sectionRe locates the heading of the spec section that lists the codes.
	sectionRe = regexp.MustCompile(`(?m)^### [0-9.]+ Diagnostic Codes\s*$`)

	// headingRe matches any level-2 or level-3 heading, ending the section.
	headingRe = regexp.MustCompile(`(?m)^###? `)

	// rowRe matches a table row whose first cell is a diagnostic code.
	rowRe = regexp.MustCompile(`(?m)^\|\s*([A-Z]+[0-9]{3})\s*\|`)
)

func TestDiagnosticCodesAreDocumented(t *testing.T) {
	root := filepath.Join("..", "..")

	emitted := collectEmittedCodes(t, root)
	if len(emitted) < minExpectedCodes {
		t.Fatalf("collected only %d codes from sources; expected at least %d (regexp broken?)", len(emitted), minExpectedCodes)
	}

	documented := collectDocumentedCodes(t, filepath.Join(root, "docs", "SPEC.md"))
	registry := loadReservedCodes(t, reservedCodesPath)

	var missing, stale []string
	for code := range emitted {
		if !documented[code] {
			missing = append(missing, code)
		}
	}
	for code := range documented {
		if _, ok := emitted[code]; ok {
			continue
		}
		if registry[code].status == "retired" {
			continue // MOD-003: a retired check keeps its row as a reservation.
		}
		stale = append(stale, code)
	}
	sort.Strings(missing)
	sort.Strings(stale)

	for _, code := range missing {
		t.Errorf("%s is emitted by %s but has no row in the Diagnostic Codes section of docs/SPEC.md (MOD-006)", code, emitted[code])
	}
	for _, code := range stale {
		t.Errorf("%s has a row in the Diagnostic Codes section of docs/SPEC.md but is not emitted by any module", code)
	}
}

// collectEmittedCodes returns code -> first source file that emits it,
// across every module in codeModules.
func collectEmittedCodes(t *testing.T, root string) map[string]string {
	t.Helper()
	codes := make(map[string]string)
	for _, mod := range codeModules {
		for code, file := range collectCodesIn(t, filepath.Join(root, mod), root) {
			if _, seen := codes[code]; !seen {
				codes[code] = file
			}
		}
	}
	return codes
}

// collectCodesIn walks one module directory and returns code -> first
// non-test source file (relative to root) that emits it.
func collectCodesIn(t *testing.T, dir, root string) map[string]string {
	t.Helper()
	codes := make(map[string]string)
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range codeRe.FindAllSubmatch(src, -1) {
			code := string(m[1])
			if _, seen := codes[code]; !seen {
				rel, _ := filepath.Rel(root, path)
				codes[code] = filepath.ToSlash(rel)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", dir, err)
	}
	return codes
}

// collectDocumentedCodes returns the set of codes listed as table rows in
// the "Diagnostic Codes" section of the spec file at specPath.
func collectDocumentedCodes(t *testing.T, specPath string) map[string]bool {
	t.Helper()
	data, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	return documentedCodesIn(t, string(data))
}

// documentedCodesIn returns the set of codes listed as table rows in the
// "Diagnostic Codes" section of the given spec text.
func documentedCodesIn(t *testing.T, spec string) map[string]bool {
	t.Helper()
	loc := sectionRe.FindStringIndex(spec)
	if loc == nil {
		t.Fatalf("docs/SPEC.md has no \"### N.N Diagnostic Codes\" section")
	}
	section := spec[loc[1]:]
	if end := headingRe.FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}

	documented := make(map[string]bool)
	for _, m := range rowRe.FindAllStringSubmatch(section, -1) {
		documented[m[1]] = true
	}
	if len(documented) == 0 {
		t.Fatalf("Diagnostic Codes section of docs/SPEC.md lists no codes")
	}
	return documented
}
