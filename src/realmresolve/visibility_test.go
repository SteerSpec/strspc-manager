package realmresolve

import (
	"context"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestPermissions(t *testing.T) {
	tests := []struct {
		visibility        string
		rel               relation
		inherit, override bool
	}{
		{entity.VisibilityPublic, sameRealm, true, true},
		{entity.VisibilityPublic, sameTree, true, true},
		{entity.VisibilityPublic, otherRealm, true, true},
		{entity.VisibilityProtected, sameRealm, true, true},
		{entity.VisibilityProtected, sameTree, true, true},
		{entity.VisibilityProtected, otherRealm, true, false},
		{entity.VisibilityPrivate, sameRealm, true, true},
		{entity.VisibilityPrivate, sameTree, true, false},
		{entity.VisibilityPrivate, otherRealm, false, false},
		// An unknown value permits nothing, even from inside the Realm.
		{"internal", sameRealm, false, false},
	}
	for _, tt := range tests {
		if got := mayInherit(tt.visibility, tt.rel); got != tt.inherit {
			t.Errorf("mayInherit(%s, %d) = %v, want %v", tt.visibility, tt.rel, got, tt.inherit)
		}
		if got := mayOverride(tt.visibility, tt.rel); got != tt.override {
			t.Errorf("mayOverride(%s, %d) = %v, want %v", tt.visibility, tt.rel, got, tt.override)
		}
	}
}

func TestRelationOf(t *testing.T) {
	tests := []struct {
		name     string
		from, to place
		want     relation
	}{
		{"same Realm", place{"dev.r", "dev.r"}, place{"dev.r", "dev.r"}, sameRealm},
		{"sub-realm from parent", place{"dev.r", "dev.r"}, place{"dev.r", "dev.r.a"}, sameTree},
		{"parent from sub-realm", place{"dev.r", "dev.r.a"}, place{"dev.r", "dev.r"}, sameTree},
		{"sibling", place{"dev.r", "dev.r.a"}, place{"dev.r", "dev.r.b"}, sameTree},
		{"dependency", place{"dev.r", "dev.r"}, place{"dev.d", "dev.d"}, otherRealm},
	}
	for _, tt := range tests {
		if got := relationOf(tt.from, tt.to); got != tt.want {
			t.Errorf("%s: got %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestNodeVisibility(t *testing.T) {
	file := func(id, parent, visibility string) *entity.File {
		return &entity.File{Entity: entity.Entity{ID: id, Parent: parent, Visibility: visibility}}
	}
	top, narrow, mid, leaf := file("TOP", "", ""), file("NAR", "", entity.VisibilityPrivate), file("MID", "NAR", ""), file("LEAF", "MID", "")
	loop := file("LOOP", "LOOP", "")
	n := &node{entities: map[string]*entity.File{"TOP": top, "NAR": narrow, "MID": mid, "LEAF": leaf, "LOOP": loop}}

	tests := []struct {
		e    *entity.File
		want string
	}{
		{top, entity.VisibilityPublic},
		{narrow, entity.VisibilityPrivate},
		{mid, entity.VisibilityPrivate},
		{leaf, entity.VisibilityPrivate},
		{file("ORPHAN", "NOPE", ""), entity.VisibilityPublic},
		{loop, entity.VisibilityPublic},
	}
	for _, tt := range tests {
		if got := n.visibility(tt.e); got != tt.want {
			t.Errorf("visibility(%s) = %q, want %q", tt.e.Entity.ID, got, tt.want)
		}
	}
}

func TestVisibility(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "visibility/root"), absTestdata(t, "visibility/root"))

	tests := []struct{ code, substr string }{
		// ENT-016: the inherited Entity must permit the inheritance.
		{"RR022", "entity UPRIV: inherits dev.lib:LPRIV, whose visibility `private` does not permit it from realm dev.root"},
		// ENT-015: not wider than the inherited Entity.
		{"RR024", "entity UWIDE: visibility `public` is wider than `protected` of inherited entity dev.lib:LPROT"},
		// ENT-016: an override needs the owner's permission, Draft target and
		// superseded declaring rule included.
		{"RR023", "rule UPROT-001 overrides dev.lib:LPROT-001: visibility `protected` of dev.lib:LPROT does not permit it from realm dev.root"},
		{"RR023", "rule UPROT-002 overrides dev.lib:LPROT-002"},
		{"RR023", "rule UPROT-003 overrides dev.lib:LPROT-001"},
		// ENT-016: an intermediate Entity binds a Rule it does not own.
		{"RR023", "rule UCHAIN-001 overrides dev.lib:LPUB-001: visibility `protected` of dev.lib:LMID does not permit it from realm dev.root"},
		// ENT-014: a sub-realm is outside the Realm for a `private` override.
		{"RR023", "rule AUP-001 overrides dev.root:RPRIV-001: visibility `private` of dev.root:RPRIV does not permit it from realm dev.root.a"},
		{"RR023", "rule BSIB-001 overrides dev.root:APRIV-001: visibility `private` of dev.root:APRIV does not permit it from realm dev.root.b"},
		// A dependency that breaks the rules itself doesn't launder a Rule to
		// whoever inherits from it.
		{"RR022", "entity UDEEP (through dev.lib:LWIDE): inherits dev.deep:DPRIV, whose visibility `private` does not permit it from realm dev.lib"},
		{"RR024", "entity UDEEP (through dev.lib:LWIDE): visibility `public` is wider than `private` of inherited entity dev.deep:DPRIV"},
		{"RR023", "rule LWIDE-001 overrides dev.deep:DPRIV-001: visibility `private` of dev.deep:DPRIV does not permit it from realm dev.lib"},
		// A dependency's sub-realm is a Realm of its own for a `private` override.
		{"RR022", "entity UCHILD: inherits dev.lib:LUP"},
		{"RR023", "rule LUP-001 overrides dev.lib:LCHILD-001: visibility `private` of dev.lib:LCHILD does not permit it from realm dev.lib"},
		// NTE-012: narrower target, also unqualified, and an unreachable one.
		{"RR025", "note NOTES-001/01 references \"RPRIV-001\": visibility `private` is narrower than `public` of entity NOTES"},
		{"RR025", "note NOTES-002/01 references \"dev.lib:LPRIV-001\": visibility `private` does not permit inheritance from realm dev.root"},
		{"RR025", "note NPRIV-002/01 references \"dev.lib:LPRIV-001\": visibility `private` does not permit inheritance from realm dev.root"},
		// NTE-012 keeps a superseded rule under the check.
		{"RR025", "note NOTES-004/01 references \"dev.lib:LPROT-001\": visibility `protected` is narrower than `public` of entity NOTES"},
		// ENT-011: a sub-entity takes the visibility of its container.
		{"RR025", "note NOTES-007/01 references \"dev.lib:LSUB-001\": visibility `protected` is narrower than `public` of entity NOTES"},
	}
	for _, tt := range tests {
		got := diagsFor(res, tt.code, tt.substr)
		if len(got) != 1 {
			t.Errorf("%s %q: got %d diagnostics, want 1", tt.code, tt.substr, len(got))
			continue
		}
		if got[0].Severity != result.Error {
			t.Errorf("%s %q: severity = %v, want Error", tt.code, tt.substr, got[0].Severity)
		}
	}

	// Anything else stays silent: UKID, as UWIDE answers for itself; UPUB, ROWN (stopping at the owner keeps AOWN
	// from being judged by the Entity above ROWN), ASAME in its own Realm,
	// APROT within the tree, the Retired UPROT-005, NOTES-003's related note,
	// the Retired NOTES-006, and NPRIV-001 within the tree.
	var got int
	for _, d := range res.Diagnostics {
		switch d.Code {
		case "RR022", "RR023", "RR024", "RR025":
			got++
		}
	}
	if got != len(tests) {
		t.Errorf("got %d visibility diagnostics, want %d: %v", got, len(tests), res.Diagnostics)
	}
}
