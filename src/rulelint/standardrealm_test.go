package rulelint

import (
	"maps"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// standardRealmCodes are the codes CheckStandardRealm emits.
var standardRealmCodes = []string{"RL033", "RL034", "RL035", "RL036", "RL037", "RL038", "RL039", "RL040", "RL041"}

// standardCodes counts the diagnostics of res with a code CheckStandardRealm
// emits, leaving out the others.
func standardCodes(res *result.Result) map[string]int {
	all := codes(res)
	out := make(map[string]int)
	for _, c := range standardRealmCodes {
		if all[c] > 0 {
			out[c] = all[c]
		}
	}
	return out
}

func strp(s string) *string { return &s }

// stdRule is a rule of the STD entity.
func stdRule(id, state, clause string, profiles []string, supersedes *string) entity.Rule {
	return entity.Rule{
		ID: id, State: state, Body: "The system MUST work.", AddedBy: "github:a", AddedAt: "2026-01-01",
		Supersedes: supersedes, Clause: clause, Profiles: profiles,
	}
}

// stdRules is a Standard Realm entity that breaks none of STDRLM: two live
// rules sharing a clause, a fix that is Published with its target Terminated,
// a live Draft, a Draft fix of a live rule and an Abandoned fix.
func stdRules() []entity.Rule {
	return []entity.Rule{
		stdRule("STD-001", entity.StatePublished, "V1.1.1", []string{"L1"}, nil),
		stdRule("STD-002", entity.StatePublished, "V1.1.1", []string{"L1"}, nil),
		stdRule("STD-003", entity.StateTerminated, "V1.2.1", []string{"L2"}, nil),
		stdRule("STD-004", entity.StatePublished, "V1.2.1", []string{"L2"}, strp("STD-003")),
		stdRule("STD-005", entity.StateDraft, "V1.3.1", nil, nil),
		stdRule("STD-006", entity.StateDraft, "V1.1.1", []string{"L1"}, strp("STD-001")),
		stdRule("STD-007", entity.StateAbandoned, "X", nil, strp("STD-005")),
	}
}

func stdMeta(id string) entity.RealmMeta {
	return entity.RealmMeta{
		ID: id, Title: "S", Version: "1.0.0",
		Kind: entity.KindStandard, Edition: "5.0.0", License: "CC-BY-SA-4.0",
		Attribution: &entity.Attribution{Text: "OWASP", URL: "https://owasp.org"},
		Profiles:    []string{"L1", "L2"},
	}
}

func entityWith(id string, rules []entity.Rule) *entity.File {
	return &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: id, Title: id},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules:   rules,
		Notes:   []entity.Note{},
	}
}

// writeStdRealm writes a Realm with meta and one entity STD holding rules, and
// returns its directory.
func writeStdRealm(t *testing.T, meta entity.RealmMeta, rules []entity.Rule) string {
	t.Helper()
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: meta})
	writeJSON(t, filepath.Join(dir, "STD.json"), entityWith("STD", rules))
	return dir
}

func lintStd(t *testing.T, dir string) map[string]int {
	t.Helper()
	return standardCodes(New().LintRealm(t.Context(), dir))
}

func TestStandardRealm_Clean(t *testing.T) {
	dir := writeStdRealm(t, stdMeta("dev.test.std"), stdRules())
	if got := lintStd(t, dir); len(got) != 0 {
		t.Errorf("a Standard Realm that keeps STDRLM got %v", got)
	}
}

// Each change breaks one rule of STDRLM and fires its code once.
func TestStandardRealm_EachCode(t *testing.T) {
	tests := []struct {
		name   string
		change func(rules []entity.Rule) []entity.Rule
		want   string
	}{
		{"live rule without clause", func(r []entity.Rule) []entity.Rule {
			r[4].Clause = ""
			return r
		}, "RL033"},
		{"undeclared profile", func(r []entity.Rule) []entity.Rule {
			r[4].Profiles = []string{"L3"}
			return r
		}, "RL035"},
		{"profiles differ on a clause", func(r []entity.Rule) []entity.Rule {
			r[1].Profiles = []string{"L1", "L2"}
			return r
		}, "RL036"},
		{"implemented", func(r []entity.Rule) []entity.Rule {
			r[0].State = entity.StateImplemented
			return r
		}, "RL037"},
		{"fix changes the clause", func(r []entity.Rule) []entity.Rule {
			r[5].Clause = "V9.9.9"
			return r
		}, "RL038"},
		{"two fixes of one rule", func(r []entity.Rule) []entity.Rule {
			return append(r, stdRule("STD-008", entity.StateDraft, "V1.1.1", []string{"L1"}, strp("STD-001")))
		}, "RL039"},
		{"draft fix of a draft fix", func(r []entity.Rule) []entity.Rule {
			return append(r, stdRule("STD-008", entity.StateDraft, "V1.1.1", []string{"L1"}, strp("STD-006")))
		}, "RL040"},
		{"fix of an abandoned rule", func(r []entity.Rule) []entity.Rule {
			return append(r, stdRule("STD-008", entity.StateDraft, "X", nil, strp("STD-007")))
		}, "RL040"},
		{"published fix, target not terminated", func(r []entity.Rule) []entity.Rule {
			r[2].State = entity.StatePublished
			return r
		}, "RL041"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeStdRealm(t, stdMeta("dev.test.std"), tt.change(stdRules()))
			got := lintStd(t, dir)
			if want := map[string]int{tt.want: 1}; !maps.Equal(got, want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// An Abandoned fix no longer counts: it may carry any clause and target any
// rule.
func TestStandardRealm_AbandonedFixExempt(t *testing.T) {
	rules := append(stdRules(), stdRule("STD-008", entity.StateAbandoned, "V9.9.9", nil, strp("STD-003")))
	dir := writeStdRealm(t, stdMeta("dev.test.std"), rules)
	if got := lintStd(t, dir); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// A fix whose target is in another entity is left to RL025.
func TestStandardRealm_FixTargetElsewhere(t *testing.T) {
	rules := append(stdRules(), stdRule("STD-008", entity.StatePublished, "V1.4.1", nil, strp("OTHER-001")))
	dir := writeStdRealm(t, stdMeta("dev.test.std"), rules)
	if got := lintStd(t, dir); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// RL036 compares the live rules of the whole Realm, across entities and
// sub-entities, and reports the one that differs at its own file.
func TestStandardRealm_ProfilesAcrossEntities(t *testing.T) {
	dir := writeStdRealm(t, stdMeta("dev.test.std"), stdRules())
	other := entityWith("OTHER", []entity.Rule{stdRule("OTHER-001", entity.StatePublished, "V1.3.1", nil, nil)})
	sub := entityWith("SUBENT", []entity.Rule{stdRule("SUBENT-001", entity.StatePublished, "V1.1.1", []string{"L2"}, nil)})
	sub.Entity.Parent = "OTHER"
	other.SubEntities = []entity.File{*sub}
	path := filepath.Join(dir, "OTHER.json")
	writeJSON(t, path, other)

	res := New().LintRealm(t.Context(), dir)
	if got, want := standardCodes(res), map[string]int{"RL036": 1}; !maps.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, d := range res.Diagnostics {
		if d.Code == "RL036" && d.Path != path {
			t.Errorf("RL036 at %s, want %s", d.Path, path)
		}
	}
}

// A rule of a code Realm has neither clause nor profiles (RUL-017).
func TestCodeRealm_ClauseAndProfiles(t *testing.T) {
	rules := []entity.Rule{
		stdRule("STD-001", entity.StatePublished, "V1.1.1", nil, nil),
		stdRule("STD-002", entity.StatePublished, "", []string{"L1"}, nil),
		stdRule("STD-003", entity.StateImplemented, "", nil, nil),
	}
	dir := writeStdRealm(t, entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}, rules)
	if got, want := lintStd(t, dir), map[string]int{"RL034": 2}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Each Realm of the tree is judged by its own manifest: a sub-realm that
// declares no kind is a code Realm under a Standard Realm (RLMMNFST-009), and
// one that declares it is a Standard Realm under a code Realm.
func TestStandardRealm_SubRealmKind(t *testing.T) {
	t.Run("code sub-realm of a Standard Realm", func(t *testing.T) {
		dir := t.TempDir()
		parent := stdMeta("dev.test.std")
		writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: parent, SubRealms: []string{"team"}})
		writeJSON(t, filepath.Join(dir, "STD.json"), entityWith("STD", stdRules()))
		writeJSON(t, filepath.Join(dir, "team", "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.std.team", Title: "T", Version: "1.0.0"}})
		writeJSON(t, filepath.Join(dir, "team", "TEAM.json"), entityWith("TEAM", []entity.Rule{stdRule("TEAM-001", entity.StateImplemented, "V1.1.1", nil, nil)}))
		if got, want := lintStd(t, dir), map[string]int{"RL034": 1}; !maps.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("standard sub-realm of a code Realm", func(t *testing.T) {
		dir := t.TempDir()
		writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}, SubRealms: []string{"std"}})
		writeJSON(t, filepath.Join(dir, "CODE.json"), entityWith("CODE", []entity.Rule{stdRule("CODE-001", entity.StateImplemented, "", nil, nil)}))
		writeJSON(t, filepath.Join(dir, "std", "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: stdMeta("dev.test.code.std")})
		writeJSON(t, filepath.Join(dir, "std", "STD.json"), entityWith("STD", []entity.Rule{stdRule("STD-001", entity.StatePublished, "", nil, nil)}))
		if got, want := lintStd(t, dir), map[string]int{"RL033": 1}; !maps.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("unreadable sub-realm manifest", func(t *testing.T) {
		dir := t.TempDir()
		writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: stdMeta("dev.test.std"), SubRealms: []string{"gone"}})
		writeJSON(t, filepath.Join(dir, "STD.json"), entityWith("STD", stdRules()))
		if got := lintStd(t, dir); len(got) != 0 {
			t.Errorf("got %v, want none: realm-lint reports the missing manifest (RM009)", got)
		}
	})
}

// A sub_realms entry that is not a directory directly inside the Realm is
// never read, so files outside the Realm are not linted (RM008 reports it).
func TestStandardRealm_SubRealmOutsideIgnored(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "realm")
	writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}, SubRealms: []string{"../outside", "a/b", ".", ""}})
	writeJSON(t, filepath.Join(root, "outside", "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.outside", Title: "O", Version: "1.0.0"}})
	writeJSON(t, filepath.Join(root, "outside", "OUT.json"), entityWith("OUT", []entity.Rule{stdRule("OUT-001", entity.StatePublished, "V1.1.1", nil, nil)}))
	res := &result.Result{}
	CheckStandardRealm(dir, &entity.RealmFile{Realm: entity.RealmMeta{ID: "dev.test.code"}, SubRealms: []string{"../outside", "a/b", ".", ""}}, res)
	if got := standardCodes(res); len(got) != 0 {
		t.Errorf("got %v, want none: the entries name no sub-realm directory", got)
	}
}

// Two profile sets are told apart whatever characters their names hold, and
// the same set matches in any order.
func TestProfileKey(t *testing.T) {
	if profileKey([]string{"a\x00b"}) == profileKey([]string{"a", "b"}) {
		t.Error(`["a\x00b"] and ["a", "b"] share a key`)
	}
	if profileKey([]string{"a,b"}) == profileKey([]string{"a", "b"}) {
		t.Error(`["a,b"] and ["a", "b"] share a key`)
	}
	if profileKey([]string{"L2", "L1"}) != profileKey([]string{"L1", "L2"}) {
		t.Error("the order of the names changed the key")
	}
}

// Without a manifest there is no Realm, so neither kind can be judged.
func TestStandardRealm_NoManifest(t *testing.T) {
	dir := t.TempDir()
	writeJSON(t, filepath.Join(dir, "STD.json"), entityWith("STD", []entity.Rule{stdRule("STD-001", entity.StateImplemented, "V1.1.1", []string{"L9"}, nil)}))
	if got := lintStd(t, dir); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

// No Realm of the org uses the core 1.21.0 fields yet, so none of the new
// codes may fire on this repo's own rules.
func TestStandardRealm_OwnRulesUnaffected(t *testing.T) {
	if got := standardCodes(New().LintRealm(t.Context(), filepath.Join("..", "..", "rules"))); len(got) != 0 {
		t.Errorf("rules/ got %v, want none", got)
	}
}
