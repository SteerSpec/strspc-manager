package render

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestDefaultFormat(t *testing.T) {
	f := DefaultRuleIDFormatter()
	r := &entity.Rule{ID: "ENT-001", Revision: 0, State: "D"}
	got := f.Format(r)
	want := "[ENT-001.0/D]"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestFormatVariousStates(t *testing.T) {
	f := DefaultRuleIDFormatter()
	tests := []struct {
		state string
		want  string
	}{
		{"D", "[RUL-003.0/D]"},
		{"A", "[RUL-003.0/A]"},
		{"P", "[RUL-003.0/P]"},
		{"I", "[RUL-003.0/I]"},
		{"R", "[RUL-003.0/R]"},
		{"T", "[RUL-003.0/T]"},
	}
	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			r := &entity.Rule{ID: "RUL-003", Revision: 0, State: tt.state}
			got := f.Format(r)
			if got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatWithRevision(t *testing.T) {
	f := DefaultRuleIDFormatter()
	r := &entity.Rule{ID: "TST-002", Revision: 3, State: "P"}
	got := f.Format(r)
	want := "[TST-002.3/P]"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestCustomBrackets(t *testing.T) {
	f := RuleIDFormatter{
		OpenBracket:      "(",
		CloseBracket:     ")",
		RevisionSplitter: ".",
		StateSplitter:    "/",
	}
	r := &entity.Rule{ID: "ENT-001", Revision: 0, State: "D"}
	got := f.Format(r)
	want := "(ENT-001.0/D)"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestCustomSplitters(t *testing.T) {
	f := RuleIDFormatter{
		OpenBracket:      "{",
		CloseBracket:     "}",
		RevisionSplitter: "#",
		StateSplitter:    "|",
	}
	r := &entity.Rule{ID: "ENT-001", Revision: 2, State: "I"}
	got := f.Format(r)
	want := "{ENT-001#2|I}"
	if got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestFormatEUIDPositionAndIDSplitter(t *testing.T) {
	r := &entity.Rule{ID: "ENT-001", Revision: 1, State: "P"}
	tests := []struct {
		name        string
		idSplitter  string
		euidClosing bool
		want        string
	}{
		{"opening keeps stored form", "-", false, "[ENT-001.1/P]"},
		{"opening underscore", "_", false, "[ENT_001.1/P]"},
		{"closing", "-", true, "[001-ENT.1/P]"},
		{"closing underscore", "_", true, "[001_ENT.1/P]"},
		{"empty splitter keeps dash", "", true, "[001-ENT.1/P]"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := DefaultRuleIDFormatter()
			f.IDSplitter = tt.idSplitter
			f.EUIDClosing = tt.euidClosing
			if got := f.Format(r); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatNonStoredIDUnchanged(t *testing.T) {
	f := DefaultRuleIDFormatter()
	f.IDSplitter = "_"
	f.EUIDClosing = true
	r := &entity.Rule{ID: "not-a-rule-id", Revision: 0, State: "D"}
	if got, want := f.Format(r), "[not-a-rule-id.0/D]"; got != want {
		t.Errorf("Format() = %q, want %q", got, want)
	}
}

func TestNewRuleIDFormatter(t *testing.T) {
	r := &entity.Rule{ID: "ENT-001", Revision: 2, State: "I"}
	tests := []struct {
		name string
		rif  *entity.RuleIdentifierFormat
		want string
	}{
		{"nil keeps defaults", nil, "[ENT-001.2/I]"},
		{"empty keeps defaults", &entity.RuleIdentifierFormat{}, "[ENT-001.2/I]"},
		{"parentheses", &entity.RuleIdentifierFormat{Brackets: "(,)"}, "(ENT-001.2/I)"},
		{"braces", &entity.RuleIdentifierFormat{Brackets: "{,}"}, "{ENT-001.2/I}"},
		{"euid closing", &entity.RuleIdentifierFormat{EUIDPosition: "closing"}, "[001-ENT.2/I]"},
		{"euid opening", &entity.RuleIdentifierFormat{EUIDPosition: "opening"}, "[ENT-001.2/I]"},
		{"id splitter", &entity.RuleIdentifierFormat{IDSplitter: "_"}, "[ENT_001.2/I]"},
		{"revision splitter", &entity.RuleIdentifierFormat{RevisionSplitter: "#"}, "[ENT-001#2/I]"},
		{"backslash state splitter", &entity.RuleIdentifierFormat{StateSplitter: `\`}, `[ENT-001.2\I]`},
		{"pipe state splitter", &entity.RuleIdentifierFormat{StateSplitter: "|"}, "[ENT-001.2|I]"},
		{"all fields", &entity.RuleIdentifierFormat{
			Brackets: "{,}", EUIDPosition: "closing", IDSplitter: "_",
			RevisionSplitter: "#", StateSplitter: "|",
		}, "{001_ENT#2|I}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := NewRuleIDFormatter(tt.rif)
			if err != nil {
				t.Fatalf("NewRuleIDFormatter: %v", err)
			}
			if got := f.Format(r); got != tt.want {
				t.Errorf("Format() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewRuleIDFormatterRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name, field string
		rif         entity.RuleIdentifierFormat
	}{
		{"brackets", "brackets", entity.RuleIdentifierFormat{Brackets: "<,>"}},
		{"euid position", "euid_position", entity.RuleIdentifierFormat{EUIDPosition: "middle"}},
		{"id splitter", "id_splitter", entity.RuleIdentifierFormat{IDSplitter: "."}},
		{"revision splitter", "revision_splitter", entity.RuleIdentifierFormat{RevisionSplitter: "/"}},
		{"state splitter", "state_splitter", entity.RuleIdentifierFormat{StateSplitter: "."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewRuleIDFormatter(&tt.rif)
			if err == nil || !strings.Contains(err.Error(), tt.field) {
				t.Fatalf("err = %v, want one naming %s", err, tt.field)
			}
		})
	}
}
