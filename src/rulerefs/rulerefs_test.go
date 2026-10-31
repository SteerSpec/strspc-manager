package rulerefs_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
)

func strPtr(s string) *string { return &s }

// core is a realm that holds one rule for every case the checker tells apart.
func core() rulerefs.Source {
	return rulerefs.Source{
		RealmID: "dev.steerspec.core",
		Entities: []*entity.File{{
			Entity: entity.Entity{ID: "RUL"},
			Rules: []entity.Rule{
				{ID: "RUL-001", State: entity.StateImplemented},
				{ID: "RUL-002", State: entity.StatePublished},
				{ID: "RUL-003", State: entity.StateDraft},
				{ID: "RUL-004", State: entity.StateAbandoned},
				{ID: "RUL-005", State: entity.StateRetired},
				{ID: "RUL-006", State: entity.StateTerminated},
				// RUL-007 is replaced by a rule in force, RUL-008 by a
				// published one, and RUL-009 by a rule that is not there yet.
				{ID: "RUL-007", State: entity.StateImplemented},
				{ID: "RUL-008", State: entity.StateImplemented},
				{ID: "RUL-009", State: entity.StateImplemented},
				{ID: "RUL-010", State: entity.StateImplemented, Supersedes: strPtr("RUL-007")},
				{ID: "RUL-011", State: entity.StatePublished, Supersedes: strPtr("RUL-008")},
				{ID: "RUL-012", State: entity.StateDraft, Supersedes: strPtr("RUL-009")},
				{ID: "RUL-013", State: entity.StateAbandoned, Supersedes: strPtr("RUL-009")},
				// RUL-005 has the usual end state: retired, with a live superseder.
				{ID: "RUL-014", State: entity.StateImplemented, Supersedes: strPtr("RUL-005")},
			},
			SubEntities: []entity.File{{
				Entity: entity.Entity{ID: "SUB", Parent: "RUL"},
				Rules:  []entity.Rule{{ID: "SUB-001", State: entity.StateImplemented}},
			}},
		}},
	}
}

// finding is the part of a diagnostic the tests compare.
type finding struct {
	Code     string
	Severity result.Severity
	Line     int
}

func findings(res *result.Result) []finding {
	var out []finding
	for _, d := range res.Diagnostics {
		out = append(out, finding{d.Code, d.Severity, d.Line})
	}
	return out
}

func check(t *testing.T, text string, sources []rulerefs.Source, opts ...rulerefs.Option) *result.Result {
	t.Helper()
	c, err := rulerefs.New(sources, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c.CheckText("doc.md", []byte(text))
}

func TestCheckText(t *testing.T) {
	tests := []struct {
		name string
		text string
		want []finding
	}{
		{"implemented", "See RUL-001.", nil},
		{"published", "See RUL-002.", nil},
		{"sub-entity rule", "See SUB-001.", nil},
		{"draft", "See RUL-003.", []finding{{"RF006", result.Warning, 1}}},
		{"abandoned", "See RUL-004.", []finding{{"RF002", result.Error, 1}}},
		{"retired with a live superseder stays a warning", "See RUL-005.", []finding{{"RF003", result.Warning, 1}}},
		{"terminated", "See RUL-006.", []finding{{"RF003", result.Warning, 1}}},
		{"superseder in force", "See RUL-007.", []finding{{"RF004", result.Error, 1}}},
		{"superseder published", "See RUL-008.", []finding{{"RF005", result.Warning, 1}}},
		{"draft and abandoned superseders do not count", "See RUL-009.", nil},

		{"known prefix, unknown number", "See RUL-999.", []finding{{"RF001", result.Error, 1}}},
		{"unknown prefix", "See CLAMS-001.", []finding{{"RF001", result.Error, 1}}},
		{"lookalike reported until ignored", "Uses SHA-256.", []finding{{"RF001", result.Error, 1}}},

		{"rendered form", "See [RUL-001.2/I].", nil},
		{"rendered form carries the real state", "See [RUL-004.0/I].", []finding{{"RF002", result.Error, 1}}},
		{"inline code", "See `RUL-004`.", []finding{{"RF002", result.Error, 1}}},
		{"fenced code", "```\nRUL-004\n```\n", []finding{{"RF002", result.Error, 2}}},
		{"line number", "one\n\nthree RUL-004\n", []finding{{"RF002", result.Error, 3}}},
		{"same id twice on a line", "RUL-004 and RUL-004", []finding{{"RF002", result.Error, 1}}},
		{"same id on two lines", "RUL-004\nRUL-004\n", []finding{{"RF002", result.Error, 1}, {"RF002", result.Error, 2}}},
		{"link text is checked, its URL is not", "[RUL-004](https://example.com/ENG-123)", []finding{{"RF002", result.Error, 1}}},

		{"qualified", "See dev.steerspec.core:RUL-001.", nil},
		{"qualified, judged like any citation", "See dev.steerspec.core:RUL-004.", []finding{{"RF002", result.Error, 1}}},
		{"qualified, not in that realm", "See dev.steerspec.core:ABC-001.", []finding{{"RF001", result.Error, 1}}},
		{"qualified, realm not in scope", "See dev.other.realm:RUL-001.", []finding{{"RF001", result.Error, 1}}},
		{"single-label label is prose", "see:RUL-001 and rule:RUL-004", []finding{{"RF002", result.Error, 1}}},

		{"lowercase slug", "Branch feat-123 and lzhr-456.", nil},
		{"number range", "Takes 100-200 ms, call 800-555.", nil},
		{"mixed case unknown prefix", "Pod web1-123 and Foo-123.", nil},
		{"URL", "See https://linear.app/x/issue/ENG-123 now.", nil},
		{"URL with an uppercase scheme", "See HTTPS://linear.app/x/issue/ENG-123 now.", nil},
		{"embedded before", "The foo-RUL-004 slug.", nil},
		{"embedded after", "The RUL-004-bar slug.", nil},
		{"four digits", "See RUL-0040.", nil},
		{"two digits", "See RUL-04.", nil},
		{"too short a prefix", "See AB-123.", nil},

		{"marker on the line", "RUL-004 <!-- strspc-refs: ignore -->", nil},
		{"marker on the line before", "<!-- strspc-refs: ignore -->\nRUL-004\n", nil},
		{"marker covers one line only", "# strspc-refs: ignore\nRUL-004\nRUL-004\n", []finding{{"RF002", result.Error, 3}}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(check(t, tt.text, []rulerefs.Source{core()}))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDiagnosticFields(t *testing.T) {
	res := check(t, "RUL-005", []rulerefs.Source{core()})
	if len(res.Diagnostics) != 1 {
		t.Fatalf("got %v", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Module != "rule-refs" || d.Path != "doc.md" {
		t.Errorf("module %q path %q", d.Module, d.Path)
	}
	if !strings.Contains(d.Message, "RUL-014") {
		t.Errorf("message %q does not name the superseder", d.Message)
	}
}

func TestStrictPromotesWarnings(t *testing.T) {
	res := check(t, "RUL-003\nRUL-005\nRUL-006\nRUL-008\n", []rulerefs.Source{core()}, rulerefs.WithStrict(true))
	if len(res.Diagnostics) != 4 {
		t.Fatalf("got %v", res.Diagnostics)
	}
	for _, d := range res.Diagnostics {
		if d.Severity != result.Error {
			t.Errorf("%s stayed %s under strict", d.Code, d.Severity)
		}
	}
}

func TestAllowedStates(t *testing.T) {
	tests := []struct {
		name  string
		state string
		text  string
		want  []finding
	}{
		{"abandoned", "A", "RUL-004", nil},
		{"retired", "R", "RUL-005", nil},
		{"draft", "D", "RUL-003", nil},
		{"implemented covers a superseded rule", "I", "RUL-007", nil},
		{"other states stay reported", "R", "RUL-004", []finding{{"RF002", result.Error, 1}}},
		{"unknown ids stay reported", "R", "RUL-999", []finding{{"RF001", result.Error, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(check(t, tt.text, []rulerefs.Source{core()}, rulerefs.WithAllowedStates(tt.state)))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIgnore(t *testing.T) {
	res := check(t, "SHA-256 and RUL-004 and RUL-999", []rulerefs.Source{core()}, rulerefs.WithIgnore("SHA-256", "RUL-004"))
	want := []finding{{"RF001", result.Error, 1}}
	if got := findings(res); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRealmFormats(t *testing.T) {
	realm := func(id string, rif *entity.RuleIdentifierFormat) rulerefs.Source {
		return rulerefs.Source{
			RealmID: "dev.acme.app",
			Format:  rif,
			Entities: []*entity.File{{
				Entity: entity.Entity{ID: id},
				Rules: []entity.Rule{
					{ID: id + "-001", State: entity.StateImplemented},
					{ID: id + "-002", State: entity.StateAbandoned},
				},
			}},
		}
	}
	under := &entity.RuleIdentifierFormat{IDSplitter: "_"}
	closing := &entity.RuleIdentifierFormat{EUIDPosition: "closing"}
	closingUnder := &entity.RuleIdentifierFormat{EUIDPosition: "closing", IDSplitter: "_"}

	tests := []struct {
		name string
		rif  *entity.RuleIdentifierFormat
		text string
		want []finding
	}{
		{"underscore", under, "APP_001", nil},
		{"underscore, judged", under, "APP_002", []finding{{"RF002", result.Error, 1}}},
		{"underscore, unknown number", under, "APP_999", []finding{{"RF001", result.Error, 1}}},
		{"underscore, foreign prefix is not a citation", under, "MAX_100 and HTTP_200", nil},
		{"closing", closing, "002-APP", []finding{{"RF002", result.Error, 1}}},
		{"closing, rendered", closing, "[002-APP.1/A]", []finding{{"RF002", result.Error, 1}}},
		{"closing underscore", closingUnder, "002_APP", []finding{{"RF002", result.Error, 1}}},
		{"stored form still matches", under, "APP-002", []finding{{"RF002", result.Error, 1}}},
		{"qualified with its realm", under, "dev.acme.app:APP_002", []finding{{"RF002", result.Error, 1}}},
		{"qualified with a realm not in scope", under, "dev.other.realm:APP_001", []finding{{"RF001", result.Error, 1}}},
		{"closing, qualified with a realm not in scope", closing, "dev.other.realm:001-APP", []finding{{"RF001", result.Error, 1}}},
		{"default format ignores the other forms", nil, "APP_002 and 002-APP", nil},
		{"a form does not reach another realm", under, "RUL_004", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findings(check(t, tt.text, []rulerefs.Source{realm("APP", tt.rif), core()}))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSourceWithoutRealmID(t *testing.T) {
	source := core()
	source.RealmID = ""
	want := []finding{{"RF002", result.Error, 1}}
	if got := findings(check(t, "RUL-004", []rulerefs.Source{source})); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFirstSourceWins(t *testing.T) {
	own := rulerefs.Source{
		RealmID: "dev.acme.app",
		Entities: []*entity.File{{
			Entity: entity.Entity{ID: "RUL"},
			Rules:  []entity.Rule{{ID: "RUL-004", State: entity.StateImplemented}},
		}},
	}
	if got := findings(check(t, "RUL-004", []rulerefs.Source{own, core()})); got != nil {
		t.Errorf("got %v", got)
	}
}

func TestNewRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		sources []rulerefs.Source
		opts    []rulerefs.Option
	}{
		{"unknown state", nil, []rulerefs.Option{rulerefs.WithAllowedStates("X")}},
		{"bad format", []rulerefs.Source{{Format: &entity.RuleIdentifierFormat{IDSplitter: "+"}}}, nil},
		{"rule in an unknown state", []rulerefs.Source{{Entities: []*entity.File{{
			Entity: entity.Entity{ID: "RUL"},
			Rules:  []entity.Rule{{ID: "RUL-001", State: "X"}},
		}}}}, nil},
		{"sub-entity rule in an unknown state", []rulerefs.Source{{Entities: []*entity.File{{
			Entity: entity.Entity{ID: "RUL"},
			SubEntities: []entity.File{{
				Entity: entity.Entity{ID: "SUB"},
				Rules:  []entity.Rule{{ID: "SUB-001", State: "X"}},
			}},
		}}}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := rulerefs.New(tt.sources, tt.opts...); err == nil {
				t.Error("want an error")
			}
		})
	}
}

// A long line must not cost more than a linear scan, and the position of a
// finding must stay correct after a URL is blanked out.
func TestLongLine(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 2000; i++ {
		b.WriteString("https://example.com/ENG-" + strconv.Itoa(100+i%800) + " ")
	}
	b.WriteString("RUL-004")
	want := []finding{{"RF002", result.Error, 1}}
	if got := findings(check(t, b.String(), []rulerefs.Source{core()})); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
