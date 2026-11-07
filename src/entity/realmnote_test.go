package entity

import (
	"slices"
	"testing"
)

func TestRealmNoteTypes(t *testing.T) {
	want := []string{"exclusion", "rationale", "clarification", "reference"}
	got := RealmNoteTypes()
	if !slices.Equal(got, want) {
		t.Fatalf("RealmNoteTypes() = %v, want %v", got, want)
	}
	got[0] = "changed"
	if RealmNoteTypes()[0] != "exclusion" {
		t.Error("RealmNoteTypes() returned the package's own slice")
	}
	for _, typ := range want {
		if !IsValidRealmNoteType(typ) {
			t.Errorf("IsValidRealmNoteType(%q) = false, want true", typ)
		}
	}
	// A rule note type outside NTE-031, and the empty string, are not Realm
	// Note types.
	for _, typ := range []string{"standard", "example", "changelog", "", "Exclusion"} {
		if IsValidRealmNoteType(typ) {
			t.Errorf("IsValidRealmNoteType(%q) = true, want false", typ)
		}
	}
	// exclusion is a Realm Note type only (NTE-032).
	if IsValidNoteType(NoteTypeExclusion) {
		t.Error("IsValidNoteType(exclusion) = true, want false")
	}
}

func TestCheckExclusionRef(t *testing.T) {
	standards := []StandardRef{
		{ID: "owasp.asvs", Relation: RelationImplements, Realm: "dev.steerspec.std.owasp.asvs.v5"},
		{ID: "ietf.rfc2119", Relation: RelationDerivedFrom},
	}
	tests := []struct {
		ref     string
		wantErr bool
	}{
		{"owasp.asvs:V1.1.1", false},
		{"owasp.asvs:V1 with spaces after", false},
		{"", true},                   // No ref.
		{"owasp.asvs", true},         // No colon.
		{"owasp.asvs:", true},        // No clause.
		{"owasp.asvs: V1.1.1", true}, // Clause starts with a space.
		{":V1.1.1", true},            // No id.
		{"iso.27001:A.5.1", true},    // Undeclared standard.
		{"ietf.rfc2119:2", true},     // Standard with no realm.
	}
	for _, tt := range tests {
		err := CheckExclusionRef(tt.ref, standards)
		if (err != nil) != tt.wantErr {
			t.Errorf("CheckExclusionRef(%q) error = %v, wantErr %v", tt.ref, err, tt.wantErr)
		}
	}
}
