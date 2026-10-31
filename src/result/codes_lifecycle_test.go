package result_test

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// MOD-007: a module emits diagnostic codes only from the prefix it owns.
// MOD-003: a code that has shipped is never reused or renumbered; a retired
// check keeps its code reserved.
//
// Both rules are enforced against the same source scan as the MOD-006 test
// in codes_doc_test.go, plus the registry testdata/reserved_codes.txt. The
// registry itself is checked against the version shipped by the latest
// release tag, so a line cannot be deleted or given a new meaning without
// failing the build.

// ownedPrefix maps each module directory in codeModules to the single
// diagnostic prefix it owns, as listed in MOD-007.
var ownedPrefix = map[string]string{
	"src/rulelint":     "RL",
	"src/rulediff":     "RD",
	"src/realmlint":    "RM",
	"src/realmresolve": "RR",
	"src/ruleresolve":  "RSV",
	"src/ruleeval":     "RE",
	"src/evalcache":    "EC",
	"src/rulerefs":     "RF",
}

// reservedCodesPath is the registry of every code that has shipped,
// relative to this package directory.
const reservedCodesPath = "testdata/reserved_codes.txt"

// reservedEntry is one registry line.
type reservedEntry struct {
	status  string // "active" or "retired"
	meaning string // stable slug, never changes once shipped
}

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
			t.Errorf("%s is scanned for codes but owns no prefix; add it to ownedPrefix (MOD-007)", mod)
			continue
		}
		for code, file := range collectCodesIn(t, filepath.Join(root, mod), root) {
			if got := codePrefix(code); got != prefix {
				t.Errorf("%s emits %s but owns only the %s prefix (MOD-007)", file, code, prefix)
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

	for _, code := range sortedKeys(registry) {
		entry := registry[code]
		if !owned[codePrefix(code)] {
			t.Errorf("%s: %s has a prefix no module owns", reservedCodesPath, code)
		}
		switch entry.status {
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
			t.Errorf("%s: %s has unknown status %q (want active or retired)", reservedCodesPath, code, entry.status)
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
		t.Errorf("%s is emitted by %s but not registered; append \"%s active <meaning>\" to %s (MOD-003)", code, emitted[code], code, reservedCodesPath)
	}
}

// TestReservedCodesAreAppendOnly compares the registry with the one shipped
// by the latest release tag: every shipped line must still be present with
// the same meaning, and a shipped status may only move from active to
// retired. Releases that predate the registry fall back to the codes listed
// in docs/SPEC.md at that tag, which must all still be registered.
func TestReservedCodesAreAppendOnly(t *testing.T) {
	root := filepath.Join("..", "..")
	registry := loadReservedCodes(t, reservedCodesPath)

	tag := latestReleaseTag(t, root)
	baselineRegistry, ok := gitShow(t, root, tag, "src/result/testdata/reserved_codes.txt")
	if !ok {
		spec, ok := gitShow(t, root, tag, "docs/SPEC.md")
		if !ok {
			t.Fatalf("%s has neither the registry nor docs/SPEC.md", tag)
		}
		for _, code := range sortedKeys(documentedCodesIn(t, string(spec))) {
			if _, ok := registry[code]; !ok {
				t.Errorf("%s shipped in %s but was dropped from %s; register it as retired instead (MOD-003)", code, tag, reservedCodesPath)
			}
		}
		return
	}

	baseline := parseReservedCodes(t, tag+":"+reservedCodesPath, bytes.NewReader(baselineRegistry))
	for _, code := range sortedKeys(baseline) {
		was, now := baseline[code], registry[code]
		if now == (reservedEntry{}) {
			t.Errorf("%s shipped in %s but was dropped from %s; a shipped line is never deleted (MOD-003)", code, tag, reservedCodesPath)
			continue
		}
		if now.meaning != was.meaning {
			t.Errorf("%s shipped in %s meaning %q but now reads %q; a shipped code never changes meaning (MOD-003)", code, tag, was.meaning, now.meaning)
		}
		if was.status == "retired" && now.status != "retired" {
			t.Errorf("%s shipped retired in %s but is %s again; a retired code stays reserved (MOD-003)", code, tag, now.status)
		}
	}
}

// codePrefix returns the letters of a diagnostic code ("RSV004" -> "RSV").
func codePrefix(code string) string {
	return strings.TrimRight(code, "0123456789")
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// loadReservedCodes parses the registry file into code -> entry.
func loadReservedCodes(t *testing.T, path string) map[string]reservedEntry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("opening registry: %v", err)
	}
	defer func() { _ = f.Close() }()
	return parseReservedCodes(t, path, f)
}

// parseReservedCodes parses registry lines of the form `<code> <status>
// <meaning>`. Blank lines and lines starting with '#' are ignored.
func parseReservedCodes(t *testing.T, name string, r io.Reader) map[string]reservedEntry {
	t.Helper()
	sc := bufio.NewScanner(r)
	registry := make(map[string]reservedEntry)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			t.Fatalf("%s:%d: want \"<code> <status> <meaning>\", got %q", name, line, text)
		}
		if _, dup := registry[fields[0]]; dup {
			t.Fatalf("%s:%d: %s is listed twice", name, line, fields[0])
		}
		registry[fields[0]] = reservedEntry{status: fields[1], meaning: fields[2]}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("reading %s: %v", name, err)
	}
	if len(registry) == 0 {
		t.Fatalf("%s lists no codes", name)
	}
	return registry
}

// latestReleaseTag returns the highest v* tag of the repository at root.
// It fails, rather than skips, when git or the tags are unavailable: CI
// checks out the full history so the baseline is always there.
func latestReleaseTag(t *testing.T, root string) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "git", "-C", root, "tag", "--list", "v*", "--sort=-v:refname").Output()
	if err != nil {
		t.Fatalf("listing release tags (is this a full clone with tags?): %v", err)
	}
	tags := strings.Fields(string(out))
	if len(tags) == 0 {
		t.Fatalf("no v* release tag found; fetch tags (git fetch --tags) so the shipped baseline can be checked")
	}
	return tags[0]
}

// gitShow returns the content of path at rev, or ok=false if the path does
// not exist at that revision.
func gitShow(t *testing.T, root, rev, path string) ([]byte, bool) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "-C", root, "show", rev+":"+path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && strings.Contains(stderr.String(), "not in") {
			return nil, false
		}
		t.Fatalf("git show %s:%s: %v (%s)", rev, path, err, strings.TrimSpace(stderr.String()))
	}
	return out, true
}
