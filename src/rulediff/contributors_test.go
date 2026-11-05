package rulediff

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

var testContributors = []entity.Contributor{{ID: "github:pleberre", Email: "dev@example.com"}}

func rd023(res *result.Result) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == "RD023" {
			out = append(out, d)
		}
	}
	return out
}

// headWithNew adds a rule and a note by addedBy to a head of makeBase.
func headWithNew(addedBy string) (*entity.File, *entity.File) {
	base := makeBase()
	head := makeHead(base)
	head.Rules = append(head.Rules, entity.Rule{
		ID: "TSTENT-004", State: "D", Body: "New rule", AddedBy: addedBy, AddedAt: "2026-02-01",
	})
	head.Notes = append(head.Notes, entity.Note{
		ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: "rationale", Content: "Because", AddedBy: addedBy, AddedAt: "2026-02-01",
	})
	return base, head
}

func TestRD023_NewEntries(t *testing.T) {
	tests := []struct {
		addedBy string
		want    bool
	}{
		{"github:pleberre", false},
		{"github:PLeberre", false},
		{"@pleberre", false},       // not a contributor identifier: RL032 reports it.
		{"dev@example.com", false}, // likewise.
		{"github:someone", true},
		{"@someone", false},
		{"other@example.com", false},
		{"pleberre", false}, // RL032 reports it.
		{"", false},         // RD011 reports it.
	}
	for _, tt := range tests {
		t.Run(tt.addedBy, func(t *testing.T) {
			base, head := headWithNew(tt.addedBy)
			// RD023 is an Error whether or not the run is strict.
			got := rd023(New(WithStrict(true), WithContributors(testContributors)).Diff(base, head))
			if !tt.want {
				if len(got) != 0 {
					t.Fatalf("RD023: %v", got)
				}
				return
			}
			if len(got) != 2 {
				t.Fatalf("RD023 = %v, want one for the rule and one for the note", got)
			}
			for i, w := range []struct{ path, kind string }{{"TSTENT/TSTENT-004", "new rule"}, {"TSTENT/notes/TSTENT-001/02", "new note"}} {
				d := got[i]
				if d.Severity != result.Error || d.Path != w.path || !strings.Contains(d.Message, w.kind) || !strings.Contains(d.Message, "CTRB-005") {
					t.Errorf("RD023[%d] = %+v", i, d)
				}
			}
		})
	}
}

// Without a list the Realm declares no contributors, and RD023 does not run.
func TestRD023_NoList(t *testing.T) {
	base, head := headWithNew("github:someone")
	for _, d := range []*Differ{New(), New(WithContributors(nil))} {
		if got := rd023(d.Diff(base, head)); len(got) != 0 {
			t.Fatalf("RD023: %v", got)
		}
	}
}

// Rules and notes already in base keep their added_by, listed or not.
func TestRD023_ExistingEntries(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	if got := rd023(New(WithContributors(testContributors)).Diff(base, head)); len(got) != 0 {
		t.Fatalf("RD023 on existing entries: %v", got)
	}
}

// RD023 runs on new and existing sub-entities, and on a new file.
func TestRD023_SubEntitiesAndNewFile(t *testing.T) {
	sub := entity.File{
		Entity:  entity.Entity{ID: "TSTSUB", Title: "Sub", Parent: "TSTENT"},
		RuleSet: entity.RuleSet{Version: "0.1.0"},
		Rules:   []entity.Rule{{ID: "TSTSUB-001", State: "D", Body: "Sub rule", AddedBy: "github:old", AddedAt: "2026-01-01"}},
	}
	base := makeBase()
	base.SubEntities = []entity.File{sub}
	head := makeHead(base)
	headSub := sub
	headSub.Rules = append([]entity.Rule{}, sub.Rules...)
	headSub.Rules = append(headSub.Rules, entity.Rule{ID: "TSTSUB-002", State: "D", Body: "Sub rule", AddedBy: "github:someone", AddedAt: "2026-02-01"})
	newSub := entity.File{
		Entity:  entity.Entity{ID: "TSTNEW", Title: "New", Parent: "TSTENT"},
		RuleSet: entity.RuleSet{Version: "0.1.0"},
		Rules:   []entity.Rule{{ID: "TSTNEW-001", State: "D", Body: "New rule", AddedBy: "github:someone", AddedAt: "2026-02-01"}},
	}
	head.SubEntities = []entity.File{headSub, newSub}

	var paths []string
	for _, d := range rd023(New(WithContributors(testContributors)).Diff(base, head)) {
		paths = append(paths, d.Path)
	}
	want := "TSTENT > TSTSUB/TSTSUB-002,TSTENT > TSTNEW/TSTNEW-001"
	if strings.Join(paths, ",") != want {
		t.Errorf("RD023 paths = %v, want %s", paths, want)
	}

	data, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}
	// In a new file every entry is new: the three sub-entity rules are by a
	// contributor identifier the Realm does not list. The entries of makeBase
	// are not contributor identifiers, which RL032 reports instead.
	if got := rd023(CompareNew(data, WithContributors(testContributors))); len(got) != 3 {
		t.Errorf("CompareNew: RD023 = %v, want 3", got)
	}
	baseData, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	if got := rd023(Compare(baseData, data, WithContributors(testContributors))); len(got) != 2 {
		t.Errorf("Compare: RD023 = %v", got)
	}
}
