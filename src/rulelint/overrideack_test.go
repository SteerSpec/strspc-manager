package rulelint

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// ackFile is an entity with one rule, overriding target when it is not empty,
// and one `override_ack` note on it.
func ackFile(target string) *entity.File {
	f := mappedFile("ACKED", nil, "0")
	f.Notes[0].Type = entity.NoteTypeOverrideAck
	if target != "" {
		f.Entity.Inherits = "dev.example.lib:BASE"
		f.Rules[0].Overrides = &target
	}
	return f
}

func TestRL044_OverrideAckNeedsOverrides(t *testing.T) {
	if got := codes(New().LintFile(ackFile("")))["RL044"]; got != 1 {
		t.Errorf("RL044 on a rule without overrides fired %d times, want 1", got)
	}
	if got := codes(New().LintFile(ackFile("dev.example.lib:BASE-001")))["RL044"]; got != 0 {
		t.Errorf("RL044 on an overriding rule fired %d times, want 0", got)
	}
}

func TestOverrideAckIsAValidNoteType(t *testing.T) {
	if got := codes(New().LintFile(ackFile("dev.example.lib:BASE-001")))["RL010"]; got != 0 {
		t.Errorf("RL010 fired %d times for an override_ack note, want 0", got)
	}
}
