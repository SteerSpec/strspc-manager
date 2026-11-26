package rulediff

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// overridingBase is makeBase with TSTENT-001, a Draft rule, overriding an
// inherited rule, at revision rev.
func overridingBase(rev int) *entity.File {
	base := makeBase()
	target := "dev.example.lib:BASE-001"
	base.Entity.Inherits = "dev.example.lib:BASE"
	base.Rules[0].Overrides = &target
	base.Rules[0].Revision = rev
	return base
}

func ackNote(id, content string) entity.Note {
	return entity.Note{
		ID: id, RuleRef: "TSTENT-001", Type: entity.NoteTypeOverrideAck, Content: content,
		AddedBy: "test@example.com", AddedAt: "2026-01-01",
	}
}

func TestRD032_OverrideEntersPublished(t *testing.T) {
	tests := []struct {
		name  string
		rev   int
		acks  []entity.Note
		state string
		want  bool
	}{
		{"published without an acknowledgement", 0, nil, "P", true},
		{"published with the current revision acknowledged", 0, []entity.Note{ackNote("TSTENT-001/02", "0")}, "P", false},
		{"published with only an older revision acknowledged", 1, []entity.Note{ackNote("TSTENT-001/02", "0")}, "P", true},
		{"published with an older and the current revision acknowledged", 1, []entity.Note{ackNote("TSTENT-001/02", "0"), ackNote("TSTENT-001/03", "1")}, "P", false},
		{"stays in Draft without an acknowledgement", 0, nil, "D", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := overridingBase(tt.rev)
			head := makeHead(base)
			head.Rules[0].State = tt.state
			head.Notes = append(head.Notes, tt.acks...)
			res := New().Diff(base, head)
			if tt.want {
				assertHasCode(t, res, "RD032")
			} else {
				assertNoCode(t, res, "RD032")
			}
		})
	}
}

func TestRD032_NotForARuleWithoutOverrides(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.Rules[0].State = "P"
	assertNoCode(t, New().Diff(base, head), "RD032")
}

func TestOverrideAckIsAppendOnly(t *testing.T) {
	base := overridingBase(0)
	base.Notes = append(base.Notes, ackNote("TSTENT-001/02", "0"))
	head := makeHead(base)
	head.Notes[1].Content = "1"
	assertHasCode(t, New().Diff(base, head), "RD016")

	head = makeHead(base)
	head.Notes = head.Notes[:1]
	assertHasCode(t, New().Diff(base, head), "RD012")
}
