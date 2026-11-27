package rulediff

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
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

// TestRD033_OverrideAckIsFrozen checks that a change to any field of an
// `override_ack` note present in the base is RD033, citing NTE-044, with no
// second diagnostic from RD013 or RD016, and that removal stays RD012.
func TestRD033_OverrideAckIsFrozen(t *testing.T) {
	tests := []struct {
		name  string
		edit  func(n *entity.Note)
		field string
	}{
		{"content", func(n *entity.Note) { n.Content = "1"; n.Revision++ }, "content"},
		{"rule_ref", func(n *entity.Note) { n.RuleRef = "TSTENT-002" }, "rule_ref"},
		{"added_by", func(n *entity.Note) { n.AddedBy = "github:renamed" }, "added_by"},
		{"added_at", func(n *entity.Note) { n.AddedAt = "2026-02-02" }, "added_at"},
		{"revision", func(n *entity.Note) { n.Revision = 1 }, "revision"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := overridingBase(0)
			base.Notes = append(base.Notes, ackNote("TSTENT-001/02", "0"))
			head := makeHead(base)
			tt.edit(&head.Notes[1])
			res := New().Diff(base, head)
			d := findCode(res, "RD033")
			if d == nil {
				t.Fatalf("no RD033; got %v", codes(res))
			}
			if !strings.Contains(d.Message, tt.field) || !strings.Contains(d.Message, "NTE-044") {
				t.Errorf("message %q names neither %s nor NTE-044", d.Message, tt.field)
			}
			if d.Path != "TSTENT/notes/TSTENT-001/02" {
				t.Errorf("path = %q", d.Path)
			}
			assertNoCode(t, res, "RD013")
			assertNoCode(t, res, "RD016")
		})
	}

	base := overridingBase(0)
	base.Notes = append(base.Notes, ackNote("TSTENT-001/02", "0"))
	head := makeHead(base)
	assertNoCode(t, New().Diff(base, head), "RD033")
	head.Notes = head.Notes[:1]
	res := New().Diff(base, head)
	assertHasCode(t, res, "RD012")
	assertNoCode(t, res, "RD033")
}

// TestRD033_OtherAppendOnlyNotesTakeAnAddedByRewrite checks that an
// `added_by` rewrite on a note of another append-only type stays allowed
// (NTE-010/01).
func TestRD033_OtherAppendOnlyNotesTakeAnAddedByRewrite(t *testing.T) {
	for _, typ := range []string{"changelog", "supersedes", "extends", "related"} {
		t.Run(typ, func(t *testing.T) {
			base := makeBase()
			base.Notes = append(base.Notes, entity.Note{ID: "TSTENT-001/02", RuleRef: "TSTENT-001", Type: typ,
				Content: "TSTENT-001", AddedBy: "test@example.com", AddedAt: "2026-01-01"})
			head := makeHead(base)
			head.Notes[1].AddedBy = "github:renamed"
			res := New().Diff(base, head)
			if len(res.Errors()) > 0 {
				t.Errorf("errors: %v", codes(res))
			}
		})
	}
}

func findCode(res *result.Result, code string) *result.Diagnostic {
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Code == code {
			return &res.Diagnostics[i]
		}
	}
	return nil
}

func codes(res *result.Result) []string {
	out := make([]string, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

// TestRD001_OverridesEditMovesTheRevision checks that a Draft rule's
// `overrides` changes only with its revision, by one, and apart from a state
// change, so that an override_ack names what it acknowledges (RUL-022).
func TestRD001_OverridesEditMovesTheRevision(t *testing.T) {
	other := "dev.example.lib:BASE-002"
	tests := []struct {
		name  string
		rev   int
		state string
		want  bool
	}{
		{"retargeted with the revision raised", 1, "D", false},
		{"retargeted with the revision kept", 0, "D", true},
		{"retargeted in the change that publishes it", 1, "P", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := overridingBase(0)
			base.Notes = append(base.Notes, ackNote("TSTENT-001/02", "0"))
			head := makeHead(base)
			head.Rules[0].Overrides = &other
			head.Rules[0].Revision = tt.rev
			head.Rules[0].State = tt.state
			if tt.state == "P" {
				head.Notes = append(head.Notes, ackNote("TSTENT-001/03", "1"))
			}
			res := New().Diff(base, head)
			if tt.want {
				assertHasCode(t, res, "RD001")
			} else {
				assertNoCode(t, res, "RD001")
			}
		})
	}
}
