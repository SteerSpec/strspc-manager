package realmlint

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// testdataPath returns the absolute path to the testdata directory.
func testdataPath(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot determine test file path")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}

// hasCode returns true if the result contains a diagnostic with the given code.
func hasCode(res *result.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// hasCodeWithSeverity returns true if the result contains a diagnostic with
// the given code and severity.
func hasCodeWithSeverity(res *result.Result, code string, sev result.Severity) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code && d.Severity == sev {
			return true
		}
	}
	return false
}

// assertHasCode fails the test if the result does not contain the given code.
func assertHasCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	if !hasCode(res, code) {
		t.Errorf("expected diagnostic code %s, got: %v", code, res.Diagnostics)
	}
}

func TestLint_ValidRealm(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New()
	res := linter.Lint(dir)

	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("unexpected error: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func TestLint_ValidRealmWithRuleLinter(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	rl := rulelint.New()
	linter := New(WithRuleLinter(rl))
	res := linter.Lint(dir)

	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("unexpected error: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}

	// Should not have RM005 info skip message when linter is configured.
	if hasCodeWithSeverity(res, "RM005", result.Info) {
		t.Error("expected no RM005 info diagnostic when rule linter is configured")
	}
}

func TestRM001_MissingRealmJSON(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "missing_realm_json")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM001")
	if res.OK() {
		t.Error("expected errors for missing realm.json")
	}
}

func TestRM001_BadRealmJSON(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "bad_realm_json")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM001")
	if res.OK() {
		t.Error("expected errors for bad realm.json")
	}
}

func TestRM003_MissingSchemaDir(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "missing_schema_dir")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM003")
	if res.OK() {
		t.Error("expected errors for missing _schema/ directory")
	}
}

func TestRM004_MissingEntitySchema(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "missing_entity_schema")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM004")
	if res.OK() {
		t.Error("expected errors for missing entity.v1.schema.json")
	}
}

func TestRM005_NoDelegation(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New() // no RuleLinter configured
	res := linter.Lint(dir)

	// Should emit an Info diagnostic that entity validation was skipped.
	if !hasCodeWithSeverity(res, "RM005", result.Info) {
		t.Error("expected RM005 info diagnostic when no rule linter configured")
	}
}

func TestRM006_DuplicateEUID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "duplicate_euid")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM006")
	if res.OK() {
		t.Error("expected errors for duplicate EUID")
	}
}

func TestRM007_BadRealmID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "bad_realm_fields")
	linter := New()
	res := linter.Lint(dir)

	assertHasCode(t, res, "RM007")
	if res.OK() {
		t.Error("expected errors for bad realm fields")
	}

	// Should have two RM007 diagnostics (bad ID + bad version).
	count := 0
	for _, d := range res.Diagnostics {
		if d.Code == "RM007" {
			count++
		}
	}
	if count != 2 {
		t.Errorf("expected 2 RM007 diagnostics (ID + version), got %d", count)
	}
}

func TestRM002_NoSchemaFetcher(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New() // no SchemaFetcher configured
	res := linter.Lint(dir)

	// Should emit an Info diagnostic that schema validation was skipped.
	if !hasCodeWithSeverity(res, "RM002", result.Info) {
		t.Error("expected RM002 info diagnostic when no schema fetcher configured")
	}
}

func TestStrict_PromotesWarnings(t *testing.T) {
	// Create a result with a warning and verify strict mode promotes it.
	res := &result.Result{}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RM999",
		Severity: result.Warning,
		Message:  "test warning",
	})
	promoteWarnings(res)

	if res.Diagnostics[0].Severity != result.Error {
		t.Error("expected warning to be promoted to error in strict mode")
	}
}

func TestRealmIDRegex(t *testing.T) {
	tests := []struct {
		id    string
		valid bool
	}{
		{"dev.steerspec.core", true},
		{"com.acme.security", true},
		{"myproject", true},
		{"a", true},
		{"a1b2", true},
		{"dev.steerspec", true},
		{"INVALID", false},
		{"dev.steerspec.CORE", false},
		{"dev..steerspec", false},
		{".dev.steerspec", false},
		{"dev.steerspec.", false},
		{"", false},
		{"dev-steerspec", false},
		{"dev_steerspec", false},
		{"1dev.steerspec", false},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			if got := realmIDRe.MatchString(tt.id); got != tt.valid {
				t.Errorf("realmIDRe.MatchString(%q) = %v, want %v", tt.id, got, tt.valid)
			}
		})
	}
}
