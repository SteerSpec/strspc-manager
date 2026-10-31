package result_test

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// update rewrites docs/diagnostic-codes.json from result.Codes().
var update = flag.Bool("update", false, "rewrite docs/diagnostic-codes.json")

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

	// fullRowRe splits one such row into its Code, Severity and When it fires
	// cells, trimming only the padding around each.
	fullRowRe = regexp.MustCompile(`^\|\s*([A-Z]+[0-9]{3})\s*\|\s*(.*?)\s*\|\s*(.*?)\s*\|\s*$`)

	// moduleHeadingRe matches a table's heading, such as "#### `rule-lint` (RL)".
	moduleHeadingRe = regexp.MustCompile("^#### `([a-z-]+)` \\([A-Z]+\\)\\s*$")
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
	documented := make(map[string]bool)
	for _, m := range rowRe.FindAllStringSubmatch(diagnosticSection(t, spec), -1) {
		documented[m[1]] = true
	}
	if len(documented) == 0 {
		t.Fatalf("Diagnostic Codes section of docs/SPEC.md lists no codes")
	}
	return documented
}

// diagnosticSection returns the text of the "Diagnostic Codes" section.
func diagnosticSection(t *testing.T, spec string) string {
	t.Helper()
	loc := sectionRe.FindStringIndex(spec)
	if loc == nil {
		t.Fatalf("docs/SPEC.md has no \"### N.N Diagnostic Codes\" section")
	}
	section := spec[loc[1]:]
	if end := headingRe.FindStringIndex(section); end != nil {
		section = section[:end[0]]
	}
	return section
}

// MOD-008: every diagnostic code has an entry in the registry that
// result.Codes() exports, with a title and a fix. The tests below hold that
// registry to the three sources it repeats: the Diagnostic Codes section of
// docs/SPEC.md, testdata/reserved_codes.txt and docs/diagnostic-codes.json.

// specRow is one row of a Diagnostic Codes table, cells verbatim.
type specRow struct {
	module, severity, summary string
}

// collectSpecRows parses the Diagnostic Codes tables of the given spec text
// into code -> row, taking each row's module from its table's heading.
func collectSpecRows(t *testing.T, spec string) map[string]specRow {
	t.Helper()
	rows := make(map[string]specRow)
	module := ""
	for line := range strings.Lines(diagnosticSection(t, spec)) {
		line = strings.TrimRight(line, "\r\n")
		if m := moduleHeadingRe.FindStringSubmatch(line); m != nil {
			module = m[1]
			continue
		}
		code := rowRe.FindStringSubmatch(line)
		if code == nil {
			continue
		}
		m := fullRowRe.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("docs/SPEC.md: %s row does not have the three cells Code, Severity, When it fires: %q", code[1], line)
		}
		if module == "" {
			t.Fatalf("docs/SPEC.md: %s is listed before any module heading", m[1])
		}
		if _, dup := rows[m[1]]; dup {
			t.Errorf("docs/SPEC.md: %s has two rows", m[1])
		}
		rows[m[1]] = specRow{module: module, severity: m[2], summary: m[3]}
	}
	if len(rows) == 0 {
		t.Fatalf("Diagnostic Codes section of docs/SPEC.md lists no codes")
	}
	return rows
}

func readSpecRows(t *testing.T) map[string]specRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "docs", "SPEC.md"))
	if err != nil {
		t.Fatalf("reading spec: %v", err)
	}
	return collectSpecRows(t, string(data))
}

func TestCodeRegistryMatchesSpec(t *testing.T) {
	rows := readSpecRows(t)
	if len(rows) < minExpectedCodes {
		t.Fatalf("parsed only %d rows from docs/SPEC.md; expected at least %d (regexp broken?)", len(rows), minExpectedCodes)
	}

	registered := make(map[string]bool)
	for _, info := range result.Codes() {
		registered[info.Code] = true
		row, ok := rows[info.Code]
		if !ok {
			t.Errorf("%s is in the registry but has no row in the Diagnostic Codes section of docs/SPEC.md", info.Code)
			continue
		}
		if info.Module != row.module {
			t.Errorf("%s: registry Module is %q, docs/SPEC.md lists it under %q", info.Code, info.Module, row.module)
		}
		if info.Severity != row.severity {
			t.Errorf("%s: registry Severity is %q, docs/SPEC.md says %q", info.Code, info.Severity, row.severity)
		}
		if info.Summary != row.summary {
			t.Errorf("%s: registry Summary differs from docs/SPEC.md\n registry: %s\n     spec: %s", info.Code, info.Summary, row.summary)
		}
	}
	for _, code := range sortedKeys(rows) {
		if !registered[code] {
			t.Errorf("%s has a row in docs/SPEC.md but no entry in src/result/codes_table.go (MOD-008)", code)
		}
	}
}

func TestCodeRegistryEntriesAreComplete(t *testing.T) {
	seen := make(map[string]bool)
	for _, info := range result.Codes() {
		if seen[info.Code] {
			t.Errorf("%s is registered twice", info.Code)
		}
		seen[info.Code] = true
		if strings.TrimSpace(info.Title) == "" {
			t.Errorf("%s has no Title (MOD-008)", info.Code)
		}
		if strings.TrimSpace(info.Fix) == "" {
			t.Errorf("%s has no Fix (MOD-008)", info.Code)
		}
	}
}

func TestCodeRegistryCoversEmittedCodes(t *testing.T) {
	emitted := collectEmittedCodes(t, filepath.Join("..", ".."))
	for _, code := range sortedKeys(emitted) {
		info, ok := result.LookupCode(code)
		if !ok {
			t.Errorf("%s is emitted by %s but has no entry in src/result/codes_table.go (MOD-008)", code, emitted[code])
			continue
		}
		if info.Retired {
			t.Errorf("%s is emitted by %s but the registry marks it retired", code, emitted[code])
		}
	}
}

func TestCodeRegistryMatchesReservedCodes(t *testing.T) {
	reserved := loadReservedCodes(t, reservedCodesPath)

	registered := make(map[string]bool)
	for _, info := range result.Codes() {
		registered[info.Code] = true
		entry, ok := reserved[info.Code]
		if !ok {
			t.Errorf("%s is in the registry but not in %s", info.Code, reservedCodesPath)
			continue
		}
		if retired := entry.status == "retired"; info.Retired != retired {
			t.Errorf("%s: registry Retired is %t, %s says %s", info.Code, info.Retired, reservedCodesPath, entry.status)
		}
	}
	for _, code := range sortedKeys(reserved) {
		if !registered[code] {
			t.Errorf("%s is in %s but has no entry in src/result/codes_table.go (MOD-008)", code, reservedCodesPath)
		}
	}
}

func TestCodesReturnsSortedCopy(t *testing.T) {
	codes := result.Codes()
	if !slices.IsSortedFunc(codes, func(a, b result.CodeInfo) int {
		return strings.Compare(a.Code, b.Code)
	}) {
		t.Error("Codes() is not sorted by code")
	}

	want := codes[0]
	codes[0].Title = "changed by the caller"
	if got := result.Codes()[0]; got != want {
		t.Errorf("changing the result of Codes() reached the registry: got %+v", got)
	}
}

func TestLookupCode(t *testing.T) {
	for _, want := range result.Codes() {
		got, ok := result.LookupCode(want.Code)
		if !ok || got != want {
			t.Errorf("LookupCode(%q) = %+v, %t; want %+v", want.Code, got, ok, want)
		}
	}
	for _, code := range []string{"", "RL999", "rl001"} {
		if got, ok := result.LookupCode(code); ok {
			t.Errorf("LookupCode(%q) = %+v; want no match", code, got)
		}
	}
}

// TestCodesJSONIsCurrent compares docs/diagnostic-codes.json with Codes().
// strspc-www reads that file at a pinned tag, without a Go toolchain.
// Regenerate it with: go test ./src/result -run TestCodesJSONIsCurrent -update
func TestCodesJSONIsCurrent(t *testing.T) {
	path := filepath.Join("..", "..", "docs", "diagnostic-codes.json")

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // Summaries hold `<EUID>` and the like.
	enc.SetIndent("", "  ")
	if err := enc.Encode(result.Codes()); err != nil {
		t.Fatalf("encoding the registry: %v", err)
	}

	if *update {
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("updating %s: %v", path, err)
		}
		t.Logf("updated %s", path)
		return
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	if !bytes.Equal(got, buf.Bytes()) {
		t.Errorf("docs/diagnostic-codes.json differs from result.Codes(); regenerate it with: go test ./src/result -run TestCodesJSONIsCurrent -update")
	}
}
