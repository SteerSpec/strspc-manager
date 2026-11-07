package entityops

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// standardRealmFile is an entity file of a Standard Realm whose rules and
// standard reference use the fields core 1.21.0 adds.
const standardRealmFile = `{"$schema":"./_schema/entity.v1.schema.json",
	"entity":{"id":"VTWO","title":"Authentication","standards":[
		{"id":"owasp.asvs","relation":"implements","version":"5.0.0","realm":"org.owasp.asvs","profile":"L2"}]},
	"rule_set":{"version":"0.1.0","timestamp":"2026-11-04T00:00:00Z","hash":null},
	"rules":[
		{"id":"VTWO-001","revision":0,"state":"D","body":"Draft.","added_by":"@alice","added_at":"2026-11-04","supersedes":null,"clause":"V2.1.1","profiles":["L1","L2"]},
		{"id":"VTWO-002","revision":0,"state":"P","body":"Published.","added_by":"@alice","added_at":"2026-11-04","supersedes":null,"clause":"V2.1.2","profiles":["L3"]}],
	"notes":[]}`

// Each rewrite keeps clause, profiles and the standard reference's realm and
// profile, and hashes the file with them.
func TestRewritesKeepCore121Fields(t *testing.T) {
	ops := map[string]func(*entity.File) error{
		"update body": func(f *entity.File) error { return UpdateRuleBody(f, "VTWO-001", "Edited.") },
		"promote":     func(f *entity.File) error { return PromoteRule(f, "VTWO-001") },
		"add note": func(f *entity.File) error {
			_, err := AddNote(f, "VTWO-002", "rationale", "Why.", "github:alice")
			return err
		},
		"supersede": func(f *entity.File) error {
			_, err := SupersedeRule(f, "VTWO-002", "Replacement.", "github:alice")
			return err
		},
		"rewrite added_by": func(f *entity.File) error {
			_, err := RewriteAddedBy(f, map[string]string{"@alice": "github:alice"})
			return err
		},
	}
	for name, op := range ops {
		t.Run(name, func(t *testing.T) {
			f, err := entity.Parse([]byte(standardRealmFile))
			if err != nil {
				t.Fatal(err)
			}
			if err := op(f); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(f)
			if err != nil {
				t.Fatal(err)
			}
			g, err := entity.Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range []struct {
				clause   string
				profiles []string
			}{{"V2.1.1", []string{"L1", "L2"}}, {"V2.1.2", []string{"L3"}}} {
				r := g.Rules[i]
				if r.Clause != want.clause || !slices.Equal(r.Profiles, want.profiles) {
					t.Errorf("%s: clause %q, profiles %v", r.ID, r.Clause, r.Profiles)
				}
			}
			if ref := g.Entity.Standards[0]; ref.Realm != "org.owasp.asvs" || ref.Profile != "L2" {
				t.Errorf("standard realm %q, profile %q", ref.Realm, ref.Profile)
			}
			hash, err := entity.ComputeHash(data)
			if err != nil {
				t.Fatal(err)
			}
			if f.RuleSet.Hash == nil || *f.RuleSet.Hash != hash {
				t.Errorf("rule set hash %v, want %s", f.RuleSet.Hash, hash)
			}
		})
	}
}
