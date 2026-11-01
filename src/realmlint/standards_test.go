package realmlint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

func writeStandardsRealm(t *testing.T, dir string, declared []entity.StandardRef) {
	t.Helper()
	write := func(name string, v any) {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("realm.json", entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json",
		Realm:  entity.RealmMeta{ID: "dev.test.std", Title: "S", Version: "1.0.0", Standards: declared},
	})
	write("MAPPED.json", entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "MAPPED", Title: "Mapped"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules: []entity.Rule{{
			ID: "MAPPED-001", State: entity.StatePublished, Body: "The system MUST work.",
			AddedBy: "@a", AddedAt: "2026-01-01",
		}},
		Notes: []entity.Note{{
			ID: "MAPPED-001/01", RuleRef: "MAPPED-001", Type: entity.NoteTypeStandard,
			Content: "owasp.asvs:2.1.1", AddedBy: "@a", AddedAt: "2026-01-01",
		}},
	})
}

func countCode(l *RealmLinter, t *testing.T, dir, code string) int {
	t.Helper()
	n := 0
	for _, d := range l.Lint(t.Context(), dir).Diagnostics {
		if d.Code == code {
			n++
		}
	}
	return n
}

// `strspc realm validate` without --recursive reaches entities only through
// realm-lint, so the undeclared standard check has to run here too.
func TestUndeclaredStandardWithRuleLinter(t *testing.T) {
	dir := t.TempDir()
	writeStandardsRealm(t, dir, nil)
	if got := countCode(New(WithRuleLinter(rulelint.New())), t, dir, "RL031"); got != 1 {
		t.Errorf("RL031 = %d, want 1", got)
	}
}

func TestDeclaredStandardWithRuleLinter(t *testing.T) {
	dir := t.TempDir()
	writeStandardsRealm(t, dir, []entity.StandardRef{{ID: "owasp.asvs", Relation: entity.RelationImplements}})
	if got := countCode(New(WithRuleLinter(rulelint.New())), t, dir, "RL031"); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}

// With --recursive the CLI passes no rule linter and LintRealm runs the check,
// so realm-lint must stay out of it or the finding would appear twice.
func TestUndeclaredStandardWithoutRuleLinter(t *testing.T) {
	dir := t.TempDir()
	writeStandardsRealm(t, dir, nil)
	if got := countCode(New(), t, dir, "RL031"); got != 0 {
		t.Errorf("RL031 = %d, want 0", got)
	}
}
