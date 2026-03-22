package rulelint

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func testdataPath(elem ...string) string {
	_, file, _, _ := runtime.Caller(0)
	parts := append([]string{filepath.Dir(file), "testdata"}, elem...)
	return filepath.Join(parts...)
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return data
}

func TestLintBytes_ValidBasic(t *testing.T) {
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New()
	res := l.LintBytes(data)
	// Should have only the schema-skipped info diagnostic (RL002).
	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("unexpected error: %s", d)
		}
	}
}

func TestLintFile_ValidBasic(t *testing.T) {
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	ef, err := entity.Parse(data)
	if err != nil {
		t.Fatalf("parsing: %v", err)
	}
	l := New()
	res := l.LintFile(ef)
	if !res.OK() {
		for _, d := range res.Errors() {
			t.Errorf("unexpected error: %s", d)
		}
	}
}

func TestLintBytes_InvalidJSON(t *testing.T) {
	l := New()
	res := l.LintBytes([]byte(`{not valid json`))
	if res.OK() {
		t.Fatal("expected RL001 error for invalid JSON")
	}
	if res.Diagnostics[0].Code != "RL001" {
		t.Errorf("expected RL001, got %s", res.Diagnostics[0].Code)
	}
}

func TestRL003_BadEUID(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_euid.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL003")
}

func TestRL004_BadRuleIDs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_rule_ids.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL004")
}

func TestRL005_BadSequence(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_sequence.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL005")
}

func TestRL006_BadStates(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_states.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL006")
}

func TestRL007_BadParent(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_parent.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL007")
}

func TestRL008_BadNoteRefs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_refs.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL008")
}

func TestRL009_BadNoteIDs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_ids.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL009")
}

func TestRL010_BadNoteTypes(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_types.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL010")
}

func TestRL011_BadHash(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_hash.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL011")
}

func TestRL011_ValidHash(t *testing.T) {
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New()
	res := l.LintBytes(data)
	assertNoCode(t, res, "RL011")
}

func TestRL013_BadSemver(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_semver.json"))
	l := New()
	res := l.LintBytes(data)
	assertHasCode(t, res, "RL013")
}

func TestLintDir_CrossRefs(t *testing.T) {
	dir := testdataPath("crossref")
	l := New()
	res := l.LintDir(dir)

	// Should have an RL012 warning for MISSING-001.
	assertHasCode(t, res, "RL012")

	// The valid supersedes (ENT-001) should not trigger RL012.
	count := 0
	for _, d := range res.Diagnostics {
		if d.Code == "RL012" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 RL012 diagnostic, got %d", count)
	}
}

func TestLintDir_StrictPromotesWarnings(t *testing.T) {
	dir := testdataPath("crossref")
	l := New(WithStrict(true))
	res := l.LintDir(dir)

	for _, d := range res.Diagnostics {
		if d.Code == "RL012" && d.Severity != result.Error {
			t.Errorf("expected RL012 to be Error in strict mode, got %s", d.Severity)
		}
	}
}

func TestStrictMode(t *testing.T) {
	ef := &entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0", Timestamp: "2026-01-01"},
		Rules: []entity.Rule{
			{ID: "TST-001", Revision: 0, State: "D", Body: "test", AddedBy: "t", AddedAt: "2026-01-01"},
		},
	}

	l := New(WithStrict(true))
	res := l.LintFile(ef)
	for _, d := range res.Diagnostics {
		if d.Severity == result.Warning {
			t.Errorf("strict mode should have no warnings, got: %s", d)
		}
	}
}

func assertHasCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Errorf("expected diagnostic %s but not found. diagnostics: %v", code, res.Diagnostics)
}

func assertNoCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code && d.Severity == result.Error {
			t.Errorf("unexpected diagnostic %s: %s", code, d)
		}
	}
}
