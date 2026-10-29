package inherit

import (
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func ptr(s string) *string { return &s }

func rule(id, state string, overrides *string) entity.Rule {
	return entity.Rule{ID: id, State: state, Body: id, AddedBy: "t", AddedAt: "2026-10-29", Overrides: overrides}
}

func hop(realm, id string, rules ...entity.Rule) Hop {
	return Hop{RealmID: realm, Entity: &entity.File{Entity: entity.Entity{ID: id, Title: id}, Rules: rules}}
}

// ids lists the qualified IDs of set, in order.
func ids(set []Rule) []string {
	out := make([]string, 0, len(set))
	for _, r := range set {
		out = append(out, r.QualifiedID())
	}
	return out
}

func TestInherited_PublishedAndImplementedOnly(t *testing.T) {
	acct := hop("core", "ACCT",
		rule("ACCT-001", "D", nil), rule("ACCT-002", "P", nil), rule("ACCT-003", "I", nil),
		rule("ACCT-004", "R", nil), rule("ACCT-005", "A", nil), rule("ACCT-006", "T", nil))
	got := ids(Inherited([]Hop{acct}))
	want := []string{"core:ACCT-002", "core:ACCT-003"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Inherited = %v, want %v", got, want)
	}
}

func TestInherited_AncestorOverridesApplyRootFirst(t *testing.T) {
	// SAV (prod) → ACCT (bank) → BASE (core). ACCT-001 overrides BASE-001;
	// BASE-002 stays.
	base := hop("core", "BASE", rule("BASE-001", "I", nil), rule("BASE-002", "P", nil))
	acct := hop("bank", "ACCT", rule("ACCT-001", "I", ptr("core:BASE-001")))
	got := Inherited([]Hop{acct, base})
	if want := []string{"core:BASE-002", "bank:ACCT-001"}; !reflect.DeepEqual(ids(got), want) {
		t.Fatalf("Inherited = %v, want %v", ids(got), want)
	}
	if got[1].Overrides != "core:BASE-001" {
		t.Errorf("ACCT-001 Overrides = %q, want core:BASE-001", got[1].Overrides)
	}
}

func TestInherited_DraftAncestorOverrideIgnored(t *testing.T) {
	base := hop("core", "BASE", rule("BASE-001", "I", nil))
	acct := hop("bank", "ACCT", rule("ACCT-001", "D", ptr("core:BASE-001")))
	if got := ids(Inherited([]Hop{acct, base})); !reflect.DeepEqual(got, []string{"core:BASE-001"}) {
		t.Errorf("Inherited = %v, want only core:BASE-001", got)
	}
}

func TestEffective(t *testing.T) {
	acct := hop("core", "ACCT", rule("ACCT-001", "I", nil), rule("ACCT-002", "I", nil))
	tests := []struct {
		name   string
		child  []entity.Rule
		states []string
		want   []string
		over   map[string]string // qualified ID → Overrides
	}{
		{
			name:  "own rules after inherited, override applied",
			child: []entity.Rule{rule("SAV-001", "D", ptr("core:ACCT-001")), rule("SAV-002", "P", nil)},
			want:  []string{"core:ACCT-002", "prod:SAV-001", "prod:SAV-002"},
			over:  map[string]string{"prod:SAV-001": "core:ACCT-001"},
		},
		{
			name:  "retired override lets the target reappear",
			child: []entity.Rule{rule("SAV-001", "R", ptr("core:ACCT-001"))},
			want:  []string{"core:ACCT-001", "core:ACCT-002"},
		},
		{
			name:   "states filter applies before overrides",
			child:  []entity.Rule{rule("SAV-001", "D", ptr("core:ACCT-001"))},
			states: []string{"P", "I"},
			want:   []string{"core:ACCT-001", "core:ACCT-002"},
		},
		{
			name:  "dangling override keeps nothing",
			child: []entity.Rule{rule("SAV-001", "D", ptr("core:ACCT-009"))},
			want:  []string{"core:ACCT-001", "core:ACCT-002", "prod:SAV-001"},
			over:  map[string]string{"prod:SAV-001": ""},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			child := hop("prod", "SAV", tt.child...)
			got := Effective(child, []Hop{acct}, tt.states)
			if !reflect.DeepEqual(ids(got), tt.want) {
				t.Fatalf("Effective = %v, want %v", ids(got), tt.want)
			}
			for _, r := range got {
				if r.Owner == "ACCT" && r.InheritedBy != "SAV" {
					t.Errorf("%s InheritedBy = %q, want SAV", r.QualifiedID(), r.InheritedBy)
				}
				if r.Owner == "SAV" && r.InheritedBy != "" {
					t.Errorf("%s InheritedBy = %q, want empty", r.QualifiedID(), r.InheritedBy)
				}
				if want, ok := tt.over[r.QualifiedID()]; ok && r.Overrides != want {
					t.Errorf("%s Overrides = %q, want %q", r.QualifiedID(), r.Overrides, want)
				}
			}
		})
	}
}

func TestEffective_ShortAndSubRealmOverrideForms(t *testing.T) {
	// Same-realm parent: a short override resolves in the child's namespace,
	// and a sub-realm qualifier normalises onto the realm tree.
	acct := hop("dev.bank", "ACCT", rule("ACCT-001", "I", nil), rule("ACCT-002", "I", nil))
	acct.SubRealmIDs = []string{"dev.bank.auth"}
	child := hop("dev.bank", "SAV",
		rule("SAV-001", "D", ptr("ACCT-001")),
		rule("SAV-002", "D", ptr("dev.bank.auth:ACCT-002")))
	got := ids(Effective(child, []Hop{acct}, nil))
	if want := []string{"dev.bank:SAV-001", "dev.bank:SAV-002"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Effective = %v, want %v", got, want)
	}
}

func TestNormalise_OnlyDeclaredSubRealms(t *testing.T) {
	ns := namespaces([]Hop{{RealmID: "dev.bank", SubRealmIDs: []string{"dev.bank.auth"}}, {RealmID: "dev.bank.core"}})
	for in, want := range map[string]string{
		"dev.bank.auth:X-001": "dev.bank:X-001",      // declared sub-realm
		"dev.bank.core:X-001": "dev.bank.core:X-001", // a namespace itself
		"dev.bank.fake:X-001": "dev.bank.fake:X-001", // shares the prefix only
	} {
		if got := normalise(in, ns); got != want {
			t.Errorf("normalise(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestEffective_PrefixRealmDoesNotReplaceLocalRule(t *testing.T) {
	acct := hop("dev.bank", "ACCT", rule("ACCT-001", "I", nil))
	child := hop("dev.bank", "SAV", rule("SAV-001", "D", ptr("dev.bank.fake:ACCT-001")))
	got := ids(Effective(child, []Hop{acct}, nil))
	if want := []string{"dev.bank:ACCT-001", "dev.bank:SAV-001"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Effective = %v, want %v", got, want)
	}
}

func TestEffective_NotesFollowTheirRule(t *testing.T) {
	acct := hop("core", "ACCT", rule("ACCT-001", "I", nil))
	acct.Entity.Notes = []entity.Note{{ID: "ACCT-001/01", RuleRef: "ACCT-001", Type: "rationale", Content: "why"}}
	got := Effective(hop("prod", "SAV"), []Hop{acct}, nil)
	if len(got) != 1 || len(got[0].Notes) != 1 || got[0].Notes[0].Content != "why" {
		t.Errorf("notes not carried: %+v", got)
	}
}

func TestOverriddenBy(t *testing.T) {
	acct := hop("core", "ACCT", rule("ACCT-001", "I", nil), rule("ACCT-002", "I", nil))
	sav := Effective(hop("prod", "SAV", rule("SAV-001", "D", ptr("core:ACCT-001"))), []Hop{acct}, nil)
	loan := Effective(hop("prod", "LOAN", rule("LOAN-001", "P", ptr("core:ACCT-001"))), []Hop{acct}, nil)
	got := OverriddenBy(sav, loan, sav)
	want := map[string][]string{"core:ACCT-001": {"prod:LOAN-001", "prod:SAV-001"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OverriddenBy = %v, want %v", got, want)
	}
}
