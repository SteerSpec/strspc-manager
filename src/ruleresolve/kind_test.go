package ruleresolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// writeSource writes files, by slash-separated path, under a new directory and
// returns it.
func writeSource(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Each file has the kind of its Realm, read per manifest: a Realm is a
// manifest and its declared sub-realms, as rule-lint reads it.
func TestResolve_RealmKind(t *testing.T) {
	const (
		code        = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0"}}`
		standard    = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"}}`
		codeWithSub = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0"},"sub_realms":["sub"]}`
		stdWithSub  = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"},"sub_realms":["sub"]}`
		subCode     = `{"realm":{"id":"dev.x.sub","title":"Sub","version":"1.0.0"}}`
		subStandard = `{"realm":{"id":"dev.x.sub","title":"Sub","version":"1.0.0","kind":"standard"}}`
		ent         = `{"entity":{"id":"AAA","title":"A"},"rule_set":{"version":"0.1.0","hash":null},"rules":[],"notes":[]}`
	)
	tests := []struct {
		name  string
		files map[string]string
		file  string
		want  string
	}{
		{"no manifest", nil, "A.json", ""},
		{"code Realm", map[string]string{"realm.json": code}, "A.json", ""},
		{"Standard Realm", map[string]string{"realm.json": standard}, "A.json", entity.KindStandard},
		{"folder of a Standard Realm", map[string]string{"realm.json": standard}, "V1/A.json", entity.KindStandard},
		{"code sub-realm of a Standard Realm", map[string]string{"realm.json": stdWithSub, "sub/realm.json": subCode}, "sub/A.json", ""},
		{"standard sub-realm of a code Realm", map[string]string{"realm.json": codeWithSub, "sub/realm.json": subStandard}, "sub/A.json", entity.KindStandard},
		{"folder of a sub-realm", map[string]string{"realm.json": codeWithSub, "sub/realm.json": subStandard}, "sub/V1/A.json", entity.KindStandard},
		{"root of a Realm with a sub-realm", map[string]string{"realm.json": stdWithSub, "sub/realm.json": subCode}, "A.json", entity.KindStandard},
		{"sub-realm name deeper in the tree", map[string]string{"realm.json": stdWithSub, "sub/realm.json": subCode}, "V1/sub/A.json", entity.KindStandard},
		{"undeclared nested manifest is not a Realm", map[string]string{"realm.json": code, "sub/realm.json": subStandard}, "sub/A.json", ""},
		{"sub-realm without a manifest", map[string]string{"realm.json": stdWithSub}, "sub/A.json", ""},
		{"unreadable manifest", map[string]string{"realm.json": "{"}, "A.json", ""},
		{"unreadable manifest over a standard one", map[string]string{"realm.json": "{", "sub/realm.json": subStandard}, "sub/A.json", ""},
		{"unreadable sub-realm manifest", map[string]string{"realm.json": stdWithSub, "sub/realm.json": "{"}, "sub/A.json", ""},
		{"undeclared code manifest under a standard one", map[string]string{"realm.json": standard, "sub/realm.json": subCode}, "sub/A.json", ""},
		{"code manifest deep under a standard one", map[string]string{"realm.json": standard, "a/b/realm.json": code}, "a/b/c/A.json", ""},
		{"unreadable manifest under a standard one", map[string]string{"realm.json": standard, "sub/realm.json": "{"}, "sub/A.json", ""},
		{"code manifest under a standard sub-realm", map[string]string{"realm.json": codeWithSub, "sub/realm.json": subStandard, "sub/a/realm.json": code}, "sub/a/A.json", ""},
		{"standard manifest under a standard one", map[string]string{"realm.json": standard, "sub/realm.json": subStandard}, "sub/A.json", entity.KindStandard},
		{"source holding a Standard Realm", map[string]string{"asvs/realm.json": standard}, "asvs/A.json", entity.KindStandard},
		{"beside a Standard Realm", map[string]string{"asvs/realm.json": standard}, "local/A.json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{tc.file: ent}
			for name, content := range tc.files {
				files[name] = content
			}
			r, err := New([]SourceEntry{{Source: ".", Scope: ScopeLocal}}, WithBaseDir(writeSource(t, files)))
			if err != nil {
				t.Fatal(err)
			}
			got, res := r.Resolve(t.Context())
			if len(got) != 1 {
				t.Fatalf("resolved %d files, want 1: %v", len(got), res.Diagnostics)
			}
			if got[0].RealmKind != tc.want {
				t.Errorf("RealmKind = %q, want %q", got[0].RealmKind, tc.want)
			}
		})
	}
}
