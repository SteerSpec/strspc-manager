package entityops

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// overridingFile is a file whose Draft rule TST-001, at revision rev,
// overrides an inherited rule.
func overridingFile(t *testing.T, rev int) *entity.File {
	t.Helper()
	f := makeFileWithRule(t, entity.StateDraft)
	target := "dev.example.lib:BASE-001"
	f.Entity.Inherits = "dev.example.lib:BASE"
	f.Rules[0].Overrides = &target
	f.Rules[0].Revision = rev
	return f
}

func TestAddNote_OverrideAck(t *testing.T) {
	t.Run("on an overriding rule", func(t *testing.T) {
		f := overridingFile(t, 0)
		if _, err := AddNote(f, "TST-001", entity.NoteTypeOverrideAck, "0", "github:alice"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
	})
	t.Run("on a rule without overrides (NTE-043)", func(t *testing.T) {
		f := makeFileWithRule(t, entity.StateDraft)
		_, err := AddNote(f, "TST-001", entity.NoteTypeOverrideAck, "0", "github:alice")
		if err == nil || !strings.Contains(err.Error(), "NTE-043") {
			t.Fatalf("err = %v, want an NTE-043 refusal", err)
		}
	})
	t.Run("cannot change once added (NTE-044)", func(t *testing.T) {
		f := overridingFile(t, 0)
		id, err := AddNote(f, "TST-001", entity.NoteTypeOverrideAck, "0", "github:alice")
		if err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		if err := UpdateNoteContent(f, id, "1"); err == nil {
			t.Fatal("UpdateNoteContent changed an override_ack note")
		}
	})
}

func TestPromoteRule_OverrideNeedsAck(t *testing.T) {
	t.Run("refused without an acknowledgement (RUL-022)", func(t *testing.T) {
		f := overridingFile(t, 1)
		err := PromoteRule(f, "TST-001")
		if err == nil || !strings.Contains(err.Error(), "RUL-022") {
			t.Fatalf("err = %v, want a RUL-022 refusal", err)
		}
		if f.Rules[0].State != entity.StateDraft {
			t.Errorf("state = %q, want D", f.Rules[0].State)
		}
	})
	t.Run("refused with an older revision acknowledged", func(t *testing.T) {
		f := overridingFile(t, 1)
		if _, err := AddNote(f, "TST-001", entity.NoteTypeOverrideAck, "0", "github:alice"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		if err := PromoteRule(f, "TST-001"); err == nil {
			t.Fatal("PromoteRule published with only revision 0 acknowledged at revision 1")
		}
	})
	t.Run("allowed with the current revision acknowledged", func(t *testing.T) {
		f := overridingFile(t, 1)
		if _, err := AddNote(f, "TST-001", entity.NoteTypeOverrideAck, "1", "github:alice"); err != nil {
			t.Fatalf("AddNote: %v", err)
		}
		if err := PromoteRule(f, "TST-001"); err != nil {
			t.Fatalf("PromoteRule: %v", err)
		}
	})
}
