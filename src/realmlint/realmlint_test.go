package realmlint

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
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
	res := linter.Lint(context.Background(), dir)

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
	res := linter.Lint(context.Background(), dir)

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
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM001")
	if res.OK() {
		t.Error("expected errors for missing realm.json")
	}
}

func TestRM001_BadRealmJSON(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "bad_realm_json")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM001")
	if res.OK() {
		t.Error("expected errors for bad realm.json")
	}
}

func TestRM003_MissingSchemaDir(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "missing_schema_dir")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM003")
	if res.OK() {
		t.Error("expected errors for missing _schema/ directory")
	}
}

func TestRM004_MissingEntitySchema(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "missing_entity_schema")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM004")
	if res.OK() {
		t.Error("expected errors for missing entity.v1.schema.json")
	}
}

func TestRM005_NoDelegation(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New() // no RuleLinter configured
	res := linter.Lint(context.Background(), dir)

	// Should emit an Info diagnostic that entity validation was skipped.
	if !hasCodeWithSeverity(res, "RM005", result.Info) {
		t.Error("expected RM005 info diagnostic when no rule linter configured")
	}
}

func TestLint_NestedEntityFiles(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	rl := rulelint.New()
	linter := New(WithRuleLinter(rl))
	res := linter.Lint(context.Background(), dir)

	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("unexpected error: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}

	// Should not report RM006 — TST and NESTED have distinct EUIDs.
	if hasCode(res, "RM006") {
		t.Error("unexpected RM006 diagnostic for valid nested entities")
	}

	// Verify the nested entity file was actually processed by rulelint.
	// rulelint.New() without a schema fetcher always emits RL002 per file.
	nestedSuffix := filepath.Join("subdir", "NESTED.json")
	foundNested := false
	for _, d := range res.Diagnostics {
		if strings.HasSuffix(d.Path, nestedSuffix) {
			foundNested = true
			break
		}
	}
	if !foundNested {
		t.Error("expected at least one diagnostic for nested entity file subdir/NESTED.json")
	}
}

func TestRM006_NestedDuplicateEUID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "nested_duplicate_euid")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM006")
	if res.OK() {
		t.Error("expected errors for nested duplicate EUID")
	}
}

func TestScanEntityFiles_SkipsSchemaDir(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	rl := rulelint.New()
	linter := New(WithRuleLinter(rl))
	res := linter.Lint(context.Background(), dir)

	// No diagnostic should reference any path inside _schema/.
	for _, d := range res.Diagnostics {
		if filepath.Base(filepath.Dir(d.Path)) == "_schema" {
			t.Errorf("unexpected diagnostic for _schema/ file: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func TestScanEntityFiles_SkipsNestedRealmJSON(t *testing.T) {
	// testdata/valid/subdir/realm.json contains invalid JSON that would
	// trigger diagnostics if processed as an entity file.
	dir := filepath.Join(testdataPath(t), "valid")
	rl := rulelint.New()
	linter := New(WithRuleLinter(rl))
	res := linter.Lint(context.Background(), dir)

	for _, d := range res.Diagnostics {
		if strings.HasSuffix(d.Path, filepath.Join("subdir", "realm.json")) {
			t.Errorf("realm.json in subdirectory should be skipped, got diagnostic: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func TestRM006_DuplicateEUID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "duplicate_euid")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM006")
	if res.OK() {
		t.Error("expected errors for duplicate EUID")
	}
}

func TestRM007_BadRealmID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "bad_realm_fields")
	linter := New()
	res := linter.Lint(context.Background(), dir)

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

// minimalRealmSchema is a minimal JSON Schema that validates realm.json structure.
const minimalRealmSchema = `{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","required":["realm"],"properties":{"realm":{"type":"object","required":["id","title","version"],"properties":{"id":{"type":"string"},"title":{"type":"string"},"version":{"type":"string"}}}}}`

// newTestFetcher creates a schema.Fetcher backed by a test HTTP server
// serving the minimal realm schema at realm/v1.json.
func newTestFetcher(t *testing.T) *schema.Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/realm/v1.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprint(w, minimalRealmSchema)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(t.TempDir()),
	)
}

// newBrokenFetcher creates a schema.Fetcher that always returns 500.
func newBrokenFetcher(t *testing.T) *schema.Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	return schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(t.TempDir()),
	)
}

func TestRM002_SchemaValidationPass(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	fetcher := newTestFetcher(t)
	linter := New(WithSchemaFetcher(fetcher))
	res := linter.Lint(context.Background(), dir)

	// Should not have RM002 error.
	if hasCodeWithSeverity(res, "RM002", result.Error) {
		for _, d := range res.Diagnostics {
			if d.Code == "RM002" {
				t.Errorf("unexpected RM002 error: %s", d.Message)
			}
		}
	}
}

func TestRM002_SchemaValidationFail(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "schema_fail")
	fetcher := newTestFetcher(t)
	linter := New(WithSchemaFetcher(fetcher))
	res := linter.Lint(context.Background(), dir)

	// realm.json is missing the required "realm" key, so schema validation fails.
	if !hasCodeWithSeverity(res, "RM002", result.Error) {
		t.Error("expected RM002 error for schema validation failure")
	}
}

func TestRM002_SchemaFetchError(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	fetcher := newBrokenFetcher(t)
	linter := New(WithSchemaFetcher(fetcher))
	res := linter.Lint(context.Background(), dir)

	// Should have RM002 error from failed fetch.
	if !hasCodeWithSeverity(res, "RM002", result.Error) {
		t.Error("expected RM002 error when schema fetch fails")
	}
}

func TestRM002_NoSchemaFetcher(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New() // no SchemaFetcher configured
	res := linter.Lint(context.Background(), dir)

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

func TestLint_ValidSubRealm(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid_sub_realm")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("unexpected error: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}

	// Should not report any RM008-RM012 diagnostics.
	for _, code := range []string{"RM008", "RM009", "RM010", "RM011", "RM012"} {
		if hasCode(res, code) {
			t.Errorf("unexpected %s diagnostic for valid sub-realm", code)
		}
	}

	// Should not report RM006 — PARENT and SYNC have distinct EUIDs.
	if hasCode(res, "RM006") {
		t.Error("unexpected RM006 diagnostic for valid sub-realm")
	}
}

func TestRM008_SubRealmMissingDir(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_missing_dir")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM008")
	if res.OK() {
		t.Error("expected errors for missing sub-realm directory")
	}
}

func TestRM009_SubRealmNoManifest(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_no_manifest")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM009")
	if res.OK() {
		t.Error("expected errors for sub-realm without realm.json")
	}
}

func TestRM010_SubRealmBadID(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_bad_id")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM010")
	if res.OK() {
		t.Error("expected errors for sub-realm with wrong ID")
	}

	// Verify the message mentions the expected ID.
	for _, d := range res.Diagnostics {
		if d.Code == "RM010" {
			if !strings.Contains(d.Message, "dev.steerspec.test.sync") {
				t.Errorf("RM010 message should mention expected ID, got: %s", d.Message)
			}
		}
	}
}

func TestRM011_SubRealmNested(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_nested")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM011")
	if res.OK() {
		t.Error("expected errors for nested sub-realm")
	}
}

func TestRM013_SubRealmIdentifierFormat(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_identifier_format")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM013")
	if res.OK() {
		t.Error("expected errors for sub-realm with its own rule_identifier_format")
	}
}

func TestRM007_SubRealmManifestMissingFields(t *testing.T) {
	// A child manifest with the expected id but no title or version used to
	// pass: RM007 ran only on the parent (strspc-manager#83).
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_bad_manifest")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM007")
	if res.OK() {
		t.Error("expected errors for sub-realm manifest without title and version")
	}
	for _, d := range res.Diagnostics {
		if d.Code == "RM007" && !strings.HasSuffix(d.Path, filepath.Join("sync", "realm.json")) {
			t.Errorf("RM007 should point at the sub-realm manifest, got path %q", d.Path)
		}
	}
	if hasCode(res, "RM010") {
		t.Error("the id is correct, so RM010 must not fire")
	}
}

func TestRM002_SubRealmSchemaValidationFail(t *testing.T) {
	// The child manifest has no "realm" key, so it fails realm.v1; the
	// diagnostic must point at the child, not the parent.
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_schema_fail")
	linter := New(WithSchemaFetcher(newTestFetcher(t)))
	res := linter.Lint(context.Background(), dir)

	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "RM002" && d.Severity == result.Error && strings.HasSuffix(d.Path, filepath.Join("sync", "realm.json")) {
			found = true
		}
	}
	if !found {
		t.Error("expected an RM002 error at the sub-realm manifest path")
	}
}

func TestRM003_SubRealmMissingSchemaDir(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_no_schema_dir")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM003")
	if res.OK() {
		t.Error("expected errors for sub-realm without _schema/")
	}
}

func TestRM012_SubRealmEUIDCollision(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "invalid", "sub_realm_euid_collision")
	linter := New()
	res := linter.Lint(context.Background(), dir)

	assertHasCode(t, res, "RM012")
	if res.OK() {
		t.Error("expected errors for EUID collision between parent and sub-realm")
	}

	// Verify the message mentions the colliding EUID.
	for _, d := range res.Diagnostics {
		if d.Code == "RM012" {
			if !strings.Contains(d.Message, "COLLIDE") {
				t.Errorf("RM012 message should mention colliding EUID, got: %s", d.Message)
			}
		}
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

func TestRM002_ContextCancelled(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "valid")
	linter := New(WithSchemaFetcher(newTestFetcher(t)))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := linter.Lint(ctx, dir)

	// The cancelled context must reach the schema fetch: RM002 reports the
	// cancellation instead of validating against a schema it never received.
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "RM002" && d.Severity == result.Error && strings.Contains(d.Message, context.Canceled.Error()) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RM002 error mentioning %q, got %+v", context.Canceled, res.Diagnostics)
	}
}

// TestLint_RL017_CrossFileSupersession checks that realm-lint's per-file
// delegation sees superseders in other files of the realm: OTHER-001
// supersedes KID-001, so only CONF's real duplicate override is reported.
func TestLint_RL017_CrossFileSupersession(t *testing.T) {
	dir := filepath.Join(testdataPath(t), "inherit_xsup")
	res := New(WithRuleLinter(rulelint.New())).Lint(context.Background(), dir)

	var rl017 []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "RL017" {
			rl017 = append(rl017, d)
		}
	}
	if len(rl017) != 1 {
		t.Fatalf("expected 1 RL017, got %d: %v", len(rl017), rl017)
	}
	if !strings.Contains(rl017[0].Message, "CONF-001 and CONF-002") || !strings.HasPrefix(rl017[0].Path, filepath.Join(dir, "CONF.json")) {
		t.Errorf("unexpected RL017: %s: %s", rl017[0].Path, rl017[0].Message)
	}
}
