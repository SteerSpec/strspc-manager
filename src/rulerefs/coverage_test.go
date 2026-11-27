package rulerefs_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
)

// coverageSources returns a local Realm whose entity CLM holds a rule in each
// of several states and a sub-entity CLMSUB, and a dependency that holds a
// rule of the same ID as one of CLM's and an entity DEP.
func coverageSources() []rulerefs.Source {
	return []rulerefs.Source{
		{
			RealmID: "dev.example.app",
			Local:   true,
			Entities: []*entity.File{
				{
					Entity: entity.Entity{ID: "CLM", Standards: []entity.StandardRef{{ID: "mitre.cwe"}}},
					Rules: []entity.Rule{
						{ID: "CLM-001", State: entity.StateImplemented},
						{ID: "CLM-002", State: entity.StatePublished},
						{ID: "CLM-003", State: entity.StateDraft},
						{ID: "CLM-004", State: entity.StateAbandoned},
						{ID: "CLM-005", State: entity.StateRetired},
					},
					SubEntities: []entity.File{{
						Entity: entity.Entity{ID: "CLMSUB", Parent: "CLM"},
						Rules:  []entity.Rule{{ID: "CLMSUB-001", State: entity.StateImplemented}},
					}},
				},
				{
					Entity: entity.Entity{ID: "OTH"},
					Rules:  []entity.Rule{{ID: "OTH-001", State: entity.StateImplemented}},
				},
			},
		},
		{
			RealmID: "dev.example.lib",
			Entities: []*entity.File{{
				Entity: entity.Entity{ID: "DEP"},
				Rules: []entity.Rule{
					{ID: "CLM-003", State: entity.StateImplemented},
					{ID: "DEP-001", State: entity.StateImplemented},
				},
			}},
		},
	}
}

// uncited returns the rule IDs that the RF008 diagnostics of res name, in order,
// and fails on any other diagnostic.
func uncited(t *testing.T, res *result.Result) []string {
	t.Helper()
	var out []string
	for _, d := range res.Diagnostics {
		if d.Code != "RF008" || d.Severity != result.Error || d.Module != "rule-refs" {
			t.Errorf("unexpected diagnostic %s %v %s: %s", d.Code, d.Severity, d.Module, d.Message)
			continue
		}
		id, _, _ := strings.Cut(d.Message, " ")
		out = append(out, id)
	}
	return out
}

func coverage(t *testing.T, c *rulerefs.Checker, entities, states []string) []string {
	t.Helper()
	res, err := c.Coverage(entities, states)
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	return uncited(t, res)
}

func newChecker(t *testing.T, opts ...rulerefs.Option) *rulerefs.Checker {
	t.Helper()
	c, err := rulerefs.New(coverageSources(), opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// A rule of a named entity in a covered state that no checked text cites is
// RF008; a citation of a sub-entity rule counts toward its parent, and a
// citation counts whatever the rule's state.
func TestCoverage_ReportsUncitedRules(t *testing.T) {
	c := newChecker(t)
	c.CheckText("a.md", []byte("Apply CLM-001 and CLMSUB-001."))
	c.CheckText("b.md", []byte("Draft: CLM-003."))
	if got, want := coverage(t, c, []string{"CLM"}, nil), []string{"CLM-002"}; !reflect.DeepEqual(got, want) {
		t.Errorf("uncited = %v, want %v", got, want)
	}

	res, err := c.Coverage([]string{"CLM"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg := res.Diagnostics[0].Message; !strings.Contains(msg, "CLM-002 (entity CLM)") {
		t.Errorf("message %q does not name the rule and its entity", msg)
	}
}

// Empty states means D, P and I; other states select what must be cited.
func TestCoverage_States(t *testing.T) {
	c := newChecker(t)
	if got, want := coverage(t, c, []string{"CLM"}, nil), []string{"CLM-001", "CLM-002", "CLM-003", "CLMSUB-001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("default states: uncited = %v, want %v", got, want)
	}
	if got, want := coverage(t, c, []string{"CLM"}, []string{"I", "R"}), []string{"CLM-001", "CLM-005", "CLMSUB-001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("I and R: uncited = %v, want %v", got, want)
	}
}

// A rule named through both its entity and its parent is reported once, and
// rules follow the order of the sources, parents before sub-entities.
func TestCoverage_ParentAndSubEntityNamed(t *testing.T) {
	c := newChecker(t)
	if got, want := coverage(t, c, []string{"CLMSUB", "OTH", "CLM"}, []string{"I"}), []string{"CLM-001", "CLMSUB-001", "OTH-001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("uncited = %v, want %v", got, want)
	}
}

// Only a real citation counts: not an ignored identifier, not an ignored line,
// not a standard's clause and not a citation that resolves to a dependency.
func TestCoverage_WhatIsNotACitation(t *testing.T) {
	tests := []struct {
		name string
		text string
		opts []rulerefs.Option
		rule string
	}{
		{"ignored identifier", "CLM-002 is a part number.", []rulerefs.Option{rulerefs.WithIgnore("CLM-002")}, "CLM-002"},
		{"ignored line", "<!-- strspc-refs: ignore -->\nCLM-002", nil, "CLM-002"},
		{"standard clause", "See mitre.cwe:CLM-002.", nil, "CLM-002"},
		{"dependency rule", "See dev.example.lib:CLM-003.", nil, "CLM-003"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newChecker(t, tt.opts...)
			c.CheckText("a.md", []byte(tt.text))
			got := coverage(t, c, []string{"CLM"}, []string{"P", "D"})
			if !slicesContains(got, tt.rule) {
				t.Errorf("uncited = %v, want %s among them", got, tt.rule)
			}
		})
	}

	// A qualified citation of the local Realm counts.
	c := newChecker(t)
	c.CheckText("a.md", []byte("See dev.example.app:CLM-003."))
	if got := coverage(t, c, []string{"CLM"}, []string{"D"}); len(got) != 0 {
		t.Errorf("uncited = %v, want none", got)
	}
}

func slicesContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ResetCoverage forgets the citations seen so far, so that one Checker serves
// one batch of files after another.
func TestCoverage_Reset(t *testing.T) {
	c := newChecker(t)
	c.CheckText("a.md", []byte("CLM-001 CLM-002 CLM-003 CLMSUB-001"))
	if got := coverage(t, c, []string{"CLM"}, nil); len(got) != 0 {
		t.Fatalf("uncited = %v, want none", got)
	}
	c.ResetCoverage()
	if got := coverage(t, c, []string{"CLM"}, []string{"I"}); !reflect.DeepEqual(got, []string{"CLM-001", "CLMSUB-001"}) {
		t.Errorf("after reset: uncited = %v", got)
	}
}

// An entity that no local source holds, a dependency's included, and a state
// outside the lifecycle are configuration errors.
func TestCoverage_Errors(t *testing.T) {
	c := newChecker(t)
	for _, tt := range []struct {
		name     string
		entities []string
		states   []string
	}{
		{"unknown entity", []string{"CLM", "NOPE"}, nil},
		{"dependency entity", []string{"DEP"}, nil},
		{"unknown state", []string{"CLM"}, []string{"X"}},
		{"no entity", nil, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := c.Coverage(tt.entities, tt.states); err == nil {
				t.Error("no error")
			}
		})
	}
}

// A rule of a later local source that an earlier one shadows, for its bare ID
// and, when it has one, for its Realm's qualifier, can never be cited, so
// Coverage leaves it out, as New's first-source-wins resolution does.
func TestCoverage_ShadowedRulesAreLeftOut(t *testing.T) {
	src := func(realm string) rulerefs.Source {
		return rulerefs.Source{RealmID: realm, Local: true, Entities: []*entity.File{{
			Entity: entity.Entity{ID: "CLM"},
			Rules:  []entity.Rule{{ID: "CLM-001", State: entity.StateImplemented}},
		}}}
	}
	for _, tt := range []struct {
		name  string
		first string
		later string
		text  string
		want  int
	}{
		{"same realm, qualified citation", "dev.example.app", "dev.example.app", "See dev.example.app:CLM-001.", 0},
		{"same realm, bare citation", "dev.example.app", "dev.example.app", "See CLM-001.", 0},
		{"no realm, bare citation", "", "", "See CLM-001.", 0},
		// The later copy is still reachable by its own Realm's qualifier.
		{"other realm, bare citation", "dev.example.app", "dev.example.app.sub", "See CLM-001.", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, err := rulerefs.New([]rulerefs.Source{src(tt.first), src(tt.later)})
			if err != nil {
				t.Fatal(err)
			}
			c.CheckText("a.md", []byte(tt.text))
			if got := coverage(t, c, []string{"CLM"}, nil); len(got) != tt.want {
				t.Errorf("uncited = %v, want %d", got, tt.want)
			}
		})
	}
}
