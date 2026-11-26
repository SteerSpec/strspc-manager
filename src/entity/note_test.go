package entity

import "testing"

func TestNoteTypes(t *testing.T) {
	got := NoteTypes()
	if len(got) != 14 {
		t.Fatalf("NoteTypes() has %d entries, want 14", len(got))
	}
	for _, nt := range got {
		if !IsValidNoteType(nt) {
			t.Errorf("IsValidNoteType(%q) = false for a listed type", nt)
		}
	}
	got[0] = "mutated"
	if NoteTypes()[0] != "rationale" {
		t.Error("NoteTypes() must return a fresh copy")
	}
}

func TestIsValidNoteType(t *testing.T) {
	if IsValidNoteType("general") {
		t.Error(`IsValidNoteType("general") = true, want false`)
	}
	if IsValidNoteType("") {
		t.Error(`IsValidNoteType("") = true, want false`)
	}
}

func TestIsAppendOnlyNoteType(t *testing.T) {
	for _, nt := range NoteTypes() {
		want := nt == "changelog" || nt == "supersedes" || nt == "extends" || nt == "related" || nt == "override_ack"
		if got := IsAppendOnlyNoteType(nt); got != want {
			t.Errorf("IsAppendOnlyNoteType(%q) = %v, want %v", nt, got, want)
		}
	}
}
