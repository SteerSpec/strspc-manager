package rulediff

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// helpers

func ptr(s string) *string { return &s }

// makeBase returns a minimal valid base entity with three rules (D, P, I)
// and one changelog note.
func makeBase() *entity.File {
	return &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "TSTENT", Title: "Test Entity"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "TSTENT-001", Revision: 0, State: "D", Body: "Draft body", AddedBy: "test@example.com", AddedAt: "2026-01-01"},
			{ID: "TSTENT-002", Revision: 0, State: "P", Body: "Published body", AddedBy: "test@example.com", AddedAt: "2026-01-01"},
			{ID: "TSTENT-003", Revision: 0, State: "I", Body: "Implemented body", AddedBy: "test@example.com", AddedAt: "2026-01-01"},
		},
		Notes: []entity.Note{
			{ID: "TSTENT-001/01", RuleRef: "TSTENT-001", Type: "changelog", Content: "Initial", AddedBy: "test@example.com", AddedAt: "2026-01-01"},
		},
	}
}

// makeHead clones base and bumps version + timestamp (the minimum valid head).
func makeHead(base *entity.File) *entity.File {
	rules := make([]entity.Rule, len(base.Rules))
	copy(rules, base.Rules)
	notes := make([]entity.Note, len(base.Notes))
	copy(notes, base.Notes)
	return &entity.File{
		Schema:  base.Schema,
		Entity:  base.Entity,
		RuleSet: entity.RuleSet{Version: "0.2.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules:   rules,
		Notes:   notes,
	}
}

func assertHasCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return
		}
	}
	t.Errorf("expected diagnostic %q, got: %v", code, res.Diagnostics)
}

func assertNoCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			t.Errorf("unexpected diagnostic %q: %s", code, d.Message)
		}
	}
}

func assertCodeSeverity(t *testing.T, res *result.Result, code string, sev result.Severity) {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			if d.Severity != sev {
				t.Errorf("diagnostic %q has severity %v, want %v", code, d.Severity, sev)
			}
			return
		}
	}
	t.Errorf("expected diagnostic %q (severity %v), got: %v", code, sev, res.Diagnostics)
}

// --- valid cases ---

func TestDiff_Valid_NoChanges(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	res := d.Diff(base, head)
	if !res.OK() {
		t.Errorf("expected OK for unchanged rules: %v", res.Diagnostics)
	}
}

func TestDiff_Valid_PromoteDraftToPublished(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].State = "P" // TSTENT-001: D → P
	res := d.Diff(base, head)
	if !res.OK() {
		t.Errorf("expected OK for D→P promotion: %v", res.Diagnostics)
	}
}

func TestDiff_Valid_EditDraftWithRevisionBump(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Body = "Updated draft body"
	head.Rules[0].Revision = 1
	res := d.Diff(base, head)
	if !res.OK() {
		t.Errorf("expected OK for draft edit with revision bump: %v", res.Diagnostics)
	}
}

func TestDiff_Valid_AddRule(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "D",
		Body: "New rule", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := d.Diff(base, head)
	if !res.OK() {
		t.Errorf("expected OK for adding a new rule: %v", res.Diagnostics)
	}
}

// --- RD001 ---

func TestDiff_RD001_BodyEditedRevisionNotBumped(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Body = "Changed body" // revision stays 0
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD001")
}

func TestDiff_RD001_BodyEditedAndStateChanged(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Body = "Changed body"
	head.Rules[0].Revision = 1
	head.Rules[0].State = "P" // simultaneous edit+transition not allowed
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD001")
}

// --- RD002 ---

func TestDiff_RD002_NonDraftBodyChanged(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[1].Body = "Changed published body" // TSTENT-002 is state P
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD002")
}

// --- RD003 ---

func TestDiff_RD003_BadStateTransition(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].State = "I" // D → I (skips P)
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD003")
}

func TestDiff_RD003_TerminalStateTransition(t *testing.T) {
	d := New()
	base := makeBase()
	// Make a terminated rule in base.
	base.Rules[0].State = "T"
	head := makeHead(base)
	head.Rules[0].State = "D" // T → D is invalid
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD003")
}

// --- RD004 ---

func TestDiff_RD004_NewRuleNonZeroRevision(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 2, State: "D",
		Body: "New rule", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD004")
}

func TestDiff_RD004_NewRuleNonDraftState(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "P",
		Body: "New rule", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD004")
}

// --- RD005 ---

func TestDiff_RD005_RuleRemoved(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules = head.Rules[:2] // remove TSTENT-003
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD005")
}

// --- RD006 ---

func TestDiff_RD006_SupersededNotRetired(t *testing.T) {
	d := New()
	// Base: TSTENT-004 is P and supersedes TSTENT-003.
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "P",
		Body: "Superseding rule", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-003"),
	})
	// Head: TSTENT-004 promoted to I, but TSTENT-003 stays I (not moved to R).
	head := makeHead(base)
	head.Rules[3].State = "I" // TSTENT-004: P → I
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD006")
}

func TestDiff_RD006_Valid_SupersededRetired(t *testing.T) {
	d := New()
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "P",
		Body: "Superseding rule", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-003"),
	})
	head := makeHead(base)
	head.Rules[2].State = "R" // TSTENT-003: I → R
	head.Rules[3].State = "I" // TSTENT-004: P → I
	res := d.Diff(base, head)
	assertNoCode(t, res, "RD006")
}

// --- RD007 ---

func TestDiff_RD007_VersionNotBumped(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.RuleSet.Version = "0.1.0" // same as base
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD007")
}

func TestDiff_RD007_VersionDowngraded(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.RuleSet.Version = "0.0.9" // lower than base
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD007")
}

// --- RD008 ---

func TestDiffBytes_RD008_HashMismatch(t *testing.T) {
	base := makeBase()
	head := makeHead(base)

	baseData, _ := json.Marshal(base)

	// Marshal head, inject a wrong hash.
	headData, _ := json.Marshal(head)
	var raw map[string]any
	if err := json.Unmarshal(headData, &raw); err != nil {
		t.Fatal(err)
	}
	rs := raw["rule_set"].(map[string]any)
	rs["hash"] = "blake3:0000000000000000000000000000000000000000000000000000000000000000"
	headData, _ = json.Marshal(raw)

	d := New()
	res := d.DiffBytes(baseData, headData)
	assertHasCode(t, res, "RD008")
}

func TestDiffBytes_RD008_NilHashSkipped(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	baseData, _ := json.Marshal(base)
	headData, _ := json.Marshal(head) // hash is nil → skip check
	d := New()
	res := d.DiffBytes(baseData, headData)
	assertNoCode(t, res, "RD008")
}

func TestDiffBytes_RD008_ValidHash(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	baseData, _ := json.Marshal(base)
	headData, _ := json.Marshal(head)

	// Compute the correct hash and inject it.
	hash, err := entity.ComputeHash(headData)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(headData, &raw); err != nil {
		t.Fatal(err)
	}
	raw["rule_set"].(map[string]any)["hash"] = hash
	headData, _ = json.Marshal(raw)

	d := New()
	res := d.DiffBytes(baseData, headData)
	assertNoCode(t, res, "RD008")
}

// --- RD009 ---

func TestDiff_RD009_TimestampNotUpdated(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.RuleSet.Timestamp = base.RuleSet.Timestamp // same
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD009")
}

// --- RD010 ---

func TestDiff_RD010_IDReuse(t *testing.T) {
	// RD010 fires when checkNewRule sees a new rule whose number is in baseNums.
	// Simulate by calling checkNewRule directly with a baseNums set that contains the number.
	res := &result.Result{}
	h := entity.Rule{
		ID: "TSTENT-003", Revision: 0, State: "D",
		Body: "Replacement", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	}
	baseNums := map[int]bool{3: true} // number 003 was used in base
	checkNewRule(h, baseNums, res, "TSTENT/TSTENT-003")
	assertHasCode(t, res, "RD010")
}

func TestExtractRuleNum(t *testing.T) {
	cases := []struct {
		id   string
		want int
	}{
		{"TSTENT-001", 1},
		{"TSTENT-042", 42},
		{"TSTENT-999", 999},
		{"invalid", -1},
		{"", -1},
	}
	for _, tc := range cases {
		got := extractRuleNum(tc.id)
		if got != tc.want {
			t.Errorf("extractRuleNum(%q) = %d, want %d", tc.id, got, tc.want)
		}
	}
}

// --- RD011 ---

func TestDiff_RD011_NewRuleMissingAddedBy(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "D",
		Body: "New rule", AddedBy: "", AddedAt: "2026-02-01", // missing added_by
	})
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD011")
}

func TestDiff_RD011_NewNoteMissingAddedBy(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Notes = append(head.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
		Content: "Because", AddedBy: "", AddedAt: "2026-02-01", // missing added_by
	})
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD011")
}

// --- RD012 ---

func TestDiff_RD012_AppendOnlyNoteRemoved(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	head.Notes = nil // remove the changelog note
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD012")
}

func TestDiff_RD012_NonAppendOnlyNoteRemovalAllowed(t *testing.T) {
	d := New()
	base := makeBase()
	// Add a non-append-only note to base.
	base.Notes = append(base.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
		Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-01-01",
	})
	head := makeHead(base)
	head.Notes = head.Notes[:1] // remove the rationale note, keep changelog
	res := d.Diff(base, head)
	assertNoCode(t, res, "RD012") // rationale is not append-only
}

// --- DiffNew ---

func TestDiffNew_Valid(t *testing.T) {
	d := New()
	head := &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "NEWENT", Title: "New Entity"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "D", Body: "First rule", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
		},
		Notes: []entity.Note{
			{ID: "NEWENT-001/01", RuleRef: "NEWENT-001", Type: "rationale", Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
		},
	}
	res := d.DiffNew(head)
	if !res.OK() {
		t.Errorf("expected OK for valid new entity: %v", res.Diagnostics)
	}
}

func TestDiffNew_RD004_BadRevision(t *testing.T) {
	d := New()
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 1, State: "D", Body: "Rule", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
		},
	}
	res := d.DiffNew(head)
	assertHasCode(t, res, "RD004")
}

func TestDiffNew_RD004_BadState(t *testing.T) {
	d := New()
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "P", Body: "Rule", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
		},
	}
	res := d.DiffNew(head)
	assertHasCode(t, res, "RD004")
}

func TestDiffNew_RD011_MissingAddedBy(t *testing.T) {
	d := New()
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "D", Body: "Rule", AddedBy: "", AddedAt: "2026-02-01"},
		},
	}
	res := d.DiffNew(head)
	assertHasCode(t, res, "RD011")
}

// --- semver ---

func TestIsNewerSemver(t *testing.T) {
	cases := []struct {
		base, head string
		want       bool
	}{
		{"0.1.0", "0.2.0", true},
		{"0.1.0", "1.0.0", true},
		{"1.0.0", "1.0.1", true},
		{"0.1.0", "0.1.0", false}, // equal
		{"0.2.0", "0.1.0", false}, // downgrade
		{"1.0.0", "0.9.9", false}, // downgrade
		{"bad", "0.1.0", false},   // unparseable base
		{"0.1.0", "bad", false},   // unparseable head
	}
	for _, tc := range cases {
		got := isNewerSemver(tc.base, tc.head)
		if got != tc.want {
			t.Errorf("isNewerSemver(%q, %q) = %v, want %v", tc.base, tc.head, got, tc.want)
		}
	}
}

// --- nil guard tests (Fix 2) ---

func TestDiff_NilBase(t *testing.T) {
	d := New()
	res := d.Diff(nil, makeBase())
	assertHasCode(t, res, "RD000")
}

func TestDiff_NilHead(t *testing.T) {
	d := New()
	res := d.Diff(makeBase(), nil)
	assertHasCode(t, res, "RD000")
}

func TestDiffNew_NilHead(t *testing.T) {
	d := New()
	res := d.DiffNew(nil)
	assertHasCode(t, res, "RD000")
}

// --- semver pre-release ordering (Fix 1 — Masterminds) ---

func TestIsNewerSemver_PreRelease(t *testing.T) {
	// Per semver spec: pre-release < release, so 1.0.0-alpha → 1.0.0 is a valid bump.
	if !isNewerSemver("1.0.0-alpha", "1.0.0") {
		t.Error("expected true: 1.0.0-alpha < 1.0.0")
	}
	// Bumping between pre-releases is also valid.
	if !isNewerSemver("1.0.0-alpha", "1.0.0-beta") {
		t.Error("expected true: 1.0.0-alpha < 1.0.0-beta")
	}
	// Release → pre-release of next version: not a bump (same numeric components).
	if isNewerSemver("1.0.0", "1.0.0-alpha") {
		t.Error("expected false: 1.0.0 > 1.0.0-alpha")
	}
}

// --- RD012 type mutation bypass (Fix 3) ---

func TestDiff_RD012_TypeMutation(t *testing.T) {
	d := New()
	base := makeBase()
	head := makeHead(base)
	// Change the changelog note's type to a non-append-only type.
	head.Notes[0].Type = "general"
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD012")
}

// --- RD006 is Warning (spec allows linked follow-up PR) ---

func TestDiff_RD006_IsWarning(t *testing.T) {
	d := New()
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "P",
		Body: "Superseding rule", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-003"),
	})
	head := makeHead(base)
	head.Rules[3].State = "I" // TSTENT-004: P → I; TSTENT-003 stays I (not yet R)
	res := d.Diff(base, head)
	assertCodeSeverity(t, res, "RD006", result.Warning)
}

// --- RD004 sequential numbering ---

func TestDiff_RD004_NonSequentialNumber(t *testing.T) {
	d := New()
	base := makeBase() // rules: 001, 002, 003
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-005", Revision: 0, State: "D", // skips 004
		Body: "New rule", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD004")
}

func TestDiff_RD004_Sequential_Valid(t *testing.T) {
	d := New()
	base := makeBase() // rules: 001, 002, 003
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "D",
		Body: "New rule", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := d.Diff(base, head)
	assertNoCode(t, res, "RD004")
}

// --- sub-entity deletion ---

func TestDiff_SubEntityDeleted(t *testing.T) {
	d := New()
	base := makeBase()
	base.SubEntities = []entity.File{
		{
			Entity:  entity.Entity{ID: "TSTENT-SUB1", Title: "Sub"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		},
	}
	head := makeHead(base)
	head.SubEntities = nil // delete the sub-entity
	res := d.Diff(base, head)
	assertHasCode(t, res, "RD005")
}

// --- strict mode ---

func TestDiff_StrictMode_PromotesWarnings(t *testing.T) {
	// RD006 is a Warning; strict mode promotes it to Error.
	d := New(WithStrict(true))
	base := makeBase()
	base.Rules = append(base.Rules, entity.Rule{
		ID: "TSTENT-004", Revision: 0, State: "P",
		Body: "Superseding rule", AddedBy: "test@example.com", AddedAt: "2026-01-01",
		Supersedes: ptr("TSTENT-003"),
	})
	head := makeHead(base)
	head.Rules[3].State = "I"
	res := d.Diff(base, head)
	assertCodeSeverity(t, res, "RD006", result.Error)
}
