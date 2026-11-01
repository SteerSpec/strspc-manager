package entityops

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func init() {
	// Pin time for deterministic tests.
	nowFunc = func() time.Time {
		return time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	}
}

func TestValidateEUID(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{"valid 3-char", "TST", false},
		{"valid 18-char", "ABCDEFGHIJKLMNOPQR", false},
		{"valid mixed", "Ab1Cd2", false},
		{"empty", "", true},
		{"too short", "AB", true},
		{"too long", "ABCDEFGHIJKLMNOPQRS", true},
		{"special chars", "AB-CD", true},
		{"spaces", "AB CD", true},
		{"underscore", "AB_CD", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEUID(tt.id)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEUID(%q) error = %v, wantErr %v", tt.id, err, tt.wantErr)
			}
		})
	}
}

func TestNewFile(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		f, err := NewFile("TST", "Test Entity", "A test entity", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.Entity.ID != "TST" {
			t.Errorf("ID = %q, want %q", f.Entity.ID, "TST")
		}
		if f.Entity.Title != "Test Entity" {
			t.Errorf("Title = %q, want %q", f.Entity.Title, "Test Entity")
		}
		if f.RuleSet.Version != "0.1.0" {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, "0.1.0")
		}
		if f.RuleSet.Hash == nil {
			t.Fatal("Hash should be set")
		}
		if !strings.HasPrefix(*f.RuleSet.Hash, "blake3:") {
			t.Errorf("Hash = %q, want blake3: prefix", *f.RuleSet.Hash)
		}
		if len(f.Rules) != 0 {
			t.Errorf("Rules = %d, want 0", len(f.Rules))
		}
		if len(f.Notes) != 0 {
			t.Errorf("Notes = %d, want 0", len(f.Notes))
		}
	})

	t.Run("with parent", func(t *testing.T) {
		f, err := NewFile("SUB", "Sub Entity", "", "TST")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f.Entity.Parent != "TST" {
			t.Errorf("Parent = %q, want %q", f.Entity.Parent, "TST")
		}
	})

	t.Run("invalid EUID", func(t *testing.T) {
		_, err := NewFile("AB", "Too Short", "", "")
		if err == nil {
			t.Fatal("expected error for invalid EUID")
		}
	})
}

func TestAddRule(t *testing.T) {
	t.Run("sequential IDs", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}

		id1, err := AddRule(f, "First rule body", "@user")
		if err != nil {
			t.Fatalf("AddRule 1: %v", err)
		}
		if id1 != "TST-001" {
			t.Errorf("first rule ID = %q, want %q", id1, "TST-001")
		}

		id2, err := AddRule(f, "Second rule body", "@user")
		if err != nil {
			t.Fatalf("AddRule 2: %v", err)
		}
		if id2 != "TST-002" {
			t.Errorf("second rule ID = %q, want %q", id2, "TST-002")
		}
	})

	t.Run("rule properties", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}

		_, err = AddRule(f, "A rule", "@author")
		if err != nil {
			t.Fatalf("AddRule: %v", err)
		}

		r := f.Rules[0]
		if r.State != entity.StateDraft {
			t.Errorf("State = %q, want %q", r.State, entity.StateDraft)
		}
		if r.Revision != 0 {
			t.Errorf("Revision = %d, want 0", r.Revision)
		}
		if r.AddedBy != "@author" {
			t.Errorf("AddedBy = %q, want %q", r.AddedBy, "@author")
		}
		if r.AddedAt != "2026-03-23" {
			t.Errorf("AddedAt = %q, want %q", r.AddedAt, "2026-03-23")
		}
	})

	t.Run("patch bump", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		v0 := f.RuleSet.Version

		_, err = AddRule(f, "A rule", "@user")
		if err != nil {
			t.Fatalf("AddRule: %v", err)
		}

		expected, _ := BumpPatch(v0)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("empty body rejected", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		_, err = AddRule(f, "", "@user")
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})

	t.Run("empty addedBy rejected", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		_, err = AddRule(f, "body", "")
		if err == nil {
			t.Fatal("expected error for empty addedBy")
		}
	})
}

func TestUpdateRuleBody(t *testing.T) {
	t.Run("updates draft", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		id, err := AddRule(f, "original", "@user")
		if err != nil {
			t.Fatalf("AddRule: %v", err)
		}

		err = UpdateRuleBody(f, id, "updated")
		if err != nil {
			t.Fatalf("UpdateRuleBody: %v", err)
		}

		if f.Rules[0].Body != "updated" {
			t.Errorf("Body = %q, want %q", f.Rules[0].Body, "updated")
		}
		if f.Rules[0].Revision != 1 {
			t.Errorf("Revision = %d, want 1", f.Rules[0].Revision)
		}
	})

	t.Run("non-draft rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StatePublished)

		err := UpdateRuleBody(f, "TST-001", "new body")
		if err == nil {
			t.Fatal("expected error for non-draft rule")
		}
	})

	t.Run("missing rule rejected", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}

		err = UpdateRuleBody(f, "TST-999", "body")
		if err == nil {
			t.Fatal("expected error for missing rule")
		}
	})

	t.Run("empty body rejected", func(t *testing.T) {
		f, err := NewFile("TST", "Test", "", "")
		if err != nil {
			t.Fatalf("NewFile: %v", err)
		}
		id, err := AddRule(f, "original", "@user")
		if err != nil {
			t.Fatalf("AddRule: %v", err)
		}
		err = UpdateRuleBody(f, id, "")
		if err == nil {
			t.Fatal("expected error for empty body")
		}
	})
}

func TestPromoteRule(t *testing.T) {
	t.Run("D→P", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)
		v := f.RuleSet.Version

		err := PromoteRule(f, "TST-001")
		if err != nil {
			t.Fatalf("PromoteRule: %v", err)
		}
		if f.Rules[0].State != entity.StatePublished {
			t.Errorf("State = %q, want %q", f.Rules[0].State, entity.StatePublished)
		}
		expected, _ := BumpMinor(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("P→I", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StatePublished)

		err := PromoteRule(f, "TST-001")
		if err != nil {
			t.Fatalf("PromoteRule: %v", err)
		}
		if f.Rules[0].State != entity.StateImplemented {
			t.Errorf("State = %q, want %q", f.Rules[0].State, entity.StateImplemented)
		}
	})

	t.Run("I rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateImplemented)

		err := PromoteRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Implemented rule")
		}
	})
}

func TestRetireRule(t *testing.T) {
	t.Run("I→R", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateImplemented)
		v := f.RuleSet.Version

		err := RetireRule(f, "TST-001")
		if err != nil {
			t.Fatalf("RetireRule: %v", err)
		}
		if f.Rules[0].State != entity.StateRetired {
			t.Errorf("State = %q, want %q", f.Rules[0].State, entity.StateRetired)
		}
		expected, _ := BumpMinor(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("R→T", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateRetired)

		err := RetireRule(f, "TST-001")
		if err != nil {
			t.Fatalf("RetireRule: %v", err)
		}
		if f.Rules[0].State != entity.StateTerminated {
			t.Errorf("State = %q, want %q", f.Rules[0].State, entity.StateTerminated)
		}
	})

	t.Run("D rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)

		err := RetireRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Draft rule")
		}
	})
}

func TestAbandonRule(t *testing.T) {
	t.Run("D→A", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)
		v := f.RuleSet.Version

		err := AbandonRule(f, "TST-001")
		if err != nil {
			t.Fatalf("AbandonRule: %v", err)
		}
		if f.Rules[0].State != entity.StateAbandoned {
			t.Errorf("State = %q, want %q", f.Rules[0].State, entity.StateAbandoned)
		}
		expected, _ := BumpPatch(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("P rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StatePublished)

		err := AbandonRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Published rule")
		}
	})
}

func TestSupersedeRule(t *testing.T) {
	t.Run("supersede published", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StatePublished)

		newID, err := SupersedeRule(f, "TST-001", "replacement body", "@user")
		if err != nil {
			t.Fatalf("SupersedeRule: %v", err)
		}
		if newID != "TST-002" {
			t.Errorf("new ID = %q, want %q", newID, "TST-002")
		}
		if len(f.Rules) != 2 {
			t.Fatalf("Rules = %d, want 2", len(f.Rules))
		}
		newRule := f.Rules[1]
		if newRule.State != entity.StateDraft {
			t.Errorf("new rule State = %q, want %q", newRule.State, entity.StateDraft)
		}
		if newRule.Supersedes == nil || *newRule.Supersedes != "TST-001" {
			t.Errorf("Supersedes = %v, want %q", newRule.Supersedes, "TST-001")
		}
		// Old rule unchanged.
		if f.Rules[0].State != entity.StatePublished {
			t.Errorf("old rule State = %q, want %q", f.Rules[0].State, entity.StatePublished)
		}
	})

	t.Run("draft rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)

		_, err := SupersedeRule(f, "TST-001", "body", "@user")
		if err == nil {
			t.Fatal("expected error for Draft rule")
		}
	})

	t.Run("abandoned rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateAbandoned)

		_, err := SupersedeRule(f, "TST-001", "body", "@user")
		if err == nil {
			t.Fatal("expected error for Abandoned rule")
		}
	})

	t.Run("terminated rejected", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateTerminated)

		_, err := SupersedeRule(f, "TST-001", "body", "@user")
		if err == nil {
			t.Fatal("expected error for Terminated rule")
		}
	})
}

func TestNextRuleNumber(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		f := &entity.File{Entity: entity.Entity{ID: "TST"}}
		if n := NextRuleNumber(f); n != 1 {
			t.Errorf("NextRuleNumber = %d, want 1", n)
		}
	})

	t.Run("with gap", func(t *testing.T) {
		f := &entity.File{
			Entity: entity.Entity{ID: "TST"},
			Rules: []entity.Rule{
				{ID: "TST-001"}, {ID: "TST-003"},
			},
		}
		if n := NextRuleNumber(f); n != 4 {
			t.Errorf("NextRuleNumber = %d, want 4", n)
		}
	})
}

func TestBumpPatch(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0.1.0", "0.1.1"},
		{"1.2.3", "1.2.4"},
		{"0.0.0", "0.0.1"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := BumpPatch(tt.in)
			if err != nil {
				t.Fatalf("BumpPatch(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("BumpPatch(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("invalid semver", func(t *testing.T) {
		_, err := BumpPatch("not-semver")
		if err == nil {
			t.Fatal("expected error for invalid semver")
		}
	})
}

func TestBumpMinor(t *testing.T) {
	tests := []struct{ in, want string }{
		{"0.1.0", "0.2.0"},
		{"1.2.3", "1.3.0"},
		{"0.0.0", "0.1.0"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := BumpMinor(tt.in)
			if err != nil {
				t.Fatalf("BumpMinor(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("BumpMinor(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("invalid semver", func(t *testing.T) {
		_, err := BumpMinor("bad")
		if err == nil {
			t.Fatal("expected error for invalid semver")
		}
	})
}

func TestUpdateMeta(t *testing.T) {
	f, err := NewFile("TST", "Test", "", "")
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}

	if f.RuleSet.Timestamp == "" {
		t.Error("Timestamp should be set")
	}
	if f.RuleSet.Hash == nil {
		t.Fatal("Hash should be set")
	}
	if !strings.HasPrefix(*f.RuleSet.Hash, "blake3:") {
		t.Errorf("Hash = %q, want blake3: prefix", *f.RuleSet.Hash)
	}
	if len(*f.RuleSet.Hash) != len("blake3:")+64 {
		t.Errorf("Hash length = %d, want %d", len(*f.RuleSet.Hash), len("blake3:")+64)
	}
}

func TestUpdateMeta_NilFile(t *testing.T) {
	err := UpdateMeta(nil)
	if err == nil || err.Error() != "entity file is nil" {
		t.Errorf("UpdateMeta(nil) error = %v, want entity file is nil", err)
	}
}

func TestUpdateMeta_TracksContent(t *testing.T) {
	f := makeFileWithRule(t, entity.StateDraft)
	before := *f.RuleSet.Hash

	f.RuleSet.Timestamp = ""
	f.Rules[0].Body = "changed body"
	if err := UpdateMeta(f); err != nil {
		t.Fatalf("UpdateMeta: %v", err)
	}

	if f.RuleSet.Timestamp != "2026-03-23T12:00:00Z" {
		t.Errorf("Timestamp = %q, want %q", f.RuleSet.Timestamp, "2026-03-23T12:00:00Z")
	}
	if *f.RuleSet.Hash == before {
		t.Error("Hash should change with the file content")
	}
	assertHashCurrent(t, f)
}

func TestNextRuleNumber_Table(t *testing.T) {
	tests := []struct {
		name string
		f    *entity.File
		want int
	}{
		{"nil file", nil, 1},
		{"only foreign prefixes", fileWithRuleIDs("OTHER-005", "OTHER-900"), 1},
		{"foreign prefix does not raise the maximum", fileWithRuleIDs("TST-002", "OTHER-500"), 3},
		{"prefix match is case sensitive", fileWithRuleIDs("tst-007"), 1},
		{"too few digits", fileWithRuleIDs("TST-1"), 1},
		{"too many digits", fileWithRuleIDs("TST-1000"), 1},
		{"no separator", fileWithRuleIDs("TST001"), 1},
		{"malformed IDs are skipped, valid ones count", fileWithRuleIDs("bad", "TST-004", ""), 5},
		{"highest number wins over file order", fileWithRuleIDs("TST-009", "TST-002"), 10},
		{"past the last valid number", fileWithRuleIDs("TST-999"), 1000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRuleNumber(tt.f); got != tt.want {
				t.Errorf("NextRuleNumber = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseSemver(t *testing.T) {
	valid := []struct {
		in                  string
		major, minor, patch int
	}{
		{"0.0.0", 0, 0, 0},
		{"10.20.30", 10, 20, 30},
		{"1.2.3-rc.1", 1, 2, 3},
		{"1.2.3+build.5", 1, 2, 3},
		{"1.2.3-rc.1+build.5", 1, 2, 3},
	}
	for _, tt := range valid {
		t.Run(tt.in, func(t *testing.T) {
			major, minor, patch, err := parseSemver(tt.in)
			if err != nil {
				t.Fatalf("parseSemver(%q): %v", tt.in, err)
			}
			if major != tt.major || minor != tt.minor || patch != tt.patch {
				t.Errorf("parseSemver(%q) = %d.%d.%d, want %d.%d.%d",
					tt.in, major, minor, patch, tt.major, tt.minor, tt.patch)
			}
		})
	}

	invalid := []struct {
		in      string
		wantErr string
		wantIs  error
	}{
		{in: "", wantErr: `invalid semver: ""`},
		{in: "1.2", wantErr: `invalid semver: "1.2"`},
		{in: ".2.3", wantErr: `invalid semver ".2.3": empty major`},
		{in: "1..3", wantErr: `invalid semver "1..3": empty minor`},
		{in: "1.2.", wantErr: `invalid semver "1.2.": empty patch`},
		{in: "1.2.-rc", wantErr: `invalid semver "1.2.-rc": empty patch`},
		{in: "01.2.3", wantErr: `invalid semver "01.2.3": major "01" has leading zero`},
		{in: "1.02.3", wantErr: `invalid semver "1.02.3": minor "02" has leading zero`},
		{in: "1.2.03", wantErr: `invalid semver "1.2.03": patch "03" has leading zero`},
		{in: "a.2.3", wantErr: `invalid semver "a.2.3": invalid major "a"`, wantIs: strconv.ErrSyntax},
		{in: "1.b.3", wantErr: `invalid semver "1.b.3": invalid minor "b"`, wantIs: strconv.ErrSyntax},
		{in: "1.2.c", wantErr: `invalid semver "1.2.c": invalid patch "c"`, wantIs: strconv.ErrSyntax},
		{in: "1.2.3.4", wantErr: `invalid semver "1.2.3.4": invalid patch "3.4"`, wantIs: strconv.ErrSyntax},
		{
			in:      "99999999999999999999.0.0",
			wantErr: `invalid semver "99999999999999999999.0.0": invalid major "99999999999999999999"`,
			wantIs:  strconv.ErrRange,
		},
		{in: "-1.2.3", wantErr: `invalid semver "-1.2.3": major must be non-negative`},
		{in: "1.-2.3", wantErr: `invalid semver "1.-2.3": minor must be non-negative`},
	}
	for _, tt := range invalid {
		t.Run("invalid "+tt.in, func(t *testing.T) {
			_, _, _, err := parseSemver(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("parseSemver(%q) error = %v, want %q", tt.in, err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("parseSemver(%q) error = %v, want it to wrap %v", tt.in, err, tt.wantIs)
			}
		})
	}
}

func TestAddRule_Rejected(t *testing.T) {
	tests := []struct {
		name    string
		f       *entity.File
		body    string
		addedBy string
		wantErr string
	}{
		{name: "nil file", body: "body", addedBy: "@user", wantErr: "entity file is nil"},
		{name: "empty body", f: fileWithRuleIDs(), addedBy: "@user", wantErr: "rule body must not be empty"},
		{name: "empty addedBy", f: fileWithRuleIDs(), body: "body", wantErr: "added_by must not be empty"},
		{
			name:    "invalid entity ID",
			f:       &entity.File{Entity: entity.Entity{ID: "AB"}},
			body:    "body",
			addedBy: "@user",
			wantErr: `invalid entity ID: invalid EUID "AB": must be 3-18 alphanumeric characters`,
		},
		{
			name:    "no rule number left",
			f:       fileWithRuleIDs("TST-999"),
			body:    "body",
			addedBy: "@user",
			wantErr: `cannot add rule: maximum number of rules (999) exceeded for entity "TST"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := AddRule(tt.f, tt.body, tt.addedBy)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("AddRule error = %v, want %q", err, tt.wantErr)
			}
			if id != "" {
				t.Errorf("AddRule returned ID %q alongside the error", id)
			}
		})
	}
}

func TestAddRule_LastRuleNumber(t *testing.T) {
	f := fileWithRuleIDs("TST-998")

	id, err := AddRule(f, "body", "@user")
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	if id != "TST-999" {
		t.Errorf("ID = %q, want %q", id, "TST-999")
	}
	assertHashCurrent(t, f)
}

func TestSupersedeRule_AllowedStates(t *testing.T) {
	for _, state := range []string{entity.StatePublished, entity.StateImplemented, entity.StateRetired} {
		t.Run(state, func(t *testing.T) {
			f := makeFileWithRule(t, state)
			oldVersion := f.RuleSet.Version

			id, err := SupersedeRule(f, "TST-001", "replacement body", "@author")
			if err != nil {
				t.Fatalf("SupersedeRule: %v", err)
			}
			if id != "TST-002" {
				t.Errorf("new ID = %q, want %q", id, "TST-002")
			}
			if len(f.Rules) != 2 {
				t.Fatalf("Rules = %d, want 2", len(f.Rules))
			}
			r := f.Rules[1]
			if r.ID != "TST-002" || r.State != entity.StateDraft || r.Revision != 0 ||
				r.Body != "replacement body" || r.AddedBy != "@author" || r.AddedAt != "2026-03-23" {
				t.Errorf("unexpected new rule: %+v", r)
			}
			if r.Supersedes == nil || *r.Supersedes != "TST-001" {
				t.Errorf("Supersedes = %v, want %q", r.Supersedes, "TST-001")
			}
			if f.Rules[0].State != state {
				t.Errorf("old rule State = %q, want %q", f.Rules[0].State, state)
			}
			wantVersion, err := BumpPatch(oldVersion)
			if err != nil {
				t.Fatalf("BumpPatch: %v", err)
			}
			if f.RuleSet.Version != wantVersion {
				t.Errorf("Version = %q, want %q", f.RuleSet.Version, wantVersion)
			}
			assertHashCurrent(t, f)
		})
	}
}

func TestSupersedeRule_InvalidInput(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*entity.File)
		body    string
		addedBy string
		wantErr string
	}{
		{name: "empty body", addedBy: "@user", wantErr: "rule body must not be empty"},
		{name: "empty addedBy", body: "body", wantErr: "added_by must not be empty"},
		{
			name:    "invalid entity ID",
			mutate:  func(f *entity.File) { f.Entity.ID = "AB" },
			body:    "body",
			addedBy: "@user",
			wantErr: `invalid entity ID: invalid EUID "AB": must be 3-18 alphanumeric characters`,
		},
		{
			name:    "no rule number left",
			mutate:  func(f *entity.File) { f.Rules = append(f.Rules, entity.Rule{ID: "TST-999"}) },
			body:    "body",
			addedBy: "@user",
			wantErr: `cannot add rule: maximum number of rules (999) exceeded for entity "TST"`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := makeFileWithRule(t, entity.StatePublished)
			if tt.mutate != nil {
				tt.mutate(f)
			}
			rules := len(f.Rules)

			id, err := SupersedeRule(f, "TST-001", tt.body, tt.addedBy)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("SupersedeRule error = %v, want %q", err, tt.wantErr)
			}
			if id != "" {
				t.Errorf("SupersedeRule returned ID %q alongside the error", id)
			}
			if len(f.Rules) != rules {
				t.Errorf("Rules = %d, want the file left at %d", len(f.Rules), rules)
			}
		})
	}
}

func TestLifecycleOps_RejectedStates(t *testing.T) {
	const (
		cannotPromote   = `: cannot promote`
		cannotRetire    = `: cannot retire`
		cannotAbandon   = `: only Draft rules can be abandoned`
		cannotSupersede = `: only Published, Implemented, or Retired rules can be superseded`
	)
	tests := []struct {
		op      string
		state   string
		wantErr string
	}{
		{"PromoteRule", entity.StateAbandoned, cannotPromote},
		{"PromoteRule", entity.StateImplemented, cannotPromote},
		{"PromoteRule", entity.StateRetired, cannotPromote},
		{"PromoteRule", entity.StateTerminated, cannotPromote},
		{"RetireRule", entity.StateDraft, cannotRetire},
		{"RetireRule", entity.StatePublished, cannotRetire},
		{"RetireRule", entity.StateAbandoned, cannotRetire},
		{"RetireRule", entity.StateTerminated, cannotRetire},
		{"AbandonRule", entity.StatePublished, cannotAbandon},
		{"AbandonRule", entity.StateImplemented, cannotAbandon},
		{"AbandonRule", entity.StateRetired, cannotAbandon},
		{"AbandonRule", entity.StateAbandoned, cannotAbandon},
		{"AbandonRule", entity.StateTerminated, cannotAbandon},
		{"SupersedeRule", entity.StateDraft, cannotSupersede},
		{"SupersedeRule", entity.StateAbandoned, cannotSupersede},
		{"SupersedeRule", entity.StateTerminated, cannotSupersede},
	}
	for _, tt := range tests {
		t.Run(tt.op+"/"+tt.state, func(t *testing.T) {
			f := makeFileWithRule(t, tt.state)
			version, hash := f.RuleSet.Version, *f.RuleSet.Hash

			err := ruleOps[tt.op](f, "TST-001")

			want := `rule "TST-001" is in state "` + tt.state + `"` + tt.wantErr
			if err == nil || err.Error() != want {
				t.Fatalf("%s error = %v, want %q", tt.op, err, want)
			}
			if f.Rules[0].State != tt.state {
				t.Errorf("State = %q, want it left at %q", f.Rules[0].State, tt.state)
			}
			if f.RuleSet.Version != version || *f.RuleSet.Hash != hash {
				t.Errorf("rule_set changed to %q, %q on a rejected transition", f.RuleSet.Version, *f.RuleSet.Hash)
			}
		})
	}
}

func TestRuleOps_UnknownRule(t *testing.T) {
	for op, run := range ruleOps {
		t.Run(op+"/missing rule", func(t *testing.T) {
			err := run(makeFileWithRule(t, entity.StateDraft), "TST-999")
			if err == nil || err.Error() != `rule "TST-999" not found` {
				t.Errorf("%s error = %v, want rule not found", op, err)
			}
		})
		t.Run(op+"/nil file", func(t *testing.T) {
			err := run(nil, "TST-001")
			if err == nil || err.Error() != "entity file is nil" {
				t.Errorf("%s error = %v, want entity file is nil", op, err)
			}
		})
	}
}

func TestOps_InvalidVersion(t *testing.T) {
	tests := []struct {
		name  string
		state string
		run   func(*entity.File) error
	}{
		{"AddRule", entity.StateDraft, func(f *entity.File) error {
			_, err := AddRule(f, "body", "@user")
			return err
		}},
		{"UpdateRuleBody", entity.StateDraft, func(f *entity.File) error {
			return UpdateRuleBody(f, "TST-001", "new body")
		}},
		{"PromoteRule", entity.StateDraft, func(f *entity.File) error {
			return PromoteRule(f, "TST-001")
		}},
		{"RetireRule", entity.StateImplemented, func(f *entity.File) error {
			return RetireRule(f, "TST-001")
		}},
		{"AbandonRule", entity.StateDraft, func(f *entity.File) error {
			return AbandonRule(f, "TST-001")
		}},
		{"SupersedeRule", entity.StatePublished, func(f *entity.File) error {
			_, err := SupersedeRule(f, "TST-001", "body", "@user")
			return err
		}},
		{"AddNote", entity.StatePublished, func(f *entity.File) error {
			_, err := AddNote(f, "TST-001", "rationale", "Because.", "@user")
			return err
		}},
		{"UpdateNote", entity.StatePublished, func(f *entity.File) error {
			f.Notes = append(f.Notes, entity.Note{ID: "TST-001/01", RuleRef: "TST-001", Type: "rationale", Content: "Old."})
			return UpdateNote(f, "TST-001/01", "New.", "")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := makeFileWithRule(t, tt.state)
			f.RuleSet.Version = "not-semver"

			err := tt.run(f)

			want := `bumping version: invalid semver: "not-semver"`
			if err == nil || err.Error() != want {
				t.Errorf("%s error = %v, want %q", tt.name, err, want)
			}
		})
	}
}

// ruleOps maps each lifecycle operation that acts on an existing rule to a
// common signature.
var ruleOps = map[string]func(*entity.File, string) error{
	"PromoteRule": PromoteRule,
	"RetireRule":  RetireRule,
	"AbandonRule": AbandonRule,
	"SupersedeRule": func(f *entity.File, ruleID string) error {
		_, err := SupersedeRule(f, ruleID, "body", "@user")
		return err
	},
}

// fileWithRuleIDs returns a valid TST file, version 0.1.0, holding rules with
// the given IDs.
func fileWithRuleIDs(ids ...string) *entity.File {
	f := &entity.File{
		Entity:  entity.Entity{ID: "TST"},
		RuleSet: entity.RuleSet{Version: "0.1.0"},
	}
	for _, id := range ids {
		f.Rules = append(f.Rules, entity.Rule{ID: id})
	}
	return f
}

// assertHashCurrent fails the test unless the stored rule_set hash is the hash
// of the file as it now stands.
func assertHashCurrent(t *testing.T, f *entity.File) {
	t.Helper()
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want, err := entity.ComputeHash(data)
	if err != nil {
		t.Fatalf("ComputeHash: %v", err)
	}
	if f.RuleSet.Hash == nil || *f.RuleSet.Hash != want {
		t.Errorf("Hash = %v, want %q", f.RuleSet.Hash, want)
	}
}

// makeFileWithRule creates a test entity.File with one rule in the given state.
func makeFileWithRule(t *testing.T, state string) *entity.File {
	t.Helper()
	f, err := NewFile("TST", "Test", "", "")
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	_, err = AddRule(f, "test rule body", "@user")
	if err != nil {
		t.Fatalf("AddRule: %v", err)
	}
	// Directly set state for testing purposes.
	f.Rules[0].State = state
	return f
}
