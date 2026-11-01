package realmresolve

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestStandardScopes(t *testing.T) {
	own := []entity.StandardRef{{ID: "own.ref", Relation: entity.RelationImplements}}
	outer := &entity.File{Entity: entity.Entity{ID: "OUTER", Standards: []entity.StandardRef{{ID: "outer.ref", Relation: entity.RelationImplements}}}}
	mid := &entity.File{Entity: entity.Entity{ID: "MIDDLE", Parent: "OUTER"}}
	e := &entity.File{Entity: entity.Entity{ID: "INNER", Parent: "MIDDLE", Standards: own}}
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

func TestResolve_EntitiesListsEveryEntity(t *testing.T) {
	out, _ := New().Resolve(t.Context(), loadTestRealm(t, "inherit-chain/x"), absTestdata(t, "inherit-chain/x"))
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
}

func TestResolve_EntitiesWithoutInheritance(t *testing.T) {
	out, _ := New().Resolve(t.Context(), loadTestRealm(t, "note-refs-solo"), absTestdata(t, "note-refs-solo"))
	if len(out.Entities) == 0 {
		t.Fatal("an entity with no inherits still has coverage")
	}
}
