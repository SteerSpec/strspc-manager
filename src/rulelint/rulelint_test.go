package rulelint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
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
	res := l.LintBytes(context.Background(), data)
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
	res := l.LintBytes(context.Background(), []byte(`{not valid json`))
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
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL003")
}

func TestRL004_BadRuleIDs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_rule_ids.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL004")
}

func TestRL005_BadSequence(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_sequence.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL005")
}

func TestRL005_LargeGap(t *testing.T) {
	// Rules 001 and 010 — should detect missing 002-009.
	ef := &entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0", Timestamp: "2026-01-01"},
		Rules: []entity.Rule{
			{ID: "TST-001", State: "D", Body: "r1", AddedBy: "t", AddedAt: "2026-01-01"},
			{ID: "TST-010", State: "D", Body: "r10", AddedBy: "t", AddedAt: "2026-01-01"},
		},
	}
	l := New()
	res := l.LintFile(ef)

	// Should report 8 missing numbers (002-009).
	count := 0
	for _, d := range res.Diagnostics {
		if d.Code == "RL005" {
			count++
		}
	}
	if count != 8 {
		t.Errorf("expected 8 RL005 diagnostics for gaps 002-009, got %d", count)
	}
}

func TestRL005_RuleNumberZero(t *testing.T) {
	ef := &entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0", Timestamp: "2026-01-01"},
		Rules: []entity.Rule{
			{ID: "TST-000", State: "D", Body: "r0", AddedBy: "t", AddedAt: "2026-01-01"},
			{ID: "TST-001", State: "D", Body: "r1", AddedBy: "t", AddedAt: "2026-01-01"},
		},
	}
	res := New().LintFile(ef)
	var msgs []string
	for _, d := range res.Diagnostics {
		if d.Code == "RL005" {
			msgs = append(msgs, d.Message)
		}
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "TST-000") {
		t.Errorf("expected one RL005 naming TST-000, got %v", msgs)
	}
}

func TestRL006_BadStates(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_states.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL006")
}

func TestRL007_BadParent(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_parent.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL007")
}

func TestRL008_BadNoteRefs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_refs.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL008")
}

func TestRL009_BadNoteIDs(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_ids.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL009")
}

func TestRL015_NoteIDsNotSequential(t *testing.T) {
	tests := []struct{ file, want string }{
		{"note_ids_gap.json", "missing note number TST-001/02"},
		{"note_ids_dup.json", "duplicate note ID TST-001/01"},
		{"note_ids_not_from_01.json", "missing note number TST-001/01"},
		{"note_ids_zero.json", "invalid note number in TST-001/00"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			data := mustReadFile(t, testdataPath("invalid", tt.file))
			res := New().LintBytes(context.Background(), data)
			found := false
			for _, d := range res.Diagnostics {
				if d.Code == "RL015" {
					found = true
					if d.Severity != result.Error {
						t.Errorf("RL015 severity = %v, want Error", d.Severity)
					}
					if !strings.Contains(d.Message, tt.want) {
						t.Errorf("RL015 message %q does not contain %q", d.Message, tt.want)
					}
				}
			}
			if !found {
				t.Errorf("expected RL015, got %v", res.Diagnostics)
			}
		})
	}
}

func TestRL015_SequentialNoteIDsPerRule(t *testing.T) {
	f := &entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0"},
		Rules: []entity.Rule{
			{ID: "TST-001", State: "D", Body: "a"},
			{ID: "TST-002", State: "D", Body: "b"},
		},
		Notes: []entity.Note{
			{ID: "TST-002/01", RuleRef: "TST-002", Type: "rationale"},
			{ID: "TST-001/01", RuleRef: "TST-001", Type: "rationale"},
			{ID: "TST-002/02", RuleRef: "TST-002", Type: "example"},
			{ID: "BADFORMAT", RuleRef: "TST-001", Type: "rationale"}, // RL009 only
		},
	}
	res := New().LintFile(f)
	assertNoCode(t, res, "RL015")
	assertHasCode(t, res, "RL009")
}

func TestRL010_BadNoteTypes(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_note_types.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL010")
}

func TestRL011_BadHash(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_hash.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL011")
}

func TestRL011_ValidHash(t *testing.T) {
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertNoCode(t, res, "RL011")
}

func TestRL011_HasPath(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_hash.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	for _, d := range res.Diagnostics {
		if d.Code == "RL011" {
			if d.Path == "" {
				t.Error("RL011 diagnostic should have a Path")
			}
			return
		}
	}
	t.Error("expected RL011 diagnostic")
}

func TestRL013_BadSemver(t *testing.T) {
	data := mustReadFile(t, testdataPath("invalid", "bad_semver.json"))
	l := New()
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL013")
}

func TestLintDir_CrossRefs(t *testing.T) {
	dir := testdataPath("crossref")
	l := New()
	res := l.LintDir(context.Background(), dir)

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
	res := l.LintDir(context.Background(), dir)

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

// --- RL002 Schema validation tests ---

// minimalEntitySchema is a minimal JSON Schema that requires an "entity" object
// with a string "id" field.
const minimalEntitySchema = `{
	"type": "object",
	"required": ["entity"],
	"properties": {
		"entity": {
			"type": "object",
			"required": ["id"],
			"properties": {
				"id": { "type": "string" }
			}
		}
	}
}`

func newTestFetcher(t *testing.T, schemaBody string, statusCode int) *schema.Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(statusCode)
		_, _ = fmt.Fprint(w, schemaBody)
	}))
	t.Cleanup(srv.Close)

	cacheDir := t.TempDir()
	return schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(cacheDir),
	)
}

func TestRL002_SchemaValidationPass(t *testing.T) {
	fetcher := newTestFetcher(t, minimalEntitySchema, http.StatusOK)
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New(WithSchemaFetcher(fetcher))
	res := l.LintBytes(context.Background(), data)
	assertNoCode(t, res, "RL002")
}

func TestRL002_SchemaValidationFail(t *testing.T) {
	// Schema requires "entity.id" to be a number — our valid file has a string.
	strictSchema := `{
		"type": "object",
		"required": ["entity"],
		"properties": {
			"entity": {
				"type": "object",
				"required": ["id"],
				"properties": {
					"id": { "type": "number" }
				}
			}
		}
	}`
	fetcher := newTestFetcher(t, strictSchema, http.StatusOK)
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New(WithSchemaFetcher(fetcher))
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL002")
}

func TestRL002_SchemaFetchError(t *testing.T) {
	fetcher := newTestFetcher(t, "not found", http.StatusNotFound)
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New(WithSchemaFetcher(fetcher))
	res := l.LintBytes(context.Background(), data)
	assertHasCode(t, res, "RL002")
}

func TestRL002_NoFetcherSkips(t *testing.T) {
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New() // no fetcher
	res := l.LintBytes(context.Background(), data)
	// Should have Info-level RL002 (skipped), not Error.
	for _, d := range res.Diagnostics {
		if d.Code == "RL002" && d.Severity == result.Error {
			t.Errorf("RL002 should be Info when no fetcher, got Error: %s", d)
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
		if d.Code == code {
			t.Errorf("unexpected diagnostic %s: %s", code, d)
		}
	}
}

func TestLintRealm_CrossEntitySupersedes(t *testing.T) {
	l := New()
	res := l.LintRealm(context.Background(), testdataPath("realm"))

	// ENT-001 supersedes RUL-001 — valid cross-entity reference, no RL012 for it.
	// SUB-001 supersedes MISSING-999 — invalid, should trigger RL012.

	// Check that we have exactly 1 RL012 diagnostic (for MISSING-999).
	var rl012Count int
	for _, d := range res.Diagnostics {
		if d.Code == "RL012" {
			rl012Count++
			if !strings.Contains(d.Message, "MISSING-999") {
				t.Errorf("expected RL012 for MISSING-999, got: %s", d.Message)
			}
		}
	}
	if rl012Count != 1 {
		t.Errorf("expected 1 RL012 diagnostic, got %d", rl012Count)
	}
}

func TestLintRealm_SubRealmCrossRef(t *testing.T) {
	// realm_with_sub has parent PAR.json with PAR-001, and sub-realm sync/SYN.json
	// with SYN-001 that supersedes PAR-001. Since sub-realm EUID scope is global,
	// the supersedes reference should resolve successfully (no RL012).
	l := New()
	res := l.LintRealm(context.Background(), testdataPath("realm_with_sub"))

	for _, d := range res.Diagnostics {
		if d.Code == "RL012" {
			t.Errorf("unexpected RL012: cross-realm supersedes should resolve; got: %s", d.Message)
		}
	}
}

func TestLintRealm_BackwardsCompatLintDir(t *testing.T) {
	// LintDir should still work shallow — ENT.json supersedes RUL-001 which
	// IS in the same directory, so no RL012 for it. But subdir/SUB.json
	// should NOT be scanned.
	l := New()
	res := l.LintDir(context.Background(), testdataPath("realm"))

	// In shallow mode, allRuleIDs only has ENT-001 and RUL-001.
	// ENT-001 supersedes RUL-001 — found, no RL012.
	// SUB-001 is not scanned at all (it's in subdir/).
	for _, d := range res.Diagnostics {
		if d.Code == "RL012" {
			t.Errorf("unexpected RL012 in shallow LintDir: %s", d.Message)
		}
	}
}

// --- RL014 note cross-reference tests ---

func TestLintDir_RL014_NoteCrossRefs(t *testing.T) {
	dir := testdataPath("crossref")
	res := New().LintDir(context.Background(), dir)

	var rl014 []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "RL014" {
			rl014 = append(rl014, d)
		}
	}
	// Only MISSING-002 (unqualified, unresolved, in an "extends" note) is
	// reported, and only once even though it appears twice. ENT-001 and
	// RUL-001 resolve, dev.other.realm:FOO-001 is qualified, NOPE-001 sits
	// in a rationale note, and dates/ISO-8601 do not match the pattern.
	if len(rl014) != 1 {
		t.Fatalf("expected exactly 1 RL014, got %d: %v", len(rl014), rl014)
	}
	d := rl014[0]
	if d.Severity != result.Warning {
		t.Errorf("RL014 severity = %s, want warning", d.Severity)
	}
	if !strings.Contains(d.Message, `"MISSING-002"`) || !strings.Contains(d.Message, "RUL-001/01") {
		t.Errorf("unexpected RL014 message: %q", d.Message)
	}
	if !strings.HasSuffix(d.Path, "RUL.json") {
		t.Errorf("RL014 path = %q, want the note's file", d.Path)
	}
}

func TestLintDir_RL014_StrictIsError(t *testing.T) {
	res := New(WithStrict(true)).LintDir(context.Background(), testdataPath("crossref"))
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "RL014" {
			found = true
			if d.Severity != result.Error {
				t.Errorf("strict RL014 severity = %s, want error", d.Severity)
			}
		}
	}
	if !found {
		t.Fatal("expected RL014 in strict mode")
	}
}

func TestCrossRefRe(t *testing.T) {
	cases := []struct {
		in        string
		wantRefs  []string
		qualified []bool
	}{
		{"ENT-001", []string{"ENT-001"}, []bool{false}},
		{"see dev.steerspec.core:ENT-001 now", []string{"ENT-001"}, []bool{true}},
		{"single-component realm core:ENT-001", []string{"ENT-001"}, []bool{true}},
		{"a ENT-001, b RUL-002.", []string{"ENT-001", "RUL-002"}, []bool{false, false}},
		{"ISO-8601 and 2026-03-22 and UTF-8", nil, nil},
		{"AB-001 too short", nil, nil},
		{"ENT-0012 too long", nil, nil},
	}
	for _, tc := range cases {
		ms := crossRefRe.FindAllStringSubmatch(tc.in, -1)
		if len(ms) != len(tc.wantRefs) {
			t.Errorf("%q: got %d matches, want %d (%v)", tc.in, len(ms), len(tc.wantRefs), ms)
			continue
		}
		for i, m := range ms {
			if got := m[1] + "-" + m[2]; got != tc.wantRefs[i] {
				t.Errorf("%q: match %d = %q, want %q", tc.in, i, got, tc.wantRefs[i])
			}
			if q := strings.Contains(m[0], ":"); q != tc.qualified[i] {
				t.Errorf("%q: match %d qualified=%v, want %v", tc.in, i, q, tc.qualified[i])
			}
		}
	}
}

// --- Parse-once benchmark (#54) ---

func BenchmarkLintDir(b *testing.B) {
	dir := testdataPath("realm")
	l := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = l.LintDir(context.Background(), dir)
	}
}

func BenchmarkLintBytes(b *testing.B) {
	data, err := os.ReadFile(testdataPath("valid", "basic.json"))
	if err != nil {
		b.Fatal(err)
	}
	l := New()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = l.LintBytes(context.Background(), data)
	}
}

func TestRL002_ContextCancelled(t *testing.T) {
	fetcher := newTestFetcher(t, minimalEntitySchema, http.StatusOK)
	data := mustReadFile(t, testdataPath("valid", "basic.json"))
	l := New(WithSchemaFetcher(fetcher))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := l.LintBytes(ctx, data)

	// The cancelled context must reach the schema fetch: RL002 reports the
	// cancellation instead of validating against a schema it never received.
	found := false
	for _, d := range res.Diagnostics {
		if d.Code == "RL002" && d.Severity == result.Error && strings.Contains(d.Message, context.Canceled.Error()) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected RL002 error mentioning %q, got %+v", context.Canceled, res.Diagnostics)
	}
}

// diagsWithCode returns the diagnostics carrying code.
func diagsWithCode(res *result.Result, code string) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func TestLintDir_RL016_InheritTargets(t *testing.T) {
	res := New().LintDir(context.Background(), testdataPath("inherit"))
	got := diagsWithCode(res, "RL016")
	// ORPHAN inherits the unknown NOPE and KID-002 overrides the unknown
	// MISSING-001. BASE and BASE-001 resolve; the qualified QUAL inherits and
	// KID-003 override are left to realm-resolve.
	if len(got) != 2 {
		t.Fatalf("expected 2 RL016, got %d: %v", len(got), got)
	}
	var msgs []string
	for _, d := range got {
		if d.Severity != result.Warning {
			t.Errorf("RL016 severity = %s, want warning", d.Severity)
		}
		msgs = append(msgs, d.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{`entity ORPHAN inherits "NOPE"`, `rule KID-002 overrides "MISSING-001"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing RL016 %q in:\n%s", want, joined)
		}
	}
}

func TestLintDir_RL016_StrictIsError(t *testing.T) {
	res := New(WithStrict(true)).LintDir(context.Background(), testdataPath("inherit"))
	for _, d := range diagsWithCode(res, "RL016") {
		if d.Severity != result.Error {
			t.Errorf("strict RL016 severity = %s, want error", d.Severity)
		}
	}
}

func TestLintDir_RL018_InheritChains(t *testing.T) {
	res := New().LintDir(context.Background(), testdataPath("inherit"))
	got := diagsWithCode(res, "RL018")
	// CYCA and CYCB each sit on the cycle; LONGA is four hops from LONGE.
	// LONGB (three hops), KID, ORPHAN and QUAL are fine.
	byEntity := make(map[string]string)
	for _, d := range got {
		if d.Severity != result.Error {
			t.Errorf("RL018 severity = %s, want error", d.Severity)
		}
		id := strings.TrimPrefix(strings.SplitN(d.Message, ":", 2)[0], "entity ")
		byEntity[id] = d.Message
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 RL018, got %d: %v", len(got), got)
	}
	for id, want := range map[string]string{"CYCA": "cyclic", "CYCB": "cyclic", "LONGA": "exceeds 3 hops"} {
		if !strings.Contains(byEntity[id], want) {
			t.Errorf("RL018 for %s = %q, want it to mention %q", id, byEntity[id], want)
		}
	}
}

func TestRL017_DuplicateOverrideTarget(t *testing.T) {
	rule := func(n, state string, supersedes, overrides *string) entity.Rule {
		return entity.Rule{ID: "KID-" + n, State: state, Body: "b", AddedBy: "t", AddedAt: "2026-10-29", Supersedes: supersedes, Overrides: overrides}
	}
	p := func(s string) *string { return &s }
	tests := []struct {
		name  string
		rules []entity.Rule
		want  int
	}{
		{"two live overrides", []entity.Rule{rule("001", "P", nil, p("ACCT-005")), rule("002", "D", nil, p("ACCT-005"))}, 1},
		{"distinct targets", []entity.Rule{rule("001", "P", nil, p("ACCT-005")), rule("002", "D", nil, p("ACCT-006"))}, 0},
		{"supersession restates override", []entity.Rule{rule("001", "R", nil, p("ACCT-005")), rule("002", "I", p("KID-001"), p("ACCT-005"))}, 0},
		{"abandoned draft ignored", []entity.Rule{rule("001", "A", nil, p("core:ACCT-005")), rule("002", "D", nil, p("core:ACCT-005"))}, 0},
		{"abandoned superseder keeps original live", []entity.Rule{rule("001", "I", nil, p("BASE-001")), rule("002", "A", p("KID-001"), p("BASE-001")), rule("003", "D", nil, p("BASE-001"))}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ef := &entity.File{
				Entity:  entity.Entity{ID: "KID", Title: "Kid", Inherits: "ACCT"},
				RuleSet: entity.RuleSet{Version: "1.0.0", Timestamp: "2026-10-29"},
				Rules:   tt.rules,
			}
			got := diagsWithCode(New().LintFile(ef), "RL017")
			if len(got) != tt.want {
				t.Fatalf("RL017 count = %d, want %d: %v", len(got), tt.want, got)
			}
		})
	}
}

func TestRL018_SingleFile(t *testing.T) {
	file := func(id, inherits string, subs ...entity.File) *entity.File {
		return &entity.File{
			Entity:      entity.Entity{ID: id, Title: id, Inherits: inherits},
			RuleSet:     entity.RuleSet{Version: "1.0.0", Timestamp: "2026-10-29"},
			SubEntities: subs,
		}
	}
	sub := func(id, inherits string) entity.File {
		f := file(id, inherits)
		f.Entity.Parent = "TOP"
		return *f
	}
	tests := []struct {
		name string
		ef   *entity.File
		want int
	}{
		{"self inheritance", file("TOP", "TOP"), 1},
		{"cycle within sub-entities", file("TOP", "", sub("SUBA", "SUBB"), sub("SUBB", "SUBA")), 2},
		{"chain leaving the file", file("TOP", "", sub("SUBA", "ELSEWHERE")), 0},
		{"sub inherits its container", file("TOP", "", sub("SUBA", "TOP")), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := diagsWithCode(New().LintFile(tt.ef), "RL018"); len(got) != tt.want {
				t.Errorf("LintFile RL018 count = %d, want %d: %v", len(got), tt.want, got)
			}
			data, err := json.Marshal(tt.ef)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if got := diagsWithCode(New().LintBytes(context.Background(), data), "RL018"); len(got) != tt.want {
				t.Errorf("LintBytes RL018 count = %d, want %d: %v", len(got), tt.want, got)
			}
		})
	}
}
