package entityops

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestAddNote(t *testing.T) {
	t.Run("adds note at revision 0", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StatePublished)
		f.Rules[0].Revision = 2
		oldVersion := f.RuleSet.Version

		id, err := AddNote(f, "TST-001", "rationale", "Because.", "@alice")
		if err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		if id != "TST-001/01" {
			t.Errorf("id = %q, want TST-001/01", id)
		}
		if len(f.Notes) != 1 {
			t.Fatalf("len(Notes) = %d, want 1", len(f.Notes))
		}
		n := f.Notes[0]
		if n.RuleRef != "TST-001" || n.Type != "rationale" || n.Content != "Because." ||
			n.AddedBy != "@alice" || n.AddedAt != "2026-03-23" || n.Revision != 0 {
			t.Errorf("unexpected note: %+v", n)
		}
		if f.RuleSet.Version == oldVersion {
			t.Error("version not bumped")
		}
		if f.RuleSet.Hash == nil {
			t.Error("hash not set")
		}
	})

	t.Run("numbers sequentially per rule", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)
		if _, err := AddRule(f, "second", "@user"); err != nil {
			t.Fatalf("AddRule: %v", err)
		}
		for _, step := range []struct{ rule, want string }{
			{"TST-001", "TST-001/01"},
			{"TST-002", "TST-002/01"},
			{"TST-001", "TST-001/02"},
		} {
			id, err := AddNote(f, step.rule, "example", "x", "@user")
			if err != nil {
				t.Fatalf("AddNote(%s): %v", step.rule, err)
			}
			if id != step.want {
				t.Errorf("AddNote(%s) = %q, want %q", step.rule, id, step.want)
			}
		}
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		tests := []struct {
			name, rule, typ, content, by, wantErr string
		}{
			{"missing rule", "TST-999", "rationale", "x", "@u", "not found"},
			{"invalid type", "TST-001", "general", "x", "@u", "invalid note type"},
			{"empty content", "TST-001", "rationale", "", "@u", "content"},
			{"empty added_by", "TST-001", "rationale", "x", "", "added_by"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := makeFileWithRule(t, entity.StateDraft)
				_, err := AddNote(f, tt.rule, tt.typ, tt.content, tt.by)
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if len(f.Notes) != 0 {
					t.Error("note added despite error")
				}
			})
		}
	})

	t.Run("rejects more than 99 notes", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)
		f.Notes = append(f.Notes, entity.Note{ID: "TST-001/99", RuleRef: "TST-001", Type: "example"})
		if _, err := AddNote(f, "TST-001", "example", "x", "@u"); err == nil {
			t.Fatal("expected error past note 99")
		}
	})

	t.Run("nil file", func(t *testing.T) {
		if _, err := AddNote(nil, "TST-001", "rationale", "x", "@u"); err == nil {
			t.Fatal("expected error for nil file")
		}
	})
}

func TestUpdateNoteContent(t *testing.T) {
	newFile := func(t *testing.T, noteType string) *entity.File {
		t.Helper()
		f := makeFileWithRule(t, entity.StateImplemented)
		if _, err := AddNote(f, "TST-001", noteType, "original", "@u"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		return f
	}

	t.Run("revises in place", func(t *testing.T) {
		f := newFile(t, "rationale")
		oldVersion := f.RuleSet.Version
		oldHash := *f.RuleSet.Hash

		if err := UpdateNoteContent(f, "TST-001/01", "revised"); err != nil {
			t.Fatalf("UpdateNoteContent: %v", err)
		}
		n := f.Notes[0]
		if n.Content != "revised" || n.Revision != 1 {
			t.Errorf("note = %+v, want content revised at revision 1", n)
		}
		want, _ := BumpPatch(oldVersion)
		if f.RuleSet.Version != want {
			t.Errorf("version = %q, want %q", f.RuleSet.Version, want)
		}
		if *f.RuleSet.Hash == oldHash {
			t.Error("hash not refreshed")
		}

		if err := UpdateNoteContent(f, "TST-001/01", "again"); err != nil {
			t.Fatalf("second UpdateNoteContent: %v", err)
		}
		if f.Notes[0].Revision != 2 {
			t.Errorf("Revision = %d, want 2", f.Notes[0].Revision)
		}
	})

	for _, nt := range []string{"changelog", "supersedes", "extends", "related"} {
		t.Run("rejects append-only "+nt, func(t *testing.T) {
			f := newFile(t, nt)
			if err := UpdateNoteContent(f, "TST-001/01", "changed"); err == nil {
				t.Fatal("expected error for append-only note")
			}
			if f.Notes[0].Content != "original" || f.Notes[0].Revision != 0 {
				t.Errorf("note modified despite error: %+v", f.Notes[0])
			}
		})
	}

	t.Run("rejects invalid input", func(t *testing.T) {
		tests := []struct{ name, id, content, wantErr string }{
			{"missing note", "TST-001/02", "x", "not found"},
			{"empty content", "TST-001/01", "", "content"},
			{"unchanged content", "TST-001/01", "original", "unchanged"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newFile(t, "rationale")
				version := f.RuleSet.Version
				err := UpdateNoteContent(f, tt.id, tt.content)
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				if f.Notes[0].Revision != 0 || f.RuleSet.Version != version {
					t.Error("file modified despite error")
				}
			})
		}
	})

	t.Run("nil file", func(t *testing.T) {
		if err := UpdateNoteContent(nil, "TST-001/01", "x"); err == nil {
			t.Fatal("expected error for nil file")
		}
	})

	t.Run("empty content reports lookup and type errors first", func(t *testing.T) {
		tests := []struct {
			name, noteType, id, wantErr string
			nilFile                     bool
		}{
			{"nil file", "rationale", "TST-001/01", "nil", true},
			{"missing note", "rationale", "TST-001/02", "not found", false},
			{"append-only note", "changelog", "TST-001/01", "cannot change", false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newFile(t, tt.noteType)
				if tt.nilFile {
					f = nil
				}
				err := UpdateNoteContent(f, tt.id, "")
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
			})
		}
	})
}

func TestUpdateNote(t *testing.T) {
	newFile := func(t *testing.T, noteType string) *entity.File {
		t.Helper()
		f := makeFileWithRule(t, entity.StateImplemented)
		if _, err := AddNote(f, "TST-001", noteType, "original", "@u"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		return f
	}

	tests := []struct {
		name, content, noteType string
		wantContent, wantType   string
	}{
		{"type only", "", "clarification", "original", "clarification"},
		{"content only", "revised", "", "revised", "rationale"},
		{"content and type", "revised", "clarification", "revised", "clarification"},
		{"same type with new content", "revised", "rationale", "revised", "rationale"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFile(t, "rationale")
			oldVersion := f.RuleSet.Version
			oldHash := *f.RuleSet.Hash
			if err := UpdateNote(f, "TST-001/01", tt.content, tt.noteType); err != nil {
				t.Fatalf("UpdateNote: %v", err)
			}
			n := f.Notes[0]
			if n.Content != tt.wantContent || n.Type != tt.wantType || n.Revision != 1 {
				t.Errorf("note = %+v, want content %q type %q revision 1", n, tt.wantContent, tt.wantType)
			}
			want, _ := BumpPatch(oldVersion)
			if f.RuleSet.Version != want {
				t.Errorf("version = %q, want %q", f.RuleSet.Version, want)
			}
			if *f.RuleSet.Hash == oldHash {
				t.Error("hash not refreshed")
			}
		})
	}

	t.Run("content edit keeps an out-of-enum type", func(t *testing.T) {
		f := newFile(t, "rationale")
		f.Notes[0].Type = "general" // as loaded from a file; RL010 reports it
		if err := UpdateNoteContent(f, "TST-001/01", "revised"); err != nil {
			t.Fatalf("UpdateNoteContent: %v", err)
		}
		if n := f.Notes[0]; n.Content != "revised" || n.Type != "general" || n.Revision != 1 {
			t.Errorf("note = %+v, want revised content, type general, revision 1", n)
		}
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		tests := []struct{ name, from, content, noteType, wantErr string }{
			{"nothing to change", "rationale", "", "", "unchanged"},
			{"same content and type", "rationale", "original", "rationale", "unchanged"},
			{"invalid type", "rationale", "", "general", "invalid note type"},
			{"retype into append-only", "rationale", "", "changelog", "append-only"},
			{"append-only source", "related", "", "rationale", "cannot change"},
			{"missing note", "rationale", "x", "", "not found"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				f := newFile(t, tt.from)
				version := f.RuleSet.Version
				id := "TST-001/01"
				if tt.name == "missing note" {
					id = "TST-001/02"
				}
				err := UpdateNote(f, id, tt.content, tt.noteType)
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				n := f.Notes[0]
				if n.Type != tt.from || n.Content != "original" || n.Revision != 0 || f.RuleSet.Version != version {
					t.Error("file modified despite error")
				}
			})
		}
	})
}

func TestAddNote_StandardContent(t *testing.T) {
	f := makeFileWithRule(t, entity.StatePublished)
	if _, err := AddNote(f, "TST-001", entity.NoteTypeStandard, "owasp.asvs:2.1.1", "@alice"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if _, err := AddNote(f, "TST-001", entity.NoteTypeStandard, "not a mapping", "@alice"); err == nil {
		t.Fatal("malformed content must be refused (NTE-014)")
	}
}

func TestUpdateNote_StandardRetype(t *testing.T) {
	add := func(t *testing.T, typ, content string) *entity.File {
		f := makeFileWithRule(t, entity.StateImplemented)
		if _, err := AddNote(f, "TST-001", typ, content, "@alice"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		return f
	}
	const noteID = "TST-001/01"

	t.Run("withdraws to reference", func(t *testing.T) {
		f := add(t, entity.NoteTypeStandard, "owasp.asvs:2.1.1")
		if err := UpdateNote(f, noteID, "", entity.NoteTypeReference); err != nil {
			t.Fatalf("UpdateNote: %v", err)
		}
		if f.Rules[0].Revision != 0 {
			t.Errorf("rule revision moved to %d (NTE-018)", f.Rules[0].Revision)
		}
	})
	t.Run("withdrawal keeps the content", func(t *testing.T) {
		f := add(t, entity.NoteTypeStandard, "owasp.asvs:2.1.1")
		if err := UpdateNote(f, noteID, "other", entity.NoteTypeReference); err == nil {
			t.Fatal("a withdrawal must keep its content (NTE-019)")
		}
	})
	t.Run("restores from reference", func(t *testing.T) {
		f := add(t, entity.NoteTypeReference, "owasp.asvs:2.1.1")
		if err := UpdateNote(f, noteID, "", entity.NoteTypeStandard); err != nil {
			t.Fatalf("UpdateNote: %v", err)
		}
	})
	t.Run("refuses any other retype from standard", func(t *testing.T) {
		f := add(t, entity.NoteTypeStandard, "owasp.asvs:2.1.1")
		if err := UpdateNote(f, noteID, "", "rationale"); err == nil {
			t.Fatal("standard to rationale must be refused (NTE-017)")
		}
	})
	t.Run("refuses any other retype to standard", func(t *testing.T) {
		f := add(t, "rationale", "owasp.asvs:2.1.1")
		if err := UpdateNote(f, noteID, "", entity.NoteTypeStandard); err == nil {
			t.Fatal("rationale to standard must be refused (NTE-017)")
		}
	})
	t.Run("revises a clause", func(t *testing.T) {
		f := add(t, entity.NoteTypeStandard, "owasp.asvs:2.1.1")
		if err := UpdateNoteContent(f, noteID, "owasp.asvs:2.1.2"); err != nil {
			t.Fatalf("UpdateNoteContent: %v", err)
		}
	})
	t.Run("refuses malformed revised content", func(t *testing.T) {
		f := add(t, entity.NoteTypeStandard, "owasp.asvs:2.1.1")
		if err := UpdateNoteContent(f, noteID, "2.1.2"); err == nil {
			t.Fatal("malformed content must be refused (NTE-014)")
		}
	})
}

func TestStandardMappingsStayUnique(t *testing.T) {
	f := makeFileWithRule(t, entity.StateImplemented)
	if _, err := AddNote(f, "TST-001", entity.NoteTypeStandard, "owasp.asvs:2.1.1", "@alice"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if _, err := AddNote(f, "TST-001", entity.NoteTypeStandard, "owasp.asvs:2.1.1", "@alice"); err == nil {
		t.Fatal("a duplicate mapping must be refused (NTE-015)")
	}
	if _, err := AddNote(f, "TST-001", entity.NoteTypeReference, "owasp.asvs:2.1.1", "@alice"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if err := UpdateNote(f, "TST-001/02", "", entity.NoteTypeStandard); err == nil {
		t.Fatal("a retype that duplicates a mapping must be refused (NTE-015)")
	}
	if _, err := AddNote(f, "TST-001", entity.NoteTypeStandard, "owasp.asvs:2.1.2", "@alice"); err != nil {
		t.Fatalf("AddNote: %v", err)
	}
	if err := UpdateNoteContent(f, "TST-001/03", "owasp.asvs:2.1.1"); err == nil {
		t.Fatal("a revision that duplicates a mapping must be refused (NTE-015)")
	}
}
