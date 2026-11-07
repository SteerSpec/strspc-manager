package entityops

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// noteRealm returns a Realm manifest that links one standard to its Standard
// Realm and cites another without one.
func noteRealm() *entity.RealmFile {
	return &entity.RealmFile{
		Realm: entity.RealmMeta{
			ID: "dev.test", Title: "T", Version: "1.0.0",
			Standards: []entity.StandardRef{
				{ID: "owasp.asvs", Relation: entity.RelationImplements, Realm: "dev.steerspec.std.owasp.asvs.v5"},
				{ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom},
			},
		},
	}
}

func TestAddRealmNote(t *testing.T) {
	rf := noteRealm()
	id, err := AddRealmNote(rf, "exclusion", "No UI.", "owasp.asvs:V1.1.1", "github:alice", nil)
	if err != nil {
		t.Fatal(err)
	}
	if id != "REALM/001" {
		t.Errorf("first id = %q, want REALM/001", id)
	}
	want := entity.RealmNote{ID: "REALM/001", Type: "exclusion", Ref: "owasp.asvs:V1.1.1", Content: "No UI.", AddedBy: "github:alice", AddedAt: "2026-03-23", Revision: 0}
	if len(rf.Notes) != 1 || rf.Notes[0] != want {
		t.Fatalf("notes = %+v, want [%+v]", rf.Notes, want)
	}
	if id, err = AddRealmNote(rf, "rationale", "Why.", "", "github:alice", nil); err != nil || id != "REALM/002" {
		t.Errorf("second note = %q, %v, want REALM/002", id, err)
	}
	if rf.Realm.Version != "1.0.0" || rf.Hash != "" {
		t.Errorf("version %q, hash %q: AddRealmNote changed them", rf.Realm.Version, rf.Hash)
	}
}

// The next number is one past the highest, whatever the order or gaps, and an
// identifier of another form doesn't count.
func TestAddRealmNote_NextNumber(t *testing.T) {
	rf := noteRealm()
	rf.Notes = []entity.RealmNote{{ID: "REALM/007"}, {ID: "REALM/002"}, {ID: "REALM/9999"}, {ID: "OTHER/050"}}
	if id, err := AddRealmNote(rf, "clarification", "x", "", "github:alice", nil); err != nil || id != "REALM/008" {
		t.Errorf("got %q, %v, want REALM/008", id, err)
	}
}

func TestAddRealmNote_Full(t *testing.T) {
	rf := noteRealm()
	rf.Notes = []entity.RealmNote{{ID: "REALM/999"}}
	if _, err := AddRealmNote(rf, "rationale", "x", "", "github:alice", nil); err == nil || !strings.Contains(err.Error(), "999") {
		t.Errorf("err = %v, want the 999 limit", err)
	}
	if len(rf.Notes) != 1 {
		t.Errorf("a refused note was added")
	}
}

func TestAddRealmNote_Refused(t *testing.T) {
	tests := []struct {
		name, noteType, content, ref, addedBy string
	}{
		{"unknown type", "standard", "x", "", "github:alice"},
		{"empty type", "", "x", "", "github:alice"},
		{"empty content", "rationale", "", "", "github:alice"},
		{"exclusion without ref", "exclusion", "x", "", "github:alice"},
		{"exclusion ref not <id>:<clause>", "exclusion", "x", "owasp.asvs", "github:alice"},
		{"exclusion of an undeclared standard", "exclusion", "x", "iso.27001:A.5.1", "github:alice"},
		{"exclusion of a standard with no realm", "exclusion", "x", "ietf.rfc2119:2", "github:alice"},
		{"ref on a rationale", "rationale", "x", "owasp.asvs:V1.1.1", "github:alice"},
		{"ref on a clarification", "clarification", "x", "owasp.asvs:V1.1.1", "github:alice"},
		{"ref on a new reference", "reference", "x", "owasp.asvs:V1.1.1", "github:alice"},
		{"empty added_by", "rationale", "x", "", ""},
		{"handle as added_by", "rationale", "x", "", "@alice"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := noteRealm()
			if _, err := AddRealmNote(rf, tt.noteType, tt.content, tt.ref, tt.addedBy, nil); err == nil {
				t.Error("err = nil, want an error")
			}
			if len(rf.Notes) != 0 {
				t.Errorf("a refused note was added: %+v", rf.Notes)
			}
		})
	}
	if _, err := AddRealmNote(nil, "rationale", "x", "", "github:alice", nil); err == nil {
		t.Error("nil realm file: err = nil")
	}
}

// added_by names one of the contributors passed (CTRB-005), which for a
// sub-realm are its parent's, not those of rf.
func TestAddRealmNote_Contributors(t *testing.T) {
	contributors := []entity.Contributor{{ID: "github:alice"}}
	rf := noteRealm()
	if _, err := AddRealmNote(rf, "rationale", "x", "", "github:bob", contributors); err == nil {
		t.Error("github:bob: err = nil, want CTRB-005")
	}
	if _, err := AddRealmNote(rf, "rationale", "x", "", "github:alice", contributors); err != nil {
		t.Errorf("github:alice: %v", err)
	}
	// rf's own list is not the one checked.
	rf.Realm.Contributors = []entity.Contributor{{ID: "github:bob"}}
	if _, err := AddRealmNote(rf, "rationale", "x", "", "github:bob", contributors); err == nil {
		t.Error("github:bob listed in rf only: err = nil, want CTRB-005")
	}
}

// realmWithNotes returns noteRealm with an exclusion REALM/001 and a rationale
// REALM/002.
func realmWithNotes(t *testing.T) *entity.RealmFile {
	t.Helper()
	rf := noteRealm()
	for _, n := range []struct{ typ, content, ref string }{
		{"exclusion", "No UI.", "owasp.asvs:V1.1.1"},
		{"rationale", "Why.", ""},
	} {
		if _, err := AddRealmNote(rf, n.typ, n.content, n.ref, "github:alice", nil); err != nil {
			t.Fatal(err)
		}
	}
	return rf
}

func TestUpdateRealmNote(t *testing.T) {
	tests := []struct {
		name, id, content, noteType string
		wantType, wantContent       string
	}{
		{"content of an exclusion", "REALM/001", "No UI at all.", "", "exclusion", "No UI at all."},
		{"content of a rationale", "REALM/002", "Because.", "", "rationale", "Because."},
		{"withdraw an exclusion", "REALM/001", "", "reference", "reference", "No UI."},
		{"withdraw with the same content given", "REALM/001", "No UI.", "reference", "reference", "No UI."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := realmWithNotes(t)
			before := *findNoteForTest(t, rf, tt.id)
			if err := UpdateRealmNote(rf, tt.id, tt.content, tt.noteType); err != nil {
				t.Fatal(err)
			}
			n := findNoteForTest(t, rf, tt.id)
			if n.Type != tt.wantType || n.Content != tt.wantContent || n.Revision != 1 {
				t.Errorf("got type %q, content %q, revision %d; want %q, %q, 1", n.Type, n.Content, n.Revision, tt.wantType, tt.wantContent)
			}
			if n.Ref != before.Ref || n.AddedBy != before.AddedBy || n.AddedAt != before.AddedAt {
				t.Errorf("ref, added_by or added_at changed: %+v, was %+v", *n, before)
			}
		})
	}
}

func TestUpdateRealmNote_Refused(t *testing.T) {
	tests := []struct {
		name, id, content, noteType string
	}{
		{"unknown id", "REALM/009", "x", ""},
		{"nothing changes", "REALM/002", "", ""},
		{"same content and type", "REALM/002", "Why.", "rationale"},
		{"unknown type", "REALM/002", "", "standard"},
		{"rationale to clarification", "REALM/002", "", "clarification"},
		{"rationale to exclusion", "REALM/002", "", "exclusion"},
		{"exclusion to rationale", "REALM/001", "", "rationale"},
		{"withdraw and change the content", "REALM/001", "Other.", "reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := realmWithNotes(t)
			before := fmt.Sprintf("%+v", rf.Notes)
			if err := UpdateRealmNote(rf, tt.id, tt.content, tt.noteType); err == nil {
				t.Error("err = nil, want an error")
			}
			if after := fmt.Sprintf("%+v", rf.Notes); after != before {
				t.Errorf("a refused update changed the notes:\n%s\nwas\n%s", after, before)
			}
		})
	}
	if err := UpdateRealmNote(nil, "REALM/001", "x", ""); err == nil {
		t.Error("nil realm file: err = nil")
	}
}

// A withdrawn exclusion is a reference for good: it can't be retyped back.
func TestUpdateRealmNote_WithdrawnStays(t *testing.T) {
	rf := realmWithNotes(t)
	if err := UpdateRealmNote(rf, "REALM/001", "", "reference"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRealmNote(rf, "REALM/001", "", "exclusion"); err == nil {
		t.Error("reference to exclusion: err = nil, want an error")
	}
}

// A note whose type is outside the enum can still have its content changed;
// RM002 reports the type.
func TestUpdateRealmNote_KeepsUnknownType(t *testing.T) {
	rf := noteRealm()
	rf.Notes = []entity.RealmNote{{ID: "REALM/001", Type: "other", Content: "x"}}
	if err := UpdateRealmNote(rf, "REALM/001", "y", ""); err != nil {
		t.Fatal(err)
	}
	if n := rf.Notes[0]; n.Type != "other" || n.Content != "y" || n.Revision != 1 {
		t.Errorf("got %+v", n)
	}
}

func findNoteForTest(t *testing.T, rf *entity.RealmFile, id string) *entity.RealmNote {
	t.Helper()
	n, err := findRealmNote(rf, id)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
