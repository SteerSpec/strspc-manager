package entityops

import (
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
		if r.State != StateDraft {
			t.Errorf("State = %q, want %q", r.State, StateDraft)
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
		f := makeFileWithRule(t, StatePublished)

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

func TestValidateTransition(t *testing.T) {
	valid := []struct{ from, to string }{
		{"D", "P"}, {"D", "A"}, {"P", "I"}, {"I", "R"}, {"R", "T"},
	}
	for _, tt := range valid {
		t.Run(tt.from+"→"+tt.to, func(t *testing.T) {
			if err := ValidateTransition(tt.from, tt.to); err != nil {
				t.Errorf("expected valid transition %s→%s, got error: %v", tt.from, tt.to, err)
			}
		})
	}

	invalid := []struct{ from, to string }{
		{"P", "D"}, {"I", "P"}, {"D", "I"}, {"D", "R"},
		{"A", "D"}, {"A", "P"}, {"T", "R"}, {"T", "D"},
		{"R", "I"}, {"P", "R"},
	}
	for _, tt := range invalid {
		t.Run(tt.from+"→"+tt.to+" rejected", func(t *testing.T) {
			if err := ValidateTransition(tt.from, tt.to); err == nil {
				t.Errorf("expected error for transition %s→%s", tt.from, tt.to)
			}
		})
	}
}

func TestPromoteRule(t *testing.T) {
	t.Run("D→P", func(t *testing.T) {
		f := makeFileWithRule(t, StateDraft)
		v := f.RuleSet.Version

		err := PromoteRule(f, "TST-001")
		if err != nil {
			t.Fatalf("PromoteRule: %v", err)
		}
		if f.Rules[0].State != StatePublished {
			t.Errorf("State = %q, want %q", f.Rules[0].State, StatePublished)
		}
		expected, _ := BumpMinor(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("P→I", func(t *testing.T) {
		f := makeFileWithRule(t, StatePublished)

		err := PromoteRule(f, "TST-001")
		if err != nil {
			t.Fatalf("PromoteRule: %v", err)
		}
		if f.Rules[0].State != StateImplemented {
			t.Errorf("State = %q, want %q", f.Rules[0].State, StateImplemented)
		}
	})

	t.Run("I rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StateImplemented)

		err := PromoteRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Implemented rule")
		}
	})
}

func TestRetireRule(t *testing.T) {
	t.Run("I→R", func(t *testing.T) {
		f := makeFileWithRule(t, StateImplemented)
		v := f.RuleSet.Version

		err := RetireRule(f, "TST-001")
		if err != nil {
			t.Fatalf("RetireRule: %v", err)
		}
		if f.Rules[0].State != StateRetired {
			t.Errorf("State = %q, want %q", f.Rules[0].State, StateRetired)
		}
		expected, _ := BumpMinor(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("R→T", func(t *testing.T) {
		f := makeFileWithRule(t, StateRetired)

		err := RetireRule(f, "TST-001")
		if err != nil {
			t.Fatalf("RetireRule: %v", err)
		}
		if f.Rules[0].State != StateTerminated {
			t.Errorf("State = %q, want %q", f.Rules[0].State, StateTerminated)
		}
	})

	t.Run("D rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StateDraft)

		err := RetireRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Draft rule")
		}
	})
}

func TestAbandonRule(t *testing.T) {
	t.Run("D→A", func(t *testing.T) {
		f := makeFileWithRule(t, StateDraft)
		v := f.RuleSet.Version

		err := AbandonRule(f, "TST-001")
		if err != nil {
			t.Fatalf("AbandonRule: %v", err)
		}
		if f.Rules[0].State != StateAbandoned {
			t.Errorf("State = %q, want %q", f.Rules[0].State, StateAbandoned)
		}
		expected, _ := BumpPatch(v)
		if f.RuleSet.Version != expected {
			t.Errorf("Version = %q, want %q", f.RuleSet.Version, expected)
		}
	})

	t.Run("P rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StatePublished)

		err := AbandonRule(f, "TST-001")
		if err == nil {
			t.Fatal("expected error for Published rule")
		}
	})
}

func TestSupersedeRule(t *testing.T) {
	t.Run("supersede published", func(t *testing.T) {
		f := makeFileWithRule(t, StatePublished)

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
		if newRule.State != StateDraft {
			t.Errorf("new rule State = %q, want %q", newRule.State, StateDraft)
		}
		if newRule.Supersedes == nil || *newRule.Supersedes != "TST-001" {
			t.Errorf("Supersedes = %v, want %q", newRule.Supersedes, "TST-001")
		}
		// Old rule unchanged.
		if f.Rules[0].State != StatePublished {
			t.Errorf("old rule State = %q, want %q", f.Rules[0].State, StatePublished)
		}
	})

	t.Run("draft rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StateDraft)

		_, err := SupersedeRule(f, "TST-001", "body", "@user")
		if err == nil {
			t.Fatal("expected error for Draft rule")
		}
	})

	t.Run("abandoned rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StateAbandoned)

		_, err := SupersedeRule(f, "TST-001", "body", "@user")
		if err == nil {
			t.Fatal("expected error for Abandoned rule")
		}
	})

	t.Run("terminated rejected", func(t *testing.T) {
		f := makeFileWithRule(t, StateTerminated)

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
