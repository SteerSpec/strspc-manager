package entityops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
)

func TestCheckAddedBy(t *testing.T) {
	list := []entity.Contributor{{ID: "github:pleberre", Email: "dev@example.com"}}
	tests := []struct {
		addedBy string
		list    []entity.Contributor
		err     string
	}{
		{addedBy: "github:pleberre"},
		{addedBy: "@pleberre"},
		{addedBy: "dev@example.com"},
		{addedBy: "github:someone"},
		{addedBy: "", err: "must not be empty"},
		{addedBy: "pleberre", err: "CTRB-001"},
		{addedBy: "gitlab:pleberre", err: "CTRB-002"},
		{addedBy: "github:pleberre", list: list},
		{addedBy: "@pleberre", list: list},
		{addedBy: "dev@example.com", list: list},
		{addedBy: "github:someone", list: list, err: "CTRB-005"},
		{addedBy: "@someone", list: list, err: "CTRB-005"},
		{addedBy: "pleberre", list: list, err: "CTRB-001"},
	}
	for _, tt := range tests {
		t.Run(tt.addedBy, func(t *testing.T) {
			err := CheckAddedBy(tt.addedBy, tt.list)
			if tt.err == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Fatalf("err = %v, want one containing %q", err, tt.err)
			}
		})
	}
}

// The write operations refuse an added_by in no accepted form, and leave the
// file as it was.
func TestWriteOpsRejectInvalidAddedBy(t *testing.T) {
	f := makeFileWithRule(t, entity.StatePublished)
	before, _ := json.Marshal(f)
	if _, err := AddRule(f, "body", "pleberre"); err == nil || !strings.Contains(err.Error(), "CTRB-001") {
		t.Errorf("AddRule: err = %v", err)
	}
	if _, err := AddNote(f, f.Rules[0].ID, "rationale", "x", "pleberre"); err == nil || !strings.Contains(err.Error(), "CTRB-001") {
		t.Errorf("AddNote: err = %v", err)
	}
	if _, err := SupersedeRule(f, f.Rules[0].ID, "body", "pleberre"); err == nil || !strings.Contains(err.Error(), "CTRB-001") {
		t.Errorf("SupersedeRule: err = %v", err)
	}
	if after, _ := json.Marshal(f); string(after) != string(before) {
		t.Error("a rejected write changed the file")
	}
}

// migrationFile returns TST with a rule and a note by @pleberre and a rule by
// someone else, and a sub-entity TSTSUB with a rule by @pleberre and a
// sub-entity TSTLEAF untouched by the migration.
func migrationFile(t *testing.T) *entity.File {
	t.Helper()
	f, err := NewFile("TST", "Test", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mustAdd := func(f *entity.File, by string) string {
		id, err := AddRule(f, "The system MUST work.", by)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := mustAdd(f, "@pleberre")
	if _, err := AddNote(f, id, "rationale", "Because.", "@pleberre"); err != nil {
		t.Fatal(err)
	}
	mustAdd(f, "github:other")
	f.Rules[0].State = entity.StateImplemented
	f.Rules[0].Revision = 2

	sub, err := NewFile("TSTSUB", "Sub", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(sub, "@pleberre")
	leaf, err := NewFile("TSTLEAF", "Leaf", "", "")
	if err != nil {
		t.Fatal(err)
	}
	mustAdd(leaf, "github:other")
	sub.SubEntities = append(sub.SubEntities, *leaf)
	if err := UpdateMeta(sub); err != nil {
		t.Fatal(err)
	}
	f.SubEntities = append(f.SubEntities, *sub)
	if err := UpdateMeta(f); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRewriteAddedBy(t *testing.T) {
	f := migrationFile(t)
	baseData, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	base, err := entity.Parse(baseData)
	if err != nil {
		t.Fatal(err)
	}

	// The migration runs later than the file was written, as rule-diff
	// requires a new timestamp (RD009).
	pinned := nowFunc
	nowFunc = func() time.Time { return pinned().Add(time.Hour) }
	t.Cleanup(func() { nowFunc = pinned })

	n, err := RewriteAddedBy(f, map[string]string{"@pleberre": "github:pleberre", "@nobody": "github:nobody"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("rewrote %d, want 3", n)
	}
	sub, leaf := &f.SubEntities[0], &f.SubEntities[0].SubEntities[0]
	for _, got := range []string{f.Rules[0].AddedBy, f.Notes[0].AddedBy, sub.Rules[0].AddedBy} {
		if got != "github:pleberre" {
			t.Errorf("added_by = %q, want github:pleberre", got)
		}
	}
	if f.Rules[1].AddedBy != "github:other" || leaf.Rules[0].AddedBy != "github:other" {
		t.Error("an added_by outside the map changed")
	}
	if f.Rules[0].Revision != 2 || f.Notes[0].Revision != 0 || sub.Rules[0].Revision != 0 {
		t.Error("a revision changed")
	}
	for _, c := range []struct {
		f    *entity.File
		want string
	}{
		{f, bumped(t, base.RuleSet.Version)},
		{sub, bumped(t, base.SubEntities[0].RuleSet.Version)},
		{leaf, base.SubEntities[0].SubEntities[0].RuleSet.Version},
	} {
		if c.f.RuleSet.Version != c.want {
			t.Errorf("%s version = %s, want %s", c.f.Entity.ID, c.f.RuleSet.Version, c.want)
		}
	}
	assertHashCurrent(t, f)
	assertHashCurrent(t, sub)
	if *leaf.RuleSet.Hash != *base.SubEntities[0].SubEntities[0].RuleSet.Hash {
		t.Error("the untouched sub-entity's hash changed")
	}

	// rule-diff accepts the migration: no error and no warning.
	headData, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rulediff.Compare(baseData, headData).Diagnostics {
		if d.Severity != result.Info {
			t.Errorf("rule-diff: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

func bumped(t *testing.T, v string) string {
	t.Helper()
	out, err := BumpPatch(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestRewriteAddedByNoMatch(t *testing.T) {
	f := migrationFile(t)
	before, _ := json.Marshal(f)
	n, err := RewriteAddedBy(f, map[string]string{"@nobody": "github:nobody"})
	if err != nil || n != 0 {
		t.Fatalf("n = %d, err = %v", n, err)
	}
	if after, _ := json.Marshal(f); string(after) != string(before) {
		t.Error("the file changed with nothing to rewrite")
	}
}

func TestRewriteAddedByInvalid(t *testing.T) {
	for name, m := range map[string]map[string]string{
		"target not a contributor id": {"@pleberre": "@pleberre2"},
		"unlisted provider":           {"@pleberre": "gitlab:pleberre"},
		"empty source":                {"": "github:pleberre"},
	} {
		t.Run(name, func(t *testing.T) {
			f := migrationFile(t)
			before, _ := json.Marshal(f)
			if _, err := RewriteAddedBy(f, m); err == nil {
				t.Fatal("no error")
			}
			if after, _ := json.Marshal(f); string(after) != string(before) {
				t.Error("an invalid map changed the file")
			}
		})
	}
	if _, err := RewriteAddedBy(nil, nil); err == nil {
		t.Error("nil file: no error")
	}
}

func TestRewriteAddedByBadVersion(t *testing.T) {
	f := migrationFile(t)
	f.SubEntities[0].RuleSet.Version = "x"
	if _, err := RewriteAddedBy(f, map[string]string{"@pleberre": "github:pleberre"}); err == nil || !strings.Contains(err.Error(), "TSTSUB") {
		t.Fatalf("err = %v", err)
	}
}
