package realmresolve

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// TestStandardScopes checks the declarations around an entity, nearest first.
func TestStandardScopes(t *testing.T) {
	own := []entity.StandardRef{{ID: "own.ref", Relation: entity.RelationImplements}}
	e := &entity.File{Entity: entity.Entity{ID: "INNER", Parent: "MIDDLE", Standards: own}}
	mid := &entity.File{Entity: entity.Entity{ID: "MIDDLE", Parent: "OUTER"}, SubEntities: []entity.File{*e}}
	outer := &entity.File{
		Entity:      entity.Entity{ID: "OUTER", Standards: []entity.StandardRef{{ID: "outer.ref", Relation: entity.RelationImplements}}},
		SubEntities: []entity.File{*mid},
	}
	realm := &entity.RealmFile{Realm: entity.RealmMeta{Standards: []entity.StandardRef{{ID: "realm.ref", Relation: entity.RelationImplements}}}}
	parent := &entity.RealmFile{Realm: entity.RealmMeta{Standards: []entity.StandardRef{{ID: "parent.ref", Relation: entity.RelationImplements}}}}
	entities := map[string]*entity.File{"OUTER": outer, "MIDDLE": mid, "INNER": e}

	scopes := standardScopes(e, entities, realm, parent)
	for _, id := range []string{"outer.ref", "realm.ref", "parent.ref"} {
		if entity.ResolveStandard(id, scopes...) == nil {
			t.Errorf("%s not found", id)
		}
	}
	if entity.ResolveStandard("own.ref", scopes...) != nil {
		t.Error("the entity's own references are not part of its scopes")
	}
}

// TestStandardScopes_ContainmentOnly checks that a `parent` naming an entity
// that doesn't hold this one contributes nothing, as in RL031.
func TestStandardScopes_ContainmentOnly(t *testing.T) {
	other := &entity.File{Entity: entity.Entity{ID: "OTHER", Standards: []entity.StandardRef{{ID: "other.ref", Relation: entity.RelationImplements}}}}
	e := &entity.File{Entity: entity.Entity{ID: "LOOSE", Parent: "OTHER"}}
	realm := &entity.RealmFile{}
	scopes := standardScopes(e, map[string]*entity.File{"OTHER": other, "LOOSE": e}, realm, nil)
	if entity.ResolveStandard("other.ref", scopes...) != nil {
		t.Error("an entity that doesn't contain this one must not contribute")
	}
}

// TestResolve_Entities checks that every entity is listed, with or without a
// chain.
func TestResolve_Entities(t *testing.T) {
	for _, name := range []string{"inherit-chain/x", "note-refs-solo"} {
		t.Run(name, func(t *testing.T) {
			out, _ := New().Resolve(t.Context(), loadTestRealm(t, name), absTestdata(t, name))
			if len(out.Entities) == 0 {
				t.Fatal("no entities")
			}
			withChain := 0
			for _, e := range out.Entities {
				if len(e.Ancestors) > 0 {
					withChain++
				}
				if e.Path == "" {
					t.Errorf("%s has no path", e.Entity.Entity.Entity.ID)
				}
			}
			if withChain != len(out.Inheritance) {
				t.Errorf("%d entities with a chain, want %d", withChain, len(out.Inheritance))
			}
		})
	}
}
