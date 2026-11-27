package entityops

import (
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
)

// ackFile returns migrationFile with an `override_ack` note by @pleberre on
// TST-001 and on TSTSUB-001, and a note of each other append-only type by
// @pleberre on TST-001, each file's hash current.
func ackFile(t *testing.T) *entity.File {
	t.Helper()
	f := migrationFile(t)
	target := "dev.lib:LIB-001"
	note := func(rule string, n int, typ, content string) entity.Note {
		return entity.Note{ID: fmt.Sprintf("%s/%02d", rule, 10+n), RuleRef: rule, Type: typ,
			Content: content, AddedBy: "@pleberre", AddedAt: "2026-11-01"}
	}
	sub := &f.SubEntities[0]
	sub.Rules[0].Overrides = &target
	sub.Notes = append(sub.Notes, note(sub.Rules[0].ID, 0, entity.NoteTypeOverrideAck, "0"))
	if err := UpdateMeta(sub); err != nil {
		t.Fatal(err)
	}
	id := f.Rules[0].ID
	f.Rules[0].Overrides = &target
	f.Notes = append(f.Notes,
		note(id, 1, entity.NoteTypeOverrideAck, "2"),
		note(id, 2, "changelog", "Changed."),
		note(id, 3, "supersedes", "TST-000"),
		note(id, 4, "extends", "TST-002"),
		note(id, 5, "related", "TST-002"))
	if err := UpdateMeta(f); err != nil {
		t.Fatal(err)
	}
	return f
}

// A rewrite leaves every `override_ack` note as it was and lists it, and
// rewrites the notes of the other append-only types (NTE-044/01, NTE-010/01).
func TestRewriteAddedByReport_KeepsOverrideAcks(t *testing.T) {
	f := ackFile(t)
	baseData, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	pinned := nowFunc
	nowFunc = func() time.Time { return pinned().Add(time.Hour) }
	t.Cleanup(func() { nowFunc = pinned })

	rw, err := RewriteAddedByReport(f, map[string]string{"@pleberre": "github:pleberre"})
	if err != nil {
		t.Fatal(err)
	}
	// TST-001, its rationale, TSTSUB-001 and the four other append-only notes.
	if rw.Replaced != 7 {
		t.Errorf("Replaced = %d, want 7", rw.Replaced)
	}
	want := []KeptNote{
		{EntityID: "TSTSUB", NoteID: f.SubEntities[0].Rules[0].ID + "/10", AddedBy: "@pleberre"},
		{EntityID: "TST", NoteID: f.Rules[0].ID + "/11", AddedBy: "@pleberre"},
	}
	if !slices.Equal(rw.Kept, want) {
		t.Errorf("Kept = %+v, want %+v", rw.Kept, want)
	}
	for _, n := range append(slices.Clone(f.Notes), f.SubEntities[0].Notes...) {
		wantBy := "github:pleberre"
		if n.Type == entity.NoteTypeOverrideAck {
			wantBy = "@pleberre"
		}
		if n.AddedBy != wantBy {
			t.Errorf("note %s (%s) added_by = %q, want %q", n.ID, n.Type, n.AddedBy, wantBy)
		}
	}

	// rule-diff accepts the migration: no error and no warning.
	headData, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range rulediff.Compare(baseData, headData).Diagnostics {
		if d.Severity != result.Info {
			t.Errorf("rule-diff: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

// A file whose only matches are `override_ack` notes stays as it was: no
// version bump and no new hash.
func TestRewriteAddedByReport_OnlyAcksMatch(t *testing.T) {
	f := ackFile(t)
	for i := range f.Notes {
		if f.Notes[i].Type != entity.NoteTypeOverrideAck {
			f.Notes[i].AddedBy = "github:other"
		}
	}
	f.Rules[0].AddedBy = "github:other"
	sub := &f.SubEntities[0]
	sub.Rules[0].AddedBy = "github:other"
	if err := UpdateMeta(sub); err != nil {
		t.Fatal(err)
	}
	if err := UpdateMeta(f); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(f)

	rw, err := RewriteAddedByReport(f, map[string]string{"@pleberre": "github:pleberre"})
	if err != nil {
		t.Fatal(err)
	}
	if rw.Replaced != 0 || len(rw.Kept) != 2 {
		t.Errorf("Replaced = %d, Kept = %+v; want 0 and both acks", rw.Replaced, rw.Kept)
	}
	if after, _ := json.Marshal(f); string(after) != string(before) {
		t.Error("the file changed although only override_ack notes matched")
	}
}

// RewriteAddedBy counts what RewriteAddedByReport replaces and skips the
// same `override_ack` notes.
func TestRewriteAddedBy_SkipsOverrideAcks(t *testing.T) {
	f := ackFile(t)
	n, err := RewriteAddedBy(f, map[string]string{"@pleberre": "github:pleberre"})
	if err != nil {
		t.Fatal(err)
	}
	if n != 7 {
		t.Errorf("rewrote %d, want 7", n)
	}
	if f.Notes[1].Type != entity.NoteTypeOverrideAck || f.Notes[1].AddedBy != "@pleberre" || f.SubEntities[0].Notes[0].AddedBy != "@pleberre" {
		t.Error("an override_ack note was rewritten")
	}
}
