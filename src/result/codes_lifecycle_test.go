package result_test

import (
	"bufio"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// MOD-002: a module emits diagnostic codes only from the prefix it owns.
// MOD-003: a code that has shipped is never reused or renumbered; a retired
// check keeps its code reserved.
//
// Both rules are enforced against the same source scan as the MOD-006 test
// in codes_doc_test.go, plus the registry testdata/reserved_codes.txt.

// ownedPrefix maps each module directory in codeModules to the single
// diagnostic prefix it owns. MOD-002 lists the six engine modules; evalcache
// is documented in docs/SPEC.md §6.3 as the owner of EC and is held to the
// same contract.
var ownedPrefix = map[string]string{
	"src/rulelint":     "RL",
	"src/rulediff":     "RD",
	"src/realmlint":    "RM",
	"src/realmresolve": "RR",
	"src/ruleresolve":  "RSV",
	"src/ruleeval":     "RE",
	"src/evalcache":    "EC",
}

// reservedCodesPath is the registry of every code that has shipped.
const reservedCodesPath = "testdata/reserved_codes.txt"

func TestDiagnosticCodesUseOwnedPrefix(t *testing.T) {
	root := filepath.Join("..", "..")

	owners := make(map[string]string) // prefix -> module
	for mod, prefix := range ownedPrefix {
		if other, dup := owners[prefix]; dup {
			t.Fatalf("prefix %s is claimed by both %s and %s", prefix, other, mod)
		}
		owners[prefix] = mod
	}

	for _, mod := range codeModules {
		prefix, ok := ownedPrefix[mod]
		if !ok {
			t.Errorf("%s is scanned for codes but owns no prefix; add it to ownedPrefix (MOD-002)", mod)
			continue
		}
		for code, file := range collectCodesIn(t, filepath.Join(root, mod), root) {
			if got := codePrefix(code); got != prefix {
				t.Errorf("%s emits %s but owns only the %s prefix (MOD-002)", file, code, prefix)
			}
		}
	}
}

func TestDiagnosticCodesAreReserved(t *testing.T) {
	root := filepath.Join("..", "..")

	registry := loadReservedCodes(t, reservedCodesPath)
	emitted := collectEmittedCodes(t, root)
	documented := collectDocumentedCodes(t, filepath.Join(root, "docs", "SPEC.md"))

	owned := make(map[string]bool)
	for _, prefix := range ownedPrefix {
		owned[prefix] = true
	}

	codes := make([]string, 0, len(registry))
	for code := range registry {
		codes = append(codes, code)
	}
	sort.Strings(codes)

	for _, code := range codes {
		if !owned[codePrefix(code)] {
			t.Errorf("%s: %s has a prefix no module owns", reservedCodesPath, code)
		}
		switch registry[code] {
		case "active":
			if _, ok := emitted[code]; !ok {
				t.Errorf("%s is registered as active but no module emits it; mark it retired in %s instead of dropping it (MOD-003)", code, reservedCodesPath)
			}
		case "retired":
			if file, ok := emitted[code]; ok {
				t.Errorf("%s is retired and reserved but %s emits it again (MOD-003)", code, file)
			}
			if !documented[code] {
				t.Errorf("%s is retired but has no row in the Diagnostic Codes section of docs/SPEC.md; a reserved code keeps its row (MOD-003)", code)
			}
		default:
			t.Errorf("%s: %s has unknown status %q (want active or retired)", reservedCodesPath, code, registry[code])
		}
	}

	var unregistered []string
	for code := range emitted {
		if _, ok := registry[code]; !ok {
			unregistered = append(unregistered, code)
		}
	}
	sort.Strings(unregistered)
	for _, code := range unregistered {
		t.Errorf("%s is emitted by %s but not registered; append \"%s active\" to %s (MOD-003)", code, emitted[code], code, reservedCodesPath)
	}
}

// codePrefix returns the letters of a diagnostic code ("RSV004" -> "RSV").
func codePrefix(code string) string {
	return strings.TrimRight(code, "0123456789")
}

// loadReservedCodes parses the registry into code -> status. Blank lines and
// lines starting with '#' are ignored.
func loadReservedCodes(t *testing.T, path string) map[string]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening registry: %v", err)
	}
	defer func() { _ = f.Close() }()

	registry := make(map[string]string)
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 2 {
			t.Fatalf("%s:%d: want \"<code> <status>\", got %q", path, line, text)
		}
		if _, dup := registry[fields[0]]; dup {
			t.Fatalf("%s:%d: %s is listed twice", path, line, fields[0])
		}
		registry[fields[0]] = fields[1]
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading registry: %v", err)
	}
	if len(registry) == 0 {
		t.Fatalf("%s lists no codes", path)
	}
	return registry
}
