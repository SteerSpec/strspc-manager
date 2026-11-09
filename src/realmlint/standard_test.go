package realmlint

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// manifestCodes are the codes checkManifestNotesAndHash emits.
var manifestCodes = []string{"RM021", "RM022", "RM023", "RM024", "RM025"}

// countCodes counts the diagnostics of res with one of the given codes.
func countCodes(res *result.Result, codes ...string) map[string]int {
	out := make(map[string]int)
	for _, d := range res.Diagnostics {
		for _, c := range codes {
			if d.Code == c {
				out[c]++
			}
		}
	}
	return out
}

// linkedASVS is a Standard Reference that links its Standard Realm.
var linkedASVS = entity.StandardRef{ID: "owasp.asvs", Relation: entity.RelationImplements, Version: "5.0.0", Realm: "dev.steerspec.std.owasp.asvs.v5"}

// stdManifest is a Standard Realm manifest that breaks none of the checks:
// every field STDRLM requires, and Realm Notes numbered from REALM/001 whose
// refs are where they may be.
func stdManifest() *entity.RealmFile {
	return &entity.RealmFile{
		Schema: "./_schema/realm.v1.schema.json",
		Realm: entity.RealmMeta{
			ID: "dev.test.std", Title: "S", Version: "1.0.0",
			Standards: []entity.StandardRef{linkedASVS, {ID: "ietf.rfc2119", Relation: entity.RelationDerivedFrom}},
			Kind:      entity.KindStandard, Edition: "5.0.0", License: "CC-BY-SA-4.0",
			Attribution: &entity.Attribution{Text: "OWASP", URL: "https://owasp.org"},
			Profiles:    []string{"L1"},
		},
		Notes: []entity.RealmNote{
			{ID: "REALM/002", Type: entity.NoteTypeReference, Ref: "owasp.asvs:V2.1.1", Content: "Withdrawn.", AddedBy: "github:a", AddedAt: "2026-01-01", Revision: 1},
			{ID: "REALM/001", Type: entity.NoteTypeExclusion, Ref: "owasp.asvs:V1.1.1", Content: "No UI.", AddedBy: "github:a", AddedAt: "2026-01-01"},
			{ID: "REALM/003", Type: "rationale", Content: "Why.", AddedBy: "github:a", AddedAt: "2026-01-01"},
		},
	}
}

// writeManifest writes rf as dir/realm.json, with a hash computed for its
// content when withHash is set, and returns the bytes written.
func writeManifest(t *testing.T, dir string, rf *entity.RealmFile, withHash bool) []byte {
	t.Helper()
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	if withHash {
		h, err := entity.ComputeRealmHash(data)
		if err != nil {
			t.Fatal(err)
		}
		hashed := *rf
		hashed.Hash = h
		if data, err = json.Marshal(&hashed); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return data
}

func lintManifest(t *testing.T, rf *entity.RealmFile, withHash bool) map[string]int {
	t.Helper()
	dir := t.TempDir()
	writeManifest(t, dir, rf, withHash)
	return countCodes(New().Lint(t.Context(), dir), manifestCodes...)
}

func TestManifest_Clean(t *testing.T) {
	for _, withHash := range []bool{false, true} {
		if got := lintManifest(t, stdManifest(), withHash); len(got) != 0 {
			t.Errorf("withHash=%v: got %v, want none", withHash, got)
		}
	}
}

func TestManifest_EachCode(t *testing.T) {
	tests := []struct {
		name   string
		change func(rf *entity.RealmFile)
		want   map[string]int
	}{
		{"no edition, license or attribution", func(rf *entity.RealmFile) {
			rf.Realm.Edition, rf.Realm.License, rf.Realm.Attribution = "", "", nil
		}, map[string]int{"RM021": 3}},
		{"a code Realm needs none of them", func(rf *entity.RealmFile) {
			rf.Realm.Kind, rf.Realm.Edition, rf.Realm.License, rf.Realm.Attribution = "", "", "", nil
		}, map[string]int{}},
		{"repeated note id", func(rf *entity.RealmFile) {
			rf.Notes[2].ID = "REALM/001" // 002, 001, 001: one repeat, no gap.
		}, map[string]int{"RM022": 1}},
		{"skipped note ids", func(rf *entity.RealmFile) {
			rf.Notes[2].ID = "REALM/005" // 003 and 004 are missing.
		}, map[string]int{"RM022": 2}},
		{"REALM/000", func(rf *entity.RealmFile) {
			rf.Notes = append(rf.Notes, entity.RealmNote{ID: "REALM/000", Type: "rationale", Content: "x", AddedBy: "github:a", AddedAt: "2026-01-01"})
		}, map[string]int{"RM022": 1}},
		{"exclusion without ref", func(rf *entity.RealmFile) {
			rf.Notes[1].Ref = ""
		}, map[string]int{"RM023": 1}},
		{"exclusion of an undeclared standard", func(rf *entity.RealmFile) {
			rf.Notes[1].Ref = "iso.27001:A.5.1"
		}, map[string]int{"RM023": 1}},
		{"exclusion of a standard with no realm", func(rf *entity.RealmFile) {
			rf.Notes[1].Ref = "ietf.rfc2119:2"
		}, map[string]int{"RM023": 1}},
		{"exclusion ref without a colon", func(rf *entity.RealmFile) {
			rf.Notes[1].Ref = "owasp.asvs"
		}, map[string]int{"RM023": 1}},
		{"ref on a rationale", func(rf *entity.RealmFile) {
			rf.Notes[2].Ref = "owasp.asvs:V1.1.1"
		}, map[string]int{"RM024": 1}},
		{"ref on a clarification", func(rf *entity.RealmFile) {
			rf.Notes[2].Type, rf.Notes[2].Ref = "clarification", "owasp.asvs:V1.1.1"
		}, map[string]int{"RM024": 1}},
		{"ref on an unknown type is the schema's", func(rf *entity.RealmFile) {
			rf.Notes[2].Type, rf.Notes[2].Ref = "other", "owasp.asvs:V1.1.1"
		}, map[string]int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rf := stdManifest()
			tt.change(rf)
			if got := lintManifest(t, rf, false); !maps.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// A hash that doesn't match the manifest's content is an error, and a missing
// one isn't reported until the CLI recomputes it (RLMMNFST-014).
func TestManifest_Hash(t *testing.T) {
	dir := t.TempDir()
	data := writeManifest(t, dir, stdManifest(), true)
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	m["realm"].(map[string]any)["version"] = "1.0.1" // A bump without a new hash.
	stale, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := countCodes(New().Lint(t.Context(), dir), manifestCodes...), map[string]int{"RM025": 1}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// RM018 reports a profile without a realm (STDREF-019) through
// entity.ValidateStandardRef.
func TestManifest_ProfileWithoutRealm(t *testing.T) {
	rf := stdManifest()
	rf.Realm.Standards[1].Profile = "L1"
	dir := t.TempDir()
	writeManifest(t, dir, rf, false)
	if got := countCodes(New().Lint(t.Context(), dir), "RM018"); got["RM018"] != 1 {
		t.Errorf("RM018 = %d, want 1", got["RM018"])
	}
}

// A sub-realm manifest gets the checks too, judged by its own kind.
func TestManifest_SubRealm(t *testing.T) {
	dir := t.TempDir()
	parent := &entity.RealmFile{
		Schema:    "./_schema/realm.v1.schema.json",
		Realm:     entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"},
		SubRealms: []string{"std"},
	}
	writeManifest(t, dir, parent, true)
	sub := stdManifest()
	sub.Realm.ID = "dev.test.code.std"
	sub.Realm.License = ""
	writeManifest(t, filepath.Join(dir, "std"), sub, false)
	if got, want := countCodes(New().Lint(t.Context(), dir), manifestCodes...), map[string]int{"RM021": 1}; !maps.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// realm-lint runs the rule checks that depend on the Realm's kind when it is
// given a rule linter, and leaves them to LintRealm otherwise, so they are
// reported once either way.
func TestStandardRealmRuleChecks(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}}, false)
	ef := &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "CODE", Title: "C"},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules:   []entity.Rule{{ID: "CODE-001", State: entity.StatePublished, Body: "The system MUST work.", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: "V1.1.1"}},
		Notes:   []entity.Note{},
	}
	data, err := json.Marshal(ef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "CODE.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := countCode(New(WithRuleLinter(rulelint.New())), t, dir, "RL034"); got != 1 {
		t.Errorf("with a rule linter: RL034 = %d, want 1", got)
	}
	if got := countCode(New(), t, dir, "RL034"); got != 0 {
		t.Errorf("without a rule linter: RL034 = %d, want 0", got)
	}
}

// No manifest of the org uses the core 1.21.0 fields yet, so none of the new
// codes may fire on this repo's own Realm.
func TestManifest_OwnRealmUnaffected(t *testing.T) {
	res := New(WithRuleLinter(rulelint.New())).Lint(t.Context(), filepath.Join("..", "..", "rules"))
	codes := append(append(append([]string{}, manifestCodes...), linkedCodes...), "RL033", "RL034", "RL035", "RL036", "RL037", "RL038", "RL039", "RL040", "RL041")
	if got := countCodes(res, codes...); len(got) != 0 {
		t.Errorf("rules/ got %v, want none", got)
	}
}

// realm-lint lints each Realm's entity files as the kind its manifest
// declares: in a Standard Realm a Draft body may hold no key word and keep its
// examples (Draft rules in strspc-rules#186), so RL021 and RL026 don't fire
// there, while RL022 still does.
func TestStandardRealmBodies(t *testing.T) {
	codeRealm := &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}}
	for name, tt := range map[string]struct {
		rf   *entity.RealmFile
		want map[string]int
	}{
		"standard": {stdManifest(), map[string]int{"RL022": 1}},
		"code":     {codeRealm, map[string]int{"RL021": 1, "RL022": 1, "RL026": 2}},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeManifest(t, dir, tt.rf, false)
			ef := &entity.File{
				Schema:  "./_schema/entity.v1.schema.json",
				Entity:  entity.Entity{ID: "STD", Title: "S"},
				RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
				Rules: []entity.Rule{
					{ID: "STD-001", State: entity.StateDraft, Body: "Verify that tokens, such as session tokens, are random.", AddedBy: "github:a", AddedAt: "2026-01-01"},
					{ID: "STD-002", State: entity.StateDraft, Body: "It MUST hold. It MUST stay.", AddedBy: "github:a", AddedAt: "2026-01-01"},
				},
				Notes: []entity.Note{{ID: "STD-001/01", RuleRef: "STD-001", Type: "rationale", Content: "Tokens differ, e.g. by length.", AddedBy: "github:a", AddedAt: "2026-01-01"}},
			}
			data, err := json.Marshal(ef)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "STD.json"), data, 0o644); err != nil {
				t.Fatal(err)
			}
			res := New(WithRuleLinter(rulelint.New())).Lint(t.Context(), dir)
			if got := countCodes(res, "RL021", "RL022", "RL026"); !maps.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
