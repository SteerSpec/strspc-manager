package rulelint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestRL032_AddedBy(t *testing.T) {
	tests := []struct {
		addedBy string
		want    string // "" for no RL032
	}{
		{"github:pleberre", ""},
		{"github:steerspec-sync[bot]", ""},
		{"", ""},
		{"@pleberre", "write github:pleberre"},
		{"@steerspec-sync[bot]", "write github:steerspec-sync[bot]"},
		{"dev@example.com", "CTRB-001"},
		{"gitlab:pleberre", "CTRB-002"},
		{"@a--b", "CTRB-001"},
	}
	for _, tt := range tests {
		t.Run(tt.addedBy, func(t *testing.T) {
			// Strict mode promotes warnings only: RL032 stays Info.
			res := lintRulesDir(t, true, []entity.Rule{
				{ID: "TST-001", State: "P", Body: "The system MUST work.", AddedBy: tt.addedBy, AddedAt: "2026-01-01"},
			}, []entity.Note{
				{ID: "TST-001/01", RuleRef: "TST-001", Type: "rationale", Content: "Because.", AddedBy: tt.addedBy, AddedAt: "2026-01-01"},
			})
			got := diagsWithCode(res, "RL032")
			if tt.want == "" {
				if len(got) != 0 {
					t.Fatalf("RL032: %v", got)
				}
				return
			}
			if len(got) != 2 {
				t.Fatalf("RL032 = %v, want one for the rule and one for the note", got)
			}
			for i, what := range []string{"rule TST-001", "note TST-001/01"} {
				d := got[i]
				if d.Severity != result.Info {
					t.Errorf("severity = %v, want Info", d.Severity)
				}
				for _, w := range []string{what, "CTRB-003", tt.want} {
					if !strings.Contains(d.Message, w) {
						t.Errorf("message %q lacks %q", d.Message, w)
					}
				}
			}
		})
	}
}

// RL032 reaches the rules of sub-entities, with the entity path.
func TestRL032_SubEntity(t *testing.T) {
	data, err := json.Marshal(entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: "1.0.0"},
		SubEntities: []entity.File{{
			Entity:  entity.Entity{ID: "TSTSUB", Title: "Sub", Parent: "TST"},
			RuleSet: entity.RuleSet{Version: "1.0.0"},
			Rules: []entity.Rule{
				{ID: "TSTSUB-001", State: "P", Body: "The system MUST work.", AddedBy: "@pleberre", AddedAt: "2026-01-01"},
			},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "TST.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	got := diagsWithCode(New().LintDir(context.Background(), dir), "RL032")
	if len(got) != 1 || !strings.HasSuffix(got[0].Path, ": TST > TSTSUB") || !strings.Contains(got[0].Message, "rule TSTSUB-001") {
		t.Fatalf("RL032 = %v", got)
	}
}
