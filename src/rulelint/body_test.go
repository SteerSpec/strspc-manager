package rulelint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// lintRulesDir writes one entity holding rules and notes to a temporary
// directory and lints it, so the cross-file checks run.
func lintRulesDir(t *testing.T, strict bool, rules []entity.Rule, notes []entity.Note) *result.Result {
	t.Helper()
	data, err := json.Marshal(entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0"},
		Rules:   rules,
		Notes:   notes,
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TST.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return New(WithStrict(strict)).LintDir(context.Background(), dir)
}

func TestRL019_BodyRefs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"missing", "MUST apply TST-009.", true},
		{"found", "MUST apply TST-002.", false},
		{"self", "MUST apply TST-001 first.", false},
		{"qualified", "MUST apply dev.steerspec.core:RUL-004.", false},
		{"backticked", "MUST use `SHA-256` for hashes.", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := lintRulesDir(t, false, []entity.Rule{
				{ID: "TST-001", State: "P", Body: tt.body},
				{ID: "TST-002", State: "I", Body: "MUST hold."},
			}, nil)
			if got := len(diagsWithCode(res, "RL019")) > 0; got != tt.want {
				t.Errorf("RL019 = %v, want %v: %v", got, tt.want, res.Diagnostics)
			}
		})
	}
}

func TestRL019_ReportsEachRefOnce(t *testing.T) {
	res := lintRulesDir(t, false, []entity.Rule{
		{ID: "TST-001", State: "D", Body: "MUST apply TST-009, then TST-009 again."},
	}, nil)
	if got := len(diagsWithCode(res, "RL019")); got != 1 {
		t.Errorf("RL019 count = %d, want 1: %v", got, res.Diagnostics)
	}
}

func TestRL019_SkipsDeadRules(t *testing.T) {
	for _, state := range []string{"A", "R", "T"} {
		res := lintRulesDir(t, false, []entity.Rule{
			{ID: "TST-001", State: state, Body: "MUST apply TST-009."},
		}, nil)
		assertNoCode(t, res, "RL019")
	}
}

func TestRL020_AbandonedRefs(t *testing.T) {
	rules := func(state, body string) []entity.Rule {
		return []entity.Rule{
			{ID: "TST-001", State: state, Body: body},
			{ID: "TST-002", State: "A", Body: "MUST go."},
			{ID: "TST-003", State: "R", Body: "MUST stop."},
		}
	}
	note := func(noteType string) []entity.Note {
		return []entity.Note{{ID: "TST-001/01", RuleRef: "TST-001", Type: noteType, Content: "See TST-002."}}
	}
	tests := []struct {
		name  string
		rules []entity.Rule
		notes []entity.Note
		want  bool
	}{
		{"live body", rules("I", "MUST apply TST-002."), nil, true},
		{"abandoned body", rules("A", "MUST apply TST-002."), nil, false},
		{"retired target", rules("I", "MUST apply TST-003."), nil, false},
		{"related note", rules("D", "MUST hold."), note("related"), true},
		{"extends note", rules("D", "MUST hold."), note("extends"), true},
		{"supersedes note", rules("D", "MUST hold."), note("supersedes"), false},
		{"note on abandoned rule", rules("A", "MUST hold."), note("related"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := lintRulesDir(t, false, tt.rules, tt.notes)
			if got := len(diagsWithCode(res, "RL020")) > 0; got != tt.want {
				t.Errorf("RL020 = %v, want %v: %v", got, tt.want, res.Diagnostics)
			}
		})
	}
}

func TestBodyRefs_StrictIsError(t *testing.T) {
	res := lintRulesDir(t, true, []entity.Rule{
		{ID: "TST-001", State: "I", Body: "MUST apply TST-002 and TST-009."},
		{ID: "TST-002", State: "A", Body: "MUST go."},
	}, nil)
	for _, code := range []string{"RL019", "RL020"} {
		ds := diagsWithCode(res, code)
		if len(ds) != 1 || ds[0].Severity != result.Error {
			t.Errorf("%s under strict = %v, want one Error", code, ds)
		}
	}
}

func TestDraftBodies(t *testing.T) {
	tests := []struct {
		name         string
		state        string
		body         string
		wantKeyword  bool
		wantSentence bool
	}{
		{name: "clean", state: "D", body: "MUST hold."},
		{name: "no keyword", state: "D", body: "Applies to all rules.", wantKeyword: true},
		{name: "lowercase keyword", state: "D", body: "It must hold.", wantKeyword: true},
		{name: "keyword only in code", state: "D", body: "Applies to `MUST`.", wantKeyword: true},
		{name: "defines", state: "D", body: "DEFINES a property."},
		{name: "two sentences", state: "D", body: "MUST hold. It MUST NOT fail.", wantSentence: true},
		{name: "question mark", state: "D", body: "IS it true? Yes it IS.", wantSentence: true},
		{name: "semicolon", state: "D", body: "MUST hold; it MUST NOT fail."},
		{name: "abbreviation", state: "D", body: "MUST name a tool, e.g. Claude."},
		{name: "full stop in code", state: "D", body: "MUST call `a. B` first."},
		{name: "version", state: "D", body: "MUST use 1.2.3 or later."},
		{name: "published", state: "P", body: "Applies here. It holds."},
		{name: "implemented", state: "I", body: "Applies here. It holds."},
		{name: "abandoned", state: "A", body: "Applies here. It holds."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := New().LintFile(&entity.File{
				Entity:  entity.Entity{ID: "TST", Title: "Test"},
				RuleSet: entity.RuleSet{Version: "1.0.0"},
				Rules:   []entity.Rule{{ID: "TST-001", State: tt.state, Body: tt.body}},
			})
			if got := len(diagsWithCode(res, "RL021")) > 0; got != tt.wantKeyword {
				t.Errorf("RL021 = %v, want %v: %v", got, tt.wantKeyword, res.Diagnostics)
			}
			if got := len(diagsWithCode(res, "RL022")) > 0; got != tt.wantSentence {
				t.Errorf("RL022 = %v, want %v: %v", got, tt.wantSentence, res.Diagnostics)
			}
		})
	}
}

func TestDraftBodies_SubEntity(t *testing.T) {
	res := New().LintFile(&entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0"},
		SubEntities: []entity.File{{
			Entity:  entity.Entity{ID: "SUB", Title: "Sub", Parent: "TST"},
			RuleSet: entity.RuleSet{Version: "1.0.0"},
			Rules:   []entity.Rule{{ID: "SUB-001", State: "D", Body: "Applies here."}},
		}},
	})
	assertHasCode(t, res, "RL021")
}

func TestDraftBodies_StrictIsError(t *testing.T) {
	res := New(WithStrict(true)).LintFile(&entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0"},
		Rules:   []entity.Rule{{ID: "TST-001", State: "D", Body: "Applies here. It holds."}},
	})
	for _, code := range []string{"RL021", "RL022"} {
		ds := diagsWithCode(res, code)
		if len(ds) != 1 || ds[0].Severity != result.Error {
			t.Errorf("%s under strict = %v, want one Error", code, ds)
		}
	}
}
