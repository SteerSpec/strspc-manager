package rulelint

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// bodyCodes are the codes of the checks on Draft bodies and notes.
var bodyCodes = []string{"RL021", "RL022", "RL026"}

// countBodyCodes counts the diagnostics of res with a code in bodyCodes.
func countBodyCodes(res *result.Result) map[string]int {
	out := make(map[string]int)
	for _, d := range res.Diagnostics {
		for _, c := range bodyCodes {
			if d.Code == c {
				out[c]++
			}
		}
	}
	return out
}

// verbatimRules is an entity whose Draft rule is worded as a standard words
// it: no key word, and an example in the body and in a rationale note. Its
// second rule has two sentences.
func verbatimRules(id string) *entity.File {
	ef := entityWith(id, []entity.Rule{
		{ID: id + "-001", State: entity.StateDraft, Body: "Verify that tokens, such as session tokens, are random.", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: "V1.1.1"},
		{ID: id + "-002", State: entity.StateDraft, Body: "Verify that it holds. Verify that it stays.", AddedBy: "github:a", AddedAt: "2026-01-01", Clause: "V1.1.2"},
	})
	ef.Notes = []entity.Note{{ID: id + "-001/01", RuleRef: id + "-001", Type: "rationale", Content: "Tokens differ, e.g. by length.", AddedBy: "github:a", AddedAt: "2026-01-01"}}
	return ef
}

// The bodies of a Standard Realm are the standard's wording: they MAY hold no
// key word (STDRLM-020) and keep their examples (STDRLM-021), but are still
// divided at sentence ends (STDRLM-019).
var (
	wantStandard = map[string]int{"RL022": 1}
	wantCode     = map[string]int{"RL021": 2, "RL022": 1, "RL026": 2}
)

func codeMeta(id string) entity.RealmMeta {
	return entity.RealmMeta{ID: id, Title: "C", Version: "1.0.0"}
}

func writeVerbatimRealm(t *testing.T, dir string, meta entity.RealmMeta, subRealms ...string) {
	t.Helper()
	writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: meta, SubRealms: subRealms})
	writeJSON(t, filepath.Join(dir, "STD.json"), verbatimRules("STD"))
}

func TestStandardBodies_LintRealm(t *testing.T) {
	for name, tt := range map[string]struct {
		meta entity.RealmMeta
		want map[string]int
	}{
		"standard": {stdMeta("dev.test.std"), wantStandard},
		"code":     {codeMeta("dev.test.code"), wantCode},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			writeVerbatimRealm(t, dir, tt.meta)
			if got := countBodyCodes(New().LintRealm(t.Context(), dir)); !maps.Equal(got, tt.want) {
				t.Errorf("LintRealm got %v, want %v", got, tt.want)
			}
			if got := countBodyCodes(New().LintDir(t.Context(), dir)); !maps.Equal(got, tt.want) {
				t.Errorf("LintDir got %v, want %v", got, tt.want)
			}
		})
	}
}

// Under strict, a Standard Realm still raises no RL021 or RL026.
func TestStandardBodies_Strict(t *testing.T) {
	dir := t.TempDir()
	writeVerbatimRealm(t, dir, stdMeta("dev.test.std"))
	if got := countBodyCodes(New(WithStrict(true)).LintRealm(t.Context(), dir)); !maps.Equal(got, wantStandard) {
		t.Errorf("got %v, want %v", got, wantStandard)
	}
}

// Each Realm of the tree is judged by its own manifest, whichever linter lints
// its files.
func TestStandardBodies_SubRealms(t *testing.T) {
	t.Run("code sub-realm of a Standard Realm", func(t *testing.T) {
		dir := t.TempDir()
		writeVerbatimRealm(t, dir, stdMeta("dev.test.std"), "team")
		writeJSON(t, filepath.Join(dir, "team", "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: codeMeta("dev.test.std.team")})
		writeJSON(t, filepath.Join(dir, "team", "TEAM.json"), verbatimRules("TEAM"))
		want := map[string]int{"RL021": 2, "RL022": 2, "RL026": 2}
		if got := countBodyCodes(New().LintRealm(t.Context(), dir)); !maps.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("standard sub-realm of a code Realm, with its own linter", func(t *testing.T) {
		dir := t.TempDir()
		writeVerbatimRealm(t, dir, codeMeta("dev.test.code"), "std")
		writeJSON(t, filepath.Join(dir, "std", "realm.json"), &entity.RealmFile{
			Schema: "./_schema/realm.v1.schema.json", Realm: stdMeta("dev.test.code.std"),
			Dependencies: []entity.RealmDep{{RealmID: "dev.steerspec.core", Version: "1.23.0", Source: "github://SteerSpec/strspc-rules@v1.23.0/rules/core"}},
		})
		writeJSON(t, filepath.Join(dir, "std", "SUB.json"), verbatimRules("SUB"))
		l := New(WithSubRealmLinters(func(string) *Linter { return New() }))
		want := map[string]int{"RL021": 2, "RL022": 2, "RL026": 2}
		if got := countBodyCodes(l.LintRealm(t.Context(), dir)); !maps.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("unreadable sub-realm manifest", func(t *testing.T) {
		dir := t.TempDir()
		writeVerbatimRealm(t, dir, stdMeta("dev.test.std"), "team")
		writeJSON(t, filepath.Join(dir, "team", "TEAM.json"), verbatimRules("TEAM"))
		want := map[string]int{"RL021": 2, "RL022": 2, "RL026": 2}
		if got := countBodyCodes(New().LintRealm(t.Context(), dir)); !maps.Equal(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
}

// Without a manifest to read, the linter uses the kind it was given.
func TestStandardBodies_WithRealmKind(t *testing.T) {
	ef := verbatimRules("STD")
	data, err := json.Marshal(ef)
	if err != nil {
		t.Fatal(err)
	}
	noManifest := t.TempDir()
	writeJSON(t, filepath.Join(noManifest, "STD.json"), ef)
	for name, tt := range map[string]struct {
		opts []Option
		want map[string]int
	}{
		"standard": {[]Option{WithRealmKind(entity.KindStandard)}, wantStandard},
		"code":     {nil, wantCode},
	} {
		t.Run(name, func(t *testing.T) {
			l := New(tt.opts...)
			if got := countBodyCodes(l.LintFile(ef)); !maps.Equal(got, tt.want) {
				t.Errorf("LintFile got %v, want %v", got, tt.want)
			}
			if got := countBodyCodes(l.LintBytes(t.Context(), data)); !maps.Equal(got, tt.want) {
				t.Errorf("LintBytes got %v, want %v", got, tt.want)
			}
			if got := countBodyCodes(l.LintFiles(t.Context(), []FileInput{{Data: data, File: ef}})[0]); !maps.Equal(got, tt.want) {
				t.Errorf("LintFiles got %v, want %v", got, tt.want)
			}
			if got := countBodyCodes(l.LintDir(t.Context(), noManifest)); !maps.Equal(got, tt.want) {
				t.Errorf("LintDir got %v, want %v", got, tt.want)
			}
			if got := countBodyCodes(l.LintRealm(t.Context(), noManifest)); !maps.Equal(got, tt.want) {
				t.Errorf("LintRealm got %v, want %v", got, tt.want)
			}
		})
	}
}

// A manifest's kind takes precedence over the configured one.
func TestStandardBodies_ManifestOverridesKind(t *testing.T) {
	dir := t.TempDir()
	writeVerbatimRealm(t, dir, codeMeta("dev.test.code"))
	l := New(WithRealmKind(entity.KindStandard))
	if got := countBodyCodes(l.LintRealm(t.Context(), dir)); !maps.Equal(got, wantCode) {
		t.Errorf("got %v, want %v", got, wantCode)
	}
}

func TestStandardBodies_LintRealmFiles(t *testing.T) {
	ef := verbatimRules("STD")
	data, err := json.Marshal(ef)
	if err != nil {
		t.Fatal(err)
	}
	in := []FileInput{{Data: data, File: ef}}
	if got := countBodyCodes(New().LintRealmFiles(t.Context(), entity.KindStandard, in)[0]); !maps.Equal(got, wantStandard) {
		t.Errorf("standard got %v, want %v", got, wantStandard)
	}
	l := New(WithRealmKind(entity.KindStandard))
	if got := countBodyCodes(l.LintRealmFiles(t.Context(), "", in)[0]); !maps.Equal(got, wantCode) {
		t.Errorf("code got %v, want %v", got, wantCode)
	}
}
