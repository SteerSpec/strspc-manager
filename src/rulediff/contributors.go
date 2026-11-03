package rulediff

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// WithContributors gives the contributors the Realm declares (CTRB-004), as
// entity.RealmContributors returns them, so the Differ runs RD023. An empty
// list means the Realm declares none, and RD023 does not run.
func WithContributors(list []entity.Contributor) Option {
	return func(c *Config) { c.Contributors = list }
}

// checkContributors runs RD023 for the rules and notes of head, and of its
// sub-entities, that base does not hold: the added_by of each must name one
// of the Realm's contributors (CTRB-005). base is nil for a new entity. An
// added_by that RD011 or RD019 reports is left to them. RD023 is Info while
// the contributor format is in transition (#248), so strict mode keeps it.
func checkContributors(base, head *entity.File, list []entity.Contributor, res *result.Result, path string) {
	if len(list) == 0 {
		return
	}
	oldRules := map[string]bool{}
	oldNotes := map[string]bool{}
	oldSubs := map[string]*entity.File{}
	if base != nil {
		for _, r := range base.Rules {
			oldRules[r.ID] = true
		}
		for _, n := range base.Notes {
			oldNotes[n.ID] = true
		}
		for i := range base.SubEntities {
			oldSubs[base.SubEntities[i].Entity.ID] = &base.SubEntities[i]
		}
	}
	for _, r := range head.Rules {
		if !oldRules[r.ID] {
			checkContributor("rule", r.AddedBy, list, res, path+"/"+r.ID)
		}
	}
	for _, n := range head.Notes {
		if !oldNotes[n.ID] {
			checkContributor("note", n.AddedBy, list, res, path+"/notes/"+n.ID)
		}
	}
	for i := range head.SubEntities {
		sub := &head.SubEntities[i]
		checkContributors(oldSubs[sub.Entity.ID], sub, list, res, path+" > "+sub.Entity.ID)
	}
}

func checkContributor(kind, addedBy string, list []entity.Contributor, res *result.Result, path string) {
	if !entity.ValidAddedBy(addedBy) || entity.FindContributor(list, addedBy) != nil {
		return
	}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RD023",
		Severity: result.Info,
		Message:  fmt.Sprintf("new %s has added_by %q, which names none of the Realm's contributors (CTRB-005)", kind, addedBy),
		Path:     path,
	})
}
