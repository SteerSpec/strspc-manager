package coverage

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
)

// row builds a clause of owasp.asvs 5.0 in prod:AUTH, mapped by AUTH-001/01.
func row(clause string, status Status) Clause {
	return Clause{
		Standard: "owasp.asvs", Version: "5.0", Clause: clause, Realm: "prod", Entity: "AUTH", Status: status,
		Rules: []Mapping{{Rule: "prod:AUTH-001", State: "I", Note: "AUTH-001/01"}},
	}
}

func report(declared []entity.StandardRef, clauses ...Clause) *Report {
	return &Report{Declared: declared, Clauses: clauses}
}

// kinds renders regressions as `kind standard@version:clause realm:entity base>head`.
func kinds(rs []Regression) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, string(r.Kind)+" "+r.Standard+"@"+r.Version+":"+r.Clause+" "+r.Realm+":"+r.Entity+" "+string(r.Base)+">"+string(r.Head))
	}
	return out
}

// TestCompare checks each kind of regression, and what isn't one.
func TestCompare(t *testing.T) {
	asvs := []entity.StandardRef{asvs5}
	excluded := row("1", StatusExcluded)
	excluded.Exclusion = &ExclusionRef{Realm: "prod", Note: "prod/001", Content: "no passwords"}
	moved := func(c Clause) Clause { c.Version = "6.0"; return c }
	asvs6 := entity.StandardRef{ID: "owasp.asvs", Version: "6.0", Relation: entity.RelationImplements}
	asCited := entity.StandardRef{ID: "owasp.asvs", Version: "5.0", Relation: entity.RelationDerivedFrom}
	tests := []struct {
		name       string
		base, head *Report
		want       []string
	}{
		{
			name: "identical reports",
			base: report(asvs, row("1", StatusCovered), row("2", StatusMapped), row("3", StatusLapsed)),
			head: report(asvs, row("1", StatusCovered), row("2", StatusMapped), row("3", StatusLapsed)),
		},
		{
			name: "an improvement",
			base: report(asvs, row("1", StatusMapped), row("2", StatusLapsed), row("3", StatusUnmapped)),
			head: report(asvs, row("1", StatusCovered), row("2", StatusMapped), row("4", StatusCovered)),
		},
		{
			name: "covered to mapped or lapsed, mapped to lapsed",
			base: report(asvs, row("1", StatusCovered), row("2", StatusCovered), row("3", StatusMapped)),
			head: report(asvs, row("1", StatusMapped), row("2", StatusLapsed), row("3", StatusLapsed)),
			want: []string{
				"downgraded owasp.asvs@5.0:1 prod:AUTH covered>mapped",
				"downgraded owasp.asvs@5.0:2 prod:AUTH covered>lapsed",
				"downgraded owasp.asvs@5.0:3 prod:AUTH mapped>lapsed",
			},
		},
		{
			name: "gone, only unmapped, or only cited",
			base: report(asvs, row("1", StatusCovered), row("2", StatusMapped), row("3", StatusCovered)),
			head: report(asvs, row("2", StatusUnmapped), row("3", StatusCited)),
			want: []string{
				"lost owasp.asvs@5.0:1 prod:AUTH covered>",
				"lost owasp.asvs@5.0:2 prod:AUTH mapped>unmapped",
				"lost owasp.asvs@5.0:3 prod:AUTH covered>",
			},
		},
		{
			name: "the same clause in another entity is another clause",
			base: report(asvs, row("1", StatusCovered)),
			head: report(asvs, func() Clause { c := row("1", StatusCovered); c.Realm = "team"; return c }()),
			want: []string{"lost owasp.asvs@5.0:1 prod:AUTH covered>"},
		},
		{
			name: "excluded",
			base: report(asvs, row("1", StatusCovered)),
			head: report(asvs, excluded),
			want: []string{"excluded owasp.asvs@5.0:1 prod:AUTH covered>excluded"},
		},
		{
			name: "a cited clause never regresses",
			base: report([]entity.StandardRef{cited}, Clause{Standard: "ietf.rfc2119", Clause: "1", Realm: "prod", Entity: "KWRD", Status: StatusCited}),
			head: report([]entity.StandardRef{cited}),
		},
		{
			name: "a lapsed clause never regresses",
			base: report(asvs, row("1", StatusLapsed)),
			head: report(asvs),
		},
		{
			name: "a reference gone or now derived_from",
			base: report([]entity.StandardRef{asvs4, asvs5}),
			head: report([]entity.StandardRef{asCited}),
			want: []string{"dropped owasp.asvs@4.0: : >", "dropped owasp.asvs@5.0: : >"},
		},
		{
			name: "a reference whose url changed is the same reference",
			base: report(asvs),
			head: report([]entity.StandardRef{{ID: "owasp.asvs", Version: "5.0", Relation: entity.RelationImplements, URL: "https://example.com"}}),
		},
		{
			name: "an edition upgrade is rebased",
			base: report(asvs, row("1", StatusCovered), row("2", StatusMapped)),
			head: report([]entity.StandardRef{asvs6}, moved(row("1", StatusCovered))),
			want: []string{
				"rebased owasp.asvs@5.0: : >",
				"rebased owasp.asvs@5.0:1 prod:AUTH covered>",
				"rebased owasp.asvs@5.0:2 prod:AUTH mapped>",
			},
		},
		{
			name: "a downgrade within an edition kept beside a new one is still one",
			base: report(asvs, row("1", StatusCovered), row("2", StatusCovered)),
			head: report([]entity.StandardRef{asvs5, asvs6}, row("1", StatusMapped)),
			want: []string{
				"downgraded owasp.asvs@5.0:1 prod:AUTH covered>mapped",
				"lost owasp.asvs@5.0:2 prod:AUTH covered>",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := kinds(Compare(tt.base, tt.head)); !reflect.DeepEqual(got, append([]string{}, tt.want...)) {
				t.Errorf("got  %v\nwant %v", got, tt.want)
			}
		})
	}
}

// TestCompareCarriesTheDetail checks what a regression holds besides its kind.
func TestCompareCarriesTheDetail(t *testing.T) {
	excluded := row("1", StatusExcluded)
	ex := &ExclusionRef{Realm: "prod", Note: "prod/001", Content: "no passwords"}
	excluded.Exclusion = ex
	got := Compare(report(nil, row("1", StatusCovered)), report(nil, excluded))
	want := []Regression{{
		Kind: KindExcluded, Standard: "owasp.asvs", Version: "5.0", Clause: "1", Realm: "prod", Entity: "AUTH",
		Base: StatusCovered, Head: StatusExcluded, Rules: row("1", StatusCovered).Rules, Exclusion: ex,
	}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// TestCompareUnresolved checks a note that stops resolving, once however many
// entities inherit its rule, and not a note new in head, already unresolved,
// or that backed only a citation.
func TestCompareUnresolved(t *testing.T) {
	m := Mapping{Rule: "prod:AUTH-001", State: "I", Note: "AUTH-001/01"}
	inherited := row("1", StatusCovered)
	inherited.Entity = "SAV"
	citation := Clause{Standard: "ietf.rfc2119", Clause: "1", Realm: "prod", Entity: "KWRD", Status: StatusCited,
		Rules: []Mapping{{Rule: "prod:KWRD-001", State: "I", Note: "KWRD-001/01"}}}
	base := report(nil, row("1", StatusCovered), inherited, citation)
	base.Unresolved = []Mapping{{Rule: "prod:AUTH-002", State: "I", Note: "AUTH-002/01"}}
	head := report(nil)
	head.Unresolved = []Mapping{
		{Rule: m.Rule, State: "P", Note: m.Note},
		{Rule: m.Rule, State: "P", Note: m.Note},
		{Rule: "prod:AUTH-002", State: "I", Note: "AUTH-002/01"},
		{Rule: "prod:AUTH-003", State: "I", Note: "AUTH-003/01"},
		{Rule: "prod:KWRD-001", State: "I", Note: "KWRD-001/01"},
	}
	var unresolved []Regression
	for _, r := range Compare(base, head) {
		if r.Kind == KindUnresolved {
			unresolved = append(unresolved, r)
		}
	}
	want := []Regression{{Kind: KindUnresolved, Rules: []Mapping{{Rule: m.Rule, State: "P", Note: m.Note}}}}
	if !reflect.DeepEqual(unresolved, want) {
		t.Errorf("got  %+v\nwant %+v", unresolved, want)
	}
}

// TestComputeQualifiesClausesByRealm checks that two Realm trees with an
// entity of the same EUID give two clauses, which Compare keeps apart.
func TestComputeQualifiesClausesByRealm(t *testing.T) {
	auth := func(state string) *entity.File {
		return file("AUTH", []entity.StandardRef{asvs5}, []entity.Rule{rule("AUTH-001", state)}, map[string][]string{"AUTH-001": {"owasp.asvs:1"}})
	}
	measure := func(prod, team string) *Report {
		return Compute([]*realmresolve.Inheritance{chain(hop("team", auth(team))), chain(hop("prod", auth(prod)))})
	}
	base := measure("I", "I")
	var realms []string
	for _, c := range base.Clauses {
		realms = append(realms, c.Realm+":"+c.Entity)
	}
	if want := []string{"prod:AUTH", "team:AUTH"}; !reflect.DeepEqual(realms, want) {
		t.Fatalf("clauses = %v, want %v", realms, want)
	}
	want := []string{"downgraded owasp.asvs@5.0:1 team:AUTH covered>mapped"}
	if got := kinds(Compare(base, measure("I", "P"))); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// TestCompareDecodedBaseline decodes a report in the shape
// `strspc standards coverage --json` prints, with the fields Compare doesn't
// read, and compares it with a computed one.
func TestCompareDecodedBaseline(t *testing.T) {
	const saved = `{
	  "evaluator": {"provider": "anthropic", "usable": true, "runs_on_implemented": true},
	  "declared": [{"id": "owasp.asvs", "relation": "implements", "version": "5.0"}],
	  "clauses": [
	    {"standard": "owasp.asvs", "version": "5.0", "clause": "1", "realm": "prod", "entity": "AUTH", "status": "covered",
	     "rules": [{"rule": "prod:AUTH-001", "state": "I", "note": "AUTH-001/01"}]},
	    {"standard": "owasp.asvs", "version": "5.0", "clause": "2", "realm": "prod", "entity": "AUTH", "status": "excluded",
	     "rules": [], "exclusion": {"realm": "prod", "note": "prod/001", "content": "n/a"}}
	  ],
	  "figures": [{"tree": "prod", "standard": "owasp.asvs", "version": "5.0", "realm": "owasp.asvs", "total": 2, "excluded": 1,
	     "covered": 1, "mapped": 0, "lapsed": 0, "unmapped": 0, "figure": 1}],
	  "unresolved": [],
	  "warnings": []
	}`
	var base Report
	if err := json.Unmarshal([]byte(saved), &base); err != nil {
		t.Fatal(err)
	}
	if ex := base.Clauses[1].Exclusion; ex == nil || ex.Note != "prod/001" {
		t.Errorf("Exclusion = %+v", ex)
	}
	f := file("AUTH", []entity.StandardRef{asvs5}, []entity.Rule{rule("AUTH-001", "I")}, map[string][]string{"AUTH-001": {"owasp.asvs:1"}})
	head := Compute([]*realmresolve.Inheritance{chain(&inherit.Hop{RealmID: "prod", Entity: f})})
	if got := Compare(&base, head); len(got) != 0 {
		t.Errorf("Compare = %+v, want none", got)
	}
	f.Rules[0].State = "R"
	head = Compute([]*realmresolve.Inheritance{chain(&inherit.Hop{RealmID: "prod", Entity: f})})
	if got, want := kinds(Compare(&base, head)), []string{"downgraded owasp.asvs@5.0:1 prod:AUTH covered>lapsed"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

// TestReportRoundTrips checks that a Report survives its JSON form.
func TestReportRoundTrips(t *testing.T) {
	r := report([]entity.StandardRef{asvs5}, row("1", StatusCovered))
	r.Unresolved = []Mapping{{Rule: "prod:AUTH-002", State: "P", Note: "AUTH-002/01"}}
	r.Figures = []Figure{{Tree: "prod", Standard: "owasp.asvs", Version: "5.0", Realm: "owasp.asvs", Covered: 1, Total: 1, Defined: true}}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var back Report
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(&back, r) {
		t.Errorf("got  %+v\nwant %+v", &back, r)
	}
}
