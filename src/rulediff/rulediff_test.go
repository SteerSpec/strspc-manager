package rulediff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	assertNoCode(t, res, "RD019")
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
	assertNoCode(t, res, "RD019")
}

// --- RD019 ---

func TestDiff_RD019_AddedByFormat(t *testing.T) {
	tests := []struct {
		addedBy string
		want    bool
	}{
		{"@pleberre", false},
		{"@a-b-1", false},
		{"@steerspec-sync[bot]", false},
		{"dev@example.com", false},
		{"first.last+tag@mail.example.com", false},
		{"pleberre", true},
		{"@", true},
		{"@-x", true},
		{"@x-", true},
		{"@a--b", true},
		{"@a b", true},
		{"a..b@example.com", true},
		{"a@b..c", true},
		{"dev@-example.com", true},
		{"dev@example-.com", true},
		{"dev@example.-com", true},
		{"dev@my-example.com", false},
		{"dev@" + strings.Repeat("a", 63) + ".com", false},
		{"dev@" + strings.Repeat("a", 64) + ".com", true},
		{"dev@" + strings.Repeat(strings.Repeat("a", 61)+".", 4) + "com", false}, // 251 characters
		{"dev@" + strings.Repeat(strings.Repeat("a", 63)+".", 4) + "com", true},  // 259 characters
		{"<dev@example.com>", true},
		{" @pleberre", true},
		{"dev@example", true},
		{"a@b@c.d", true},
		{"PLB <dev@example.com>", true},
	}
	for _, tt := range tests {
		t.Run(tt.addedBy, func(t *testing.T) {
			base := makeBase()
			head := makeHead(base)
			head.Rules = append(head.Rules, entity.Rule{
				ID: "TSTENT-004", Revision: 0, State: "D",
				Body: "New rule", AddedBy: tt.addedBy, AddedAt: "2026-02-01",
			})
			head.Notes = append(head.Notes, entity.Note{
				ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
				Content: "Because", AddedBy: tt.addedBy, AddedAt: "2026-02-01",
			})
			res := New().Diff(base, head)
			got := 0
			for _, d := range res.Diagnostics {
				if d.Code == "RD019" {
					got++
				}
			}
			want := 0
			if tt.want {
				want = 2 // the rule and the note
			}
			if got != want {
				t.Errorf("RD019 count = %d, want %d: %v", got, want, res.Diagnostics)
			}
		})
	}
}

func TestDiff_RD019_ExistingEntriesNotChecked(t *testing.T) {
	base := makeBase()
	base.Rules[0].AddedBy = "pleberre"
	head := makeHead(base)
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD019")
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
	assertHasCode(t, res, "RD014")
}

// --- RD013-RD016: note versioning (NTE-008..010) ---

// makeBaseWithRationale returns makeBase plus a rationale note TSTENT-001/02
// at the given revision.
func makeBaseWithRationale(revision int) *entity.File {
	base := makeBase()
	base.Notes = append(base.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
		Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-01-01", Revision: revision,
	})
	return base
}

func TestDiff_RD013_NoteRevision(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(n *entity.Note)
		wantErr bool
	}{
		{"content edit with revision+1", func(n *entity.Note) { n.Content = "Why, revised"; n.Revision = 2 }, false},
		{"type change with revision+1", func(n *entity.Note) { n.Type = "clarification"; n.Revision = 2 }, false},
		{"content edit without revision", func(n *entity.Note) { n.Content = "Why, revised" }, true},
		{"type change without revision", func(n *entity.Note) { n.Type = "clarification" }, true},
		{"content edit with revision+2", func(n *entity.Note) { n.Content = "Why, revised"; n.Revision = 3 }, true},
		{"revision bump without change", func(n *entity.Note) { n.Revision = 2 }, true},
		{"unchanged note", func(n *entity.Note) {}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := makeBaseWithRationale(1)
			head := makeHead(base)
			tt.mutate(&head.Notes[1])
			res := New().Diff(base, head)
			if tt.wantErr {
				assertHasCode(t, res, "RD013")
				assertCodeSeverity(t, res, "RD013", result.Error)
			} else if !res.OK() {
				t.Errorf("expected OK: %v", res.Diagnostics)
			}
		})
	}
}

func TestDiff_RD013_NotForAppendOnly(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes[0].Content = "Rewritten"
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD013")
	assertHasCode(t, res, "RD016")
}

func TestDiff_RD013_AppendOnlyRevisionDrift(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes[0].Revision = 1
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD013")
	assertNoCode(t, res, "RD016")
}

func TestDiff_RD014_NoteRemoved(t *testing.T) {
	base := makeBaseWithRationale(0)
	head := makeHead(base)
	head.Notes = head.Notes[:1]
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD014")
	assertCodeSeverity(t, res, "RD014", result.Error)
}

func TestDiff_RD014_AppendOnlyRemovalReportedOnce(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes = nil
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD012")
	assertNoCode(t, res, "RD014")
}

func TestDiff_RD015_NewNoteRevisionNonZero(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes = append(head.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
		Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-02-01", Revision: 1,
	})
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD015")
	assertCodeSeverity(t, res, "RD015", result.Error)
}

func TestDiff_RD015_NewNoteRevisionZeroOK(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes = append(head.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale",
		Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-02-01",
	})
	res := New().Diff(base, head)
	if !res.OK() {
		t.Errorf("expected OK: %v", res.Diagnostics)
	}
}

func TestDiffNew_RD015_NewEntityNoteRevisionNonZero(t *testing.T) {
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New Entity"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "D", Body: "First rule", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
		},
		Notes: []entity.Note{
			{ID: "NEWENT-001/01", RuleRef: "NEWENT-001", Type: "rationale", Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-02-01", Revision: 1},
		},
	}
	assertHasCode(t, New().DiffNew(head), "RD015")
}

func TestDiff_RD015_NewSubEntityNoteRevisionNonZero(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.SubEntities = []entity.File{
		{
			Entity:  entity.Entity{ID: "TSTSUB", Title: "Sub", Parent: "TSTENT"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-02-01T00:00:00Z"},
			Rules: []entity.Rule{
				{ID: "TSTSUB-001", Revision: 0, State: "D", Body: "Sub rule", AddedBy: "test@example.com", AddedAt: "2026-02-01"},
			},
			Notes: []entity.Note{
				{ID: "TSTSUB-001/01", RuleRef: "TSTSUB-001", Type: "rationale", Content: "Why", AddedBy: "test@example.com", AddedAt: "2026-02-01", Revision: 2},
			},
		},
	}
	assertHasCode(t, New().Diff(base, head), "RD015")
}

func TestDiff_RD016_AppendOnlyNoteContentChanged(t *testing.T) {
	for _, revision := range []int{0, 1} {
		base := makeBase()
		head := makeHead(base)
		head.Notes[0].Content = "Rewritten"
		head.Notes[0].Revision = revision
		res := New().Diff(base, head)
		assertHasCode(t, res, "RD016")
		assertCodeSeverity(t, res, "RD016", result.Error)
		assertNoCode(t, res, "RD012")
	}
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

// --- sub-entity version/timestamp not required (RD007/RD009 top-level only) ---

func TestDiff_UnchangedSubEntityNoVersionError(t *testing.T) {
	d := New()
	base := makeBase()
	base.SubEntities = []entity.File{
		{
			Entity:  entity.Entity{ID: "TSTENT-SUB1", Title: "Sub"},
			RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		},
	}
	head := makeHead(base) // top-level version/timestamp bumped; sub-entity unchanged
	head.SubEntities = base.SubEntities
	res := d.Diff(base, head)
	// Sub-entity has same version/timestamp as base — must NOT trigger RD007/RD009.
	if !res.OK() {
		t.Errorf("expected no errors for unchanged sub-entity: %v", res.Diagnostics)
	}
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

// --- package-level Compare / CompareNew / CompareDir ---

func TestCompare_Valid(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	baseData, _ := json.Marshal(base)
	headData, _ := json.Marshal(head)
	res := Compare(baseData, headData)
	if !res.OK() {
		t.Errorf("expected OK: %v", res.Diagnostics)
	}
}

func TestCompareNew_Valid(t *testing.T) {
	head := &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-03-24T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "D", Body: "Rule", AddedBy: "test@example.com", AddedAt: "2026-03-24"},
		},
	}
	headData, _ := json.Marshal(head)
	res := CompareNew(headData)
	if !res.OK() {
		t.Errorf("expected OK: %v", res.Diagnostics)
	}
}

func TestCompareNew_BadRevision(t *testing.T) {
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-03-24T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 1, State: "D", Body: "Rule", AddedBy: "test@example.com", AddedAt: "2026-03-24"},
		},
	}
	headData, _ := json.Marshal(head)
	res := CompareNew(headData)
	assertHasCode(t, res, "RD004")
}

func TestCompareDir_Valid(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()

	base := makeBase()
	head := makeHead(base)
	baseData, _ := json.Marshal(base)
	headData, _ := json.Marshal(head)

	if err := os.WriteFile(filepath.Join(baseDir, "TSTENT.json"), baseData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(headDir, "TSTENT.json"), headData, 0o600); err != nil {
		t.Fatal(err)
	}

	res := CompareDir(baseDir, headDir)
	if !res.OK() {
		t.Errorf("expected OK: %v", res.Diagnostics)
	}
}

func TestCompareDir_DeletedFile(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()

	base := makeBase()
	baseData, _ := json.Marshal(base)
	if err := os.WriteFile(filepath.Join(baseDir, "TSTENT.json"), baseData, 0o600); err != nil {
		t.Fatal(err)
	}
	// headDir is empty — file was deleted

	res := CompareDir(baseDir, headDir)
	assertHasCode(t, res, "RD005")
}

func TestCompareDir_NewFile(t *testing.T) {
	baseDir := t.TempDir()
	headDir := t.TempDir()

	// New file only in headDir
	head := &entity.File{
		Entity:  entity.Entity{ID: "NEWENT", Title: "New"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-03-24T00:00:00Z"},
		Rules: []entity.Rule{
			{ID: "NEWENT-001", Revision: 0, State: "D", Body: "Rule", AddedBy: "test@example.com", AddedAt: "2026-03-24"},
		},
	}
	headData, _ := json.Marshal(head)
	if err := os.WriteFile(filepath.Join(headDir, "NEWENT.json"), headData, 0o600); err != nil {
		t.Fatal(err)
	}

	res := CompareDir(baseDir, headDir)
	if !res.OK() {
		t.Errorf("expected OK for valid new file: %v", res.Diagnostics)
	}
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

func TestDiff_RD017_InheritsChanged(t *testing.T) {
	base := makeBase()
	base.Entity.Inherits = "dev.example.core:ACCT"
	head := makeHead(base)
	head.Entity.Inherits = "dev.example.core:LOAN"
	res := New().Diff(base, head)
	assertCodeSeverity(t, res, "RD017", result.Error)
}

func TestDiff_RD017_InheritsRemoved(t *testing.T) {
	base := makeBase()
	base.Entity.Inherits = "ACCT"
	head := makeHead(base)
	head.Entity.Inherits = ""
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD017")
}

func TestDiff_RD017_Valid_InheritsAdded(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Entity.Inherits = "ACCT"
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD017")
}

func TestDiff_RD017_Valid_AllDraft(t *testing.T) {
	base := makeBase()
	base.Rules = base.Rules[:1] // TSTENT-001 only, in D
	base.Entity.Inherits = "ACCT"
	head := makeHead(base)
	head.Entity.Inherits = "LOAN"
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD017")
}

func TestDiff_RD017_SubEntity(t *testing.T) {
	sub := entity.File{
		Entity: entity.Entity{ID: "TSTSUB", Title: "Sub", Parent: "TSTENT", Inherits: "ACCT"},
		Rules: []entity.Rule{
			{ID: "TSTSUB-001", Revision: 0, State: "I", Body: "Sub rule", AddedBy: "test@example.com", AddedAt: "2026-01-01"},
		},
	}
	base := makeBase()
	base.SubEntities = []entity.File{sub}
	head := makeHead(base)
	headSub := sub
	headSub.Entity.Inherits = "LOAN"
	head.SubEntities = []entity.File{headSub}
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD017")
}

func TestDiff_RD018_OverridesImmutable(t *testing.T) {
	tests := []struct {
		name       string
		idx        int // 0 = TSTENT-001 (D), 2 = TSTENT-003 (I)
		base, head *string
		want       bool
	}{
		{"added on implemented rule", 2, nil, ptr("core:ACCT-005"), true},
		{"removed from implemented rule", 2, ptr("core:ACCT-005"), nil, true},
		{"retargeted on implemented rule", 2, ptr("core:ACCT-005"), ptr("core:ACCT-006"), true},
		{"unchanged on implemented rule", 2, ptr("core:ACCT-005"), ptr("core:ACCT-005"), false},
		{"added on draft rule", 0, nil, ptr("core:ACCT-005"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := makeBase()
			base.Rules[tt.idx].Overrides = tt.base
			head := makeHead(base)
			head.Rules[tt.idx].Overrides = tt.head
			res := New().Diff(base, head)
			if tt.want {
				assertCodeSeverity(t, res, "RD018", result.Error)
			} else {
				assertNoCode(t, res, "RD018")
			}
		})
	}
}

func TestDiff_RD020_Visibility(t *testing.T) {
	tests := []struct {
		name       string
		base, head string
		sev        result.Severity
		substr     string
	}{
		{"added at the same width", "", "public", result.Info, "absent to `public`"},
		{"added and narrowed", "", "protected", result.Info, "MAJOR"},
		{"narrowed", "protected", "private", result.Info, "MAJOR"},
		{"widened", "private", "protected", result.Info, "MINOR"},
		{"removed", "protected", "", result.Error, "set `public`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := makeBase()
			base.Entity.Visibility = tt.base
			head := makeHead(base)
			head.Entity.Visibility = tt.head
			var got []result.Diagnostic
			for _, d := range New().Diff(base, head).Diagnostics {
				if d.Code == "RD020" {
					got = append(got, d)
				}
			}
			if len(got) != 1 || got[0].Severity != tt.sev || !strings.Contains(got[0].Message, tt.substr) {
				t.Errorf("RD020 diagnostics = %v, want one %s with %q", got, tt.sev, tt.substr)
			}
		})
	}
}

func TestDiff_RD020_Unchanged(t *testing.T) {
	base := makeBase()
	base.Entity.Visibility = "protected"
	head := makeHead(base)
	head.Entity.Visibility = "protected"
	assertNoCode(t, New().Diff(base, head), "RD020")
}

func TestDiff_RD020_InfoStaysInfoInStrictMode(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Entity.Visibility = "private"
	res := New(WithStrict(true)).Diff(base, head)
	if !res.OK() {
		t.Errorf("a visibility change must not fail strict mode: %v", res.Diagnostics)
	}
}

func TestDiff_RD020_SubEntity(t *testing.T) {
	base := makeBase()
	base.Entity.Visibility = "protected"
	base.SubEntities = []entity.File{{Entity: entity.Entity{ID: "SUB", Title: "SUB", Parent: base.Entity.ID}}}
	head := makeHead(base)
	head.Entity.Visibility = "protected"
	head.SubEntities = []entity.File{{Entity: entity.Entity{ID: "SUB", Title: "SUB", Parent: base.Entity.ID, Visibility: "private"}}}
	var found bool
	for _, d := range New().Diff(base, head).Diagnostics {
		// Absent takes the container's `protected`, so this narrows.
		if d.Code == "RD020" && strings.Contains(d.Path, "SUB") && strings.Contains(d.Message, "MAJOR") {
			found = true
		}
	}
	if !found {
		t.Error("expected RD020 narrowing the sub-entity")
	}
}

func TestDiff_RD017_RemovedFromEntityOutOfForce(t *testing.T) {
	for _, state := range []string{entity.StateAbandoned, entity.StateRetired, entity.StateTerminated} {
		base := makeBase()
		base.Entity.Inherits = "core:ACCT"
		for i := range base.Rules {
			base.Rules[i].State = state
		}
		head := makeHead(base)
		head.Entity.Inherits = ""
		assertNoCode(t, New().Diff(base, head), "RD017")
	}
}

func TestDiff_RD017_ChangedOnEntityOutOfForce(t *testing.T) {
	base := makeBase()
	base.Entity.Inherits = "core:ACCT"
	for i := range base.Rules {
		base.Rules[i].State = entity.StateRetired
	}
	head := makeHead(base)
	head.Entity.Inherits = "core:OTHER"
	assertHasCode(t, New().Diff(base, head), "RD017")
}

func TestRD021_RevisionMovedWithoutBody(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[1].Revision = 1
	assertHasCode(t, New().Diff(base, head), "RD021")
}

func TestRD021_DraftBodyEditStaysClean(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Body = "Draft body, edited"
	head.Rules[0].Revision = 1
	assertNoCode(t, New().Diff(base, head), "RD021")
}

func TestRD021_MappingOnImplementedRule(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Notes = append(head.Notes, entity.Note{
		ID: "TSTENT-003/01", RuleRef: "TSTENT-003", Type: entity.NoteTypeStandard, Content: "owasp.asvs:2.1.1",
		AddedBy: "test@example.com", AddedAt: "2026-01-01",
	})
	res := New().Diff(base, head)
	assertNoCode(t, res, "RD021")
	assertNoCode(t, res, "RD022")
}

func TestRD022_StandardRetype(t *testing.T) {
	note := func(typ, content string, rev int) entity.Note {
		return entity.Note{
			ID: "TSTENT-003/01", RuleRef: "TSTENT-003", Type: typ, Content: content,
			AddedBy: "test@example.com", AddedAt: "2026-01-01", Revision: rev,
		}
	}
	const mapping = "owasp.asvs:2.1.1"
	tests := []struct {
		name      string
		from, to  entity.Note
		wantRD022 bool
	}{
		{"withdrawal", note("standard", mapping, 0), note("reference", mapping, 1), false},
		{"restored", note("reference", mapping, 0), note("standard", mapping, 1), false},
		{"withdrawal changes content", note("standard", mapping, 0), note("reference", "gone", 1), true},
		{"standard to rationale", note("standard", mapping, 0), note("rationale", mapping, 1), true},
		{"rationale to standard", note("rationale", mapping, 0), note("standard", mapping, 1), true},
		{"clause revised", note("standard", mapping, 0), note("standard", "owasp.asvs:2.1.2", 1), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := makeBase()
			base.Notes = append(base.Notes, tt.from)
			head := makeHead(base)
			head.Notes[1] = tt.to
			res := New().Diff(base, head)
			if tt.wantRD022 {
				assertHasCode(t, res, "RD022")
			} else {
				assertNoCode(t, res, "RD022")
			}
		})
	}
}

// An `overrides` or `supersedes` edit on a Draft rule is an edit to the rule.
func TestRD021_DraftReferenceEdits(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].Overrides = ptr("OTHER-001")
	head.Rules[0].Revision = 1
	assertNoCode(t, New().Diff(base, head), "RD021")

	head = makeHead(base)
	head.Rules[0].Supersedes = ptr("TSTENT-002")
	head.Rules[0].Revision = 1
	assertNoCode(t, New().Diff(base, head), "RD021")
}

// Only a Draft rule can be edited, and each edit is one revision.
func TestRD021_ReferenceEditsOutsideDraft(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[1].Supersedes = ptr("TSTENT-001")
	head.Rules[1].Revision = 1
	assertHasCode(t, New().Diff(base, head), "RD021")

	head = makeHead(base)
	head.Rules[0].Overrides = ptr("OTHER-001")
	head.Rules[0].Revision = 7
	assertHasCode(t, New().Diff(base, head), "RD021")
}
