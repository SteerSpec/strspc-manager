package entityops

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// asvs is the manifest of a Standard Realm declaring three profiles.
func asvs() *entity.RealmMeta {
	return &entity.RealmMeta{
		ID: "org.owasp.asvs", Title: "ASVS", Version: "0.1.0", Kind: entity.KindStandard,
		License: "CC-BY-SA-4.0", Attribution: &entity.Attribution{Text: "OWASP ASVS 5.0.0 by the OWASP Foundation", URL: "https://owasp.org/www-project-application-security-verification-standard/"},
		Edition: "5.0.0", Profiles: []string{"L1", "L2", "L3"},
	}
}

func standardFile(t *testing.T) *entity.File {
	t.Helper()
	f, err := NewFile("V01S01", "Encoding", "", "")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestAddRuleWith(t *testing.T) {
	f := standardFile(t)
	plain := standardFile(t)
	if _, err := AddRule(plain, "The system MUST encode output.", "github:alice"); err != nil {
		t.Fatal(err)
	}
	profiles := []string{"L2", "L1"}
	id, err := AddRuleWith(f, "The system MUST encode output.", "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: profiles, Realm: asvs()})
	if err != nil {
		t.Fatal(err)
	}
	profiles[0] = "changed"
	r := f.Rules[0]
	if id != "V01S01-001" || r.Clause != "V1.1.1" || !reflect.DeepEqual(r.Profiles, []string{"L2", "L1"}) || r.State != entity.StateDraft {
		t.Errorf("rule = %+v, id %s", r, id)
	}
	if f.RuleSet.Version != "0.1.1" {
		t.Errorf("version = %s, want 0.1.1", f.RuleSet.Version)
	}
	if f.RuleSet.Hash == nil || plain.RuleSet.Hash == nil || *f.RuleSet.Hash == *plain.RuleSet.Hash {
		t.Error("the hash doesn't cover clause and profiles")
	}

	// A second rule of the clause, with its profiles in another order.
	if _, err := AddRuleWith(f, "The system MUST escape output.", "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", "L2"}, Realm: asvs()}); err != nil {
		t.Errorf("same profiles in another order: %v", err)
	}
	// A Standard Realm that declares no profiles takes rules without any.
	bare := asvs()
	bare.Profiles = nil
	if _, err := AddRuleWith(standardFile(t), "x", "github:alice", AddRuleOptions{Clause: "V1.1.1", Realm: bare}); err != nil {
		t.Errorf("no profiles in a Realm without profiles: %v", err)
	}
	// A code Realm's manifest, with nothing to set, adds a plain rule.
	if _, err := AddRuleWith(standardFile(t), "x", "github:alice", AddRuleOptions{Realm: &entity.RealmMeta{ID: "dev.test"}}); err != nil {
		t.Errorf("code Realm: %v", err)
	}
}

func TestAddRuleWith_Rejected(t *testing.T) {
	code := &entity.RealmMeta{ID: "dev.test"}
	withRule := func(r entity.Rule) func(*entity.File) {
		return func(f *entity.File) { f.Rules = append(f.Rules, r) }
	}
	tests := []struct {
		name  string
		opts  AddRuleOptions
		setup func(*entity.File)
		want  string
	}{
		{name: "clause without a Realm", opts: AddRuleOptions{Clause: "V1.1.1"}, want: "RUL-017"},
		{name: "profiles in a code Realm", opts: AddRuleOptions{Profiles: []string{"L1"}, Realm: code}, want: "RUL-017"},
		{name: "no clause", opts: AddRuleOptions{Profiles: []string{"L1"}, Realm: asvs()}, want: "STDRLM-005"},
		{name: "blank clause", opts: AddRuleOptions{Clause: "  ", Profiles: []string{"L1"}, Realm: asvs()}, want: "STDRLM-005"},
		{name: "clause with a trailing space", opts: AddRuleOptions{Clause: "V1.1.1 ", Profiles: []string{"L1"}, Realm: asvs()}, want: "RUL-015"},
		{name: "no profiles", opts: AddRuleOptions{Clause: "V1.1.1", Realm: asvs()}, want: "STDRLM-018"},
		{name: "blank profile", opts: AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", " "}, Realm: asvs()}, want: "RUL-016"},
		{name: "profile twice", opts: AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", "L1"}, Realm: asvs()}, want: "RUL-016"},
		{name: "undeclared profile", opts: AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L4"}, Realm: asvs()}, want: "STDRLM-008"},
		{
			name: "a live rule of the clause has other profiles",
			opts: AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1"}, Realm: asvs()},
			setup: withRule(entity.Rule{ID: "V01S01-001", State: entity.StatePublished, Body: "x", AddedBy: "github:a", AddedAt: "2026-01-01",
				Clause: "V1.1.1", Profiles: []string{"L1", "L2"}}),
			want: "STDRLM-009",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := standardFile(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			before := len(f.Rules)
			_, err := AddRuleWith(f, "x", "github:alice", tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one citing %s", err, tt.want)
			}
			if len(f.Rules) != before || f.RuleSet.Version != "0.1.0" {
				t.Error("the file changed")
			}
		})
	}
}

// A rule of the clause that isn't live, such as a retired one, doesn't hold
// the clause's profiles.
func TestAddRuleWith_IgnoresRulesNotLive(t *testing.T) {
	f := standardFile(t)
	f.Rules = append(f.Rules, entity.Rule{ID: "V01S01-001", State: entity.StateRetired, Body: "x", AddedBy: "github:a", AddedAt: "2026-01-01",
		Clause: "V1.1.1", Profiles: []string{"L3"}})
	if _, err := AddRuleWith(f, "x", "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1"}, Realm: asvs()}); err != nil {
		t.Error(err)
	}
}

func TestSupersedeRule_KeepsClauseAndProfiles(t *testing.T) {
	f := standardFile(t)
	id, err := AddRuleWith(f, "The system MUST encode output.", "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", "L2"}, Realm: asvs()})
	if err != nil {
		t.Fatal(err)
	}
	if err := PromoteRule(f, id); err != nil {
		t.Fatal(err)
	}
	fix, err := SupersedeRule(f, id, "The system MUST encode all output.", "github:alice")
	if err != nil {
		t.Fatal(err)
	}
	f.Rules[0].Profiles[0] = "changed"
	r := f.Rules[1]
	if r.ID != fix || r.Clause != "V1.1.1" || !reflect.DeepEqual(r.Profiles, []string{"L1", "L2"}) {
		t.Errorf("fix = %+v", r)
	}

	plain := standardFile(t)
	if _, err := AddRule(plain, "x", "github:alice"); err != nil {
		t.Fatal(err)
	}
	if err := PromoteRule(plain, "V01S01-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := SupersedeRule(plain, "V01S01-001", "y", "github:alice"); err != nil {
		t.Fatal(err)
	}
	if r := plain.Rules[1]; r.Clause != "" || r.Profiles != nil {
		t.Errorf("a code Realm's fix got %q %v", r.Clause, r.Profiles)
	}
}

// A Standard Realm written only through AddRuleWith and SupersedeRule passes
// rule-lint, schema validation included.
func TestAddRuleWith_LintsClean(t *testing.T) {
	f := standardFile(t)
	for _, body := range []string{"The system MUST encode output.", "The system MUST escape output."} {
		if _, err := AddRuleWith(f, body, "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", "L2"}, Realm: asvs()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := AddRuleWith(f, "The system MUST log.", "github:alice", AddRuleOptions{Clause: "V1.2.1", Profiles: []string{"L3"}, Realm: asvs()}); err != nil {
		t.Fatal(err)
	}
	if err := PromoteRule(f, "V01S01-001"); err != nil {
		t.Fatal(err)
	}
	if _, err := SupersedeRule(f, "V01S01-001", "The system MUST encode all output.", "github:alice"); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	write := func(name string, v any) {
		data, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("realm.json", &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: *asvs()})
	write("V01S01.json", f)

	offline := &http.Client{Transport: roundTripper(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("offline")
	})}
	fetcher := schema.New(schema.WithLocalDir(filepath.Join("..", "..", "rules")), schema.WithCacheDir(t.TempDir()),
		schema.WithBaseURL("http://127.0.0.1:1"), schema.WithClient(offline))
	res := rulelint.New(rulelint.WithSchemaFetcher(fetcher)).LintRealm(t.Context(), dir)
	for _, d := range res.Diagnostics {
		if d.Severity == result.Error {
			t.Errorf("%s: %s", d.Code, d.Message)
		}
	}
}

// roundTripper is an http.RoundTripper made of one function.
type roundTripper func(*http.Request) (*http.Response, error)

// RoundTrip calls f.
func (f roundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
