package rulehistory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func ptr(s string) *string { return &s }

func rule(id, state, body string, rev int) entity.Rule {
	return entity.Rule{ID: id, Revision: rev, State: state, Body: body, AddedBy: "github:dev", AddedAt: "2026-01-01"}
}

func file(version string, rules ...entity.Rule) *entity.File {
	return &entity.File{
		Entity:  entity.Entity{ID: "TST", Title: "Test"},
		RuleSet: entity.RuleSet{Version: version, Timestamp: "2026-01-01T00:00:00Z"},
		Rules:   rules,
		Notes:   []entity.Note{},
	}
}

func raw(t *testing.T, f *entity.File) []byte {
	t.Helper()
	if f == nil {
		return nil
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var day = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)

func commit(t *testing.T, sha, subject string, before, after *entity.File) Commit {
	t.Helper()
	return Commit{SHA: sha, Date: day, Subject: subject, Before: raw(t, before), After: raw(t, after)}
}

// kinds returns "RULE:kind" for each event, the compact form the tests
// compare.
func kinds(h *History) string {
	var out []string
	for _, e := range h.Events {
		out = append(out, e.RuleID+":"+string(e.Kind))
	}
	return strings.Join(out, " ")
}

func TestBuildKinds(t *testing.T) {
	sup := rule("TST-003", "D", "new", 0)
	sup.Supersedes = ptr("TST-001")
	meta := rule("TST-001", "I", "one", 0)
	meta.AddedBy = "github:other"
	tests := []struct {
		name          string
		before, after *entity.File
		want          string
	}{
		{"file added", nil, file("0.1.0", rule("TST-001", "D", "one", 0)), "TST-001:added"},
		{"body changed", file("0.1.0", rule("TST-001", "D", "one", 0)), file("0.1.1", rule("TST-001", "D", "uno", 1)), "TST-001:body_changed"},
		{"published", file("0.1.0", rule("TST-001", "D", "one", 0)), file("0.2.0", rule("TST-001", "P", "one", 0)), "TST-001:promoted"},
		{"implemented", file("0.1.0", rule("TST-001", "P", "one", 0)), file("0.2.0", rule("TST-001", "I", "one", 0)), "TST-001:promoted"},
		{"retired", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.2.0", rule("TST-001", "R", "one", 0)), "TST-001:retired"},
		{"terminated", file("0.1.0", rule("TST-001", "R", "one", 0)), file("0.2.0", rule("TST-001", "T", "one", 0)), "TST-001:terminated"},
		{"abandoned", file("0.1.0", rule("TST-001", "D", "one", 0)), file("0.2.0", rule("TST-001", "A", "one", 0)), "TST-001:abandoned"},
		{"backwards state", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.2.0", rule("TST-001", "D", "one", 0)), "TST-001:state_changed"},
		{"metadata", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.1.1", meta), "TST-001:metadata_changed"},
		{"unchanged rule", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.1.1", rule("TST-001", "I", "one", 0)), ""},
		{"body and state", file("0.1.0", rule("TST-001", "D", "one", 0)), file("0.2.0", rule("TST-001", "P", "uno", 1)), "TST-001:body_changed TST-001:promoted"},
		{"superseded", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.2.0", rule("TST-001", "I", "one", 0), sup), "TST-001:superseded TST-003:added"},
		{"retired and superseded", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.2.0", rule("TST-001", "R", "one", 0), sup), "TST-001:retired TST-001:superseded TST-003:added"},
		{"removed", file("0.1.0", rule("TST-001", "D", "one", 0), rule("TST-002", "D", "two", 0)), file("0.2.0", rule("TST-002", "D", "two", 0)), "TST-001:removed"},
		{"file deleted", file("0.1.0", rule("TST-001", "D", "one", 0)), nil, "TST-001:removed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := Build([]Commit{commit(t, "c1", "chore: x", tt.before, tt.after)})
			if got := kinds(h); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if len(h.Warnings) != 0 {
				t.Errorf("warnings: %v", h.Warnings)
			}
		})
	}
}

func TestBuildEventFields(t *testing.T) {
	sup := rule("TST-002", "D", "new", 0)
	sup.Supersedes = ptr("TST-001")
	h := Build([]Commit{
		commit(t, "c1", "feat(rules): add TST (#7)", nil, file("0.1.0", rule("TST-001", "I", "one", 0))),
		commit(t, "c2", "feat(rules): supersede TST-001 (#9)", file("0.1.0", rule("TST-001", "I", "one", 0)),
			file("0.2.0", rule("TST-001", "R", "one", 0), sup)),
	})
	ret := h.Events[1]
	if ret.Kind != Retired || ret.FromState != "I" || ret.ToState != "R" || ret.RuleSetVersion != "0.2.0" || ret.PR != 9 || ret.Commit != "c2" || ret.Date != "2026-03-01T12:00:00Z" {
		t.Errorf("retired event = %+v", ret)
	}
	if s := h.Events[2]; s.Kind != Superseded || s.SupersededBy != "TST-002" {
		t.Errorf("superseded event = %+v", s)
	}
	if a := h.Events[3]; a.Kind != Added || a.Supersedes != "TST-001" || a.FromState != "" || a.ToState != "D" {
		t.Errorf("added event = %+v", a)
	}
}

// A rule that supersedes one in another file adds no superseded event.
func TestBuildCrossFileSupersedes(t *testing.T) {
	sup := rule("TST-002", "D", "new", 0)
	sup.Supersedes = ptr("OTHER-001")
	h := Build([]Commit{commit(t, "c1", "x", file("0.1.0"), file("0.2.0", sup))})
	if got := kinds(h); got != "TST-002:added" {
		t.Fatalf("got %q", got)
	}
	if h.Events[0].Supersedes != "OTHER-001" {
		t.Errorf("supersedes = %q", h.Events[0].Supersedes)
	}
}

func TestBuildSubEntity(t *testing.T) {
	before := file("0.1.0", rule("TST-001", "D", "one", 0))
	before.SubEntities = []entity.File{*file("", rule("SUB-001", "D", "s", 0))}
	before.SubEntities[0].Entity = entity.Entity{ID: "SUB", Parent: "TST"}
	after := file("0.2.0", rule("TST-001", "D", "one", 0))
	after.SubEntities = []entity.File{*file("", rule("SUB-001", "P", "s", 0))}
	after.SubEntities[0].Entity = entity.Entity{ID: "SUB", Parent: "TST"}
	h := Build([]Commit{commit(t, "c1", "x", before, after)})
	if len(h.Events) != 1 || h.Events[0].Entity != "TST > SUB" || h.Events[0].Kind != Promoted || h.Events[0].RuleSetVersion != "0.2.0" {
		t.Fatalf("events = %+v", h.Events)
	}
}

func TestBuildUnparsable(t *testing.T) {
	v1 := file("0.1.0", rule("TST-001", "D", "one", 0))
	v2 := file("0.2.0", rule("TST-001", "P", "one", 0))
	broken := []byte("{not json")
	h := Build([]Commit{
		commit(t, "c1", "x", nil, v1),
		{SHA: "c2", Date: day, Subject: "break", Before: raw(t, v1), After: broken},
		{SHA: "c3", Date: day, Subject: "fix", Before: broken, After: raw(t, v2)},
	})
	// c2 is skipped, and c3 is compared with c1's version.
	if got := kinds(h); got != "TST-001:added TST-001:promoted" {
		t.Fatalf("got %q", got)
	}
	if len(h.Warnings) != 2 || h.Warnings[0].Commit != "c2" || h.Warnings[1].Commit != "c3" {
		t.Fatalf("warnings = %v", h.Warnings)
	}
	if !strings.HasPrefix(h.Warnings[0].String(), "c2: skipped") {
		t.Errorf("warning string = %q", h.Warnings[0].String())
	}
}

func TestPRNumber(t *testing.T) {
	tests := map[string]int{
		"feat(rules): add TST (#12)":                      12,
		"fix: handle (#3) in text (#45)":                  45,
		"Merge pull request #34 from SteerSpec/develop":   34,
		"chore: no number":                                0,
		"Merge branch 'develop' into main":                0,
		"feat: overflow (#99999999999999999999999999999)": 0,
	}
	for subject, want := range tests {
		if got := PRNumber(subject); got != want {
			t.Errorf("PRNumber(%q) = %d, want %d", subject, got, want)
		}
	}
}

func filterHistory(t *testing.T) *History {
	t.Helper()
	sup := rule("TST-002", "D", "new", 0)
	sup.Supersedes = ptr("TST-001")
	sub := file("", rule("SUB-001", "D", "s", 0))
	sub.Entity = entity.Entity{ID: "SUB", Parent: "TST"}
	v1 := file("0.1.0", rule("TST-001", "P", "one", 0))
	v2 := file("0.2.0", rule("TST-001", "I", "one", 0))
	v3 := file("0.3.0", rule("TST-001", "R", "one", 0), sup)
	v3.SubEntities = []entity.File{*sub}
	odd := file("draft", rule("TST-001", "R", "one", 0), sup)
	odd.SubEntities = []entity.File{*sub}
	odd.Rules[1].Body = "newer"
	return Build([]Commit{
		commit(t, "c1", "x", nil, v1),
		commit(t, "c2", "x", v1, v2),
		commit(t, "c3", "x", v2, v3),
		commit(t, "c4", "x", v3, odd),
	})
}

func TestFilter(t *testing.T) {
	h := filterHistory(t)
	tests := []struct {
		name string
		f    Filter
		want string
		warn int
	}{
		{"none", Filter{}, "TST-001:added TST-001:promoted TST-001:retired TST-001:superseded TST-002:added SUB-001:added TST-002:body_changed", 0},
		{"rule keeps its superseder", Filter{RuleID: "TST-001"}, "TST-001:added TST-001:promoted TST-001:retired TST-001:superseded TST-002:added", 0},
		{"entity", Filter{Entity: "SUB"}, "SUB-001:added", 0},
		{"since is exclusive", Filter{Since: "0.1.0"}, "TST-001:promoted TST-001:retired TST-001:superseded TST-002:added SUB-001:added", 1},
		{"between", Filter{Since: "0.1.0", Until: "0.2.0"}, "TST-001:promoted", 1},
		{"until is inclusive", Filter{Until: "0.1.0"}, "TST-001:added", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.Filter(tt.f)
			if err != nil {
				t.Fatal(err)
			}
			if kinds(got) != tt.want {
				t.Errorf("got %q, want %q", kinds(got), tt.want)
			}
			if len(got.Warnings) != tt.warn {
				t.Errorf("warnings = %v", got.Warnings)
			}
		})
	}
}

func TestFilterBadBounds(t *testing.T) {
	h := filterHistory(t)
	if _, err := h.Filter(Filter{Since: "nope"}); err == nil || !strings.Contains(err.Error(), "since version") {
		t.Errorf("since: err = %v", err)
	}
	if _, err := h.Filter(Filter{Until: "nope"}); err == nil || !strings.Contains(err.Error(), "until version") {
		t.Errorf("until: err = %v", err)
	}
}

func TestParseRange(t *testing.T) {
	since, until, err := ParseRange("1.0.0..1.2.0")
	if err != nil || since != "1.0.0" || until != "1.2.0" {
		t.Errorf("got %q %q %v", since, until, err)
	}
	if since, until, err = ParseRange("1.0.0.."); err != nil || since != "1.0.0" || until != "" {
		t.Errorf("open end: got %q %q %v", since, until, err)
	}
	if _, _, err = ParseRange("1.0.0"); err == nil {
		t.Error("no separator: want an error")
	}
}

// The JSON form is what agents read, so its shape is pinned.
func TestHistoryJSON(t *testing.T) {
	sup := rule("TST-002", "D", "new", 0)
	sup.Supersedes = ptr("TST-001")
	h := Build([]Commit{
		commit(t, "c1", "feat: add (#1)", file("0.1.0", rule("TST-001", "I", "one", 0)), file("0.2.0", rule("TST-001", "R", "one", 0), sup)),
		{SHA: "c2", Date: day, Subject: "broken", Before: raw(t, file("0.2.0")), After: []byte("x")},
	})
	got, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "events": [
    {
      "entity": "TST",
      "rule_id": "TST-001",
      "kind": "retired",
      "from_state": "I",
      "to_state": "R",
      "revision": 0,
      "rule_set_version": "0.2.0",
      "commit": "c1",
      "date": "2026-03-01T12:00:00Z",
      "pr": 1,
      "subject": "feat: add (#1)"
    },
    {
      "entity": "TST",
      "rule_id": "TST-001",
      "kind": "superseded",
      "to_state": "R",
      "revision": 0,
      "rule_set_version": "0.2.0",
      "superseded_by": "TST-002",
      "commit": "c1",
      "date": "2026-03-01T12:00:00Z",
      "pr": 1,
      "subject": "feat: add (#1)"
    },
    {
      "entity": "TST",
      "rule_id": "TST-002",
      "kind": "added",
      "to_state": "D",
      "revision": 0,
      "rule_set_version": "0.2.0",
      "supersedes": "TST-001",
      "commit": "c1",
      "date": "2026-03-01T12:00:00Z",
      "pr": 1,
      "subject": "feat: add (#1)"
    }
  ],
  "warnings": [
    {
      "commit": "c2",
      "message": "skipped: the file does not parse after the commit: parsing entity JSON: invalid character 'x' looking for beginning of value"
    }
  ]
}`
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// An empty history marshals events as [], never null.
func TestEmptyHistoryJSON(t *testing.T) {
	got, err := json.Marshal(Build(nil))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"events":[]}` {
		t.Errorf("got %s", got)
	}
}
