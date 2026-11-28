package entityops

import (
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// publishedWith returns an entity file whose rule V01S01-001 is Published with
// the clause V1.1.1 and the profiles L1 and L2.
func publishedWith(t *testing.T) *entity.File {
	t.Helper()
	f := standardFile(t)
	id, err := AddRuleWith(f, "The system MUST encode output.", "github:alice", AddRuleOptions{Clause: "V1.1.1", Profiles: []string{"L1", "L2"}, Realm: asvs()})
	if err != nil {
		t.Fatal(err)
	}
	if err := PromoteRule(f, id); err != nil {
		t.Fatal(err)
	}
	return f
}

// SupersedeRuleWith keeps the clause and profiles in a Standard Realm
// (STDRLM-009, STDRLM-012) and drops them elsewhere, so the successor of a
// code Realm's rule that has them, which RUL-017 forbids, has neither.
func TestSupersedeRuleWith(t *testing.T) {
	code := &entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}
	tests := []struct {
		name     string
		realm    *entity.RealmMeta
		clause   string
		profiles []string
	}{
		{"Standard Realm", asvs(), "V1.1.1", []string{"L1", "L2"}},
		{"code Realm", code, "", nil},
		{"no Realm", nil, "", nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := publishedWith(t)
			id, err := SupersedeRuleWith(f, "V01S01-001", "The system MUST encode all output.", "github:alice", SupersedeOptions{Realm: tc.realm})
			if err != nil {
				t.Fatal(err)
			}
			r, err := findRule(f, id)
			if err != nil {
				t.Fatal(err)
			}
			if r.Clause != tc.clause || !reflect.DeepEqual(r.Profiles, tc.profiles) {
				t.Errorf("successor clause %q, profiles %q; want %q, %q", r.Clause, r.Profiles, tc.clause, tc.profiles)
			}
			if r.State != entity.StateDraft || r.Supersedes == nil || *r.Supersedes != "V01S01-001" {
				t.Errorf("successor = %+v", r)
			}
			if old := f.Rules[0]; old.Clause != "V1.1.1" || len(old.Profiles) != 2 {
				t.Errorf("the superseded rule lost its clause or profiles: %+v", old)
			}
		})
	}
	t.Run("errors as SupersedeRule", func(t *testing.T) {
		f := publishedWith(t)
		if _, err := SupersedeRuleWith(f, "V01S01-009", "x", "github:alice", SupersedeOptions{}); err == nil {
			t.Error("superseding a missing rule passed")
		}
	})
}
