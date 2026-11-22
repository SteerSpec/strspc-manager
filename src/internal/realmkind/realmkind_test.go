package realmkind

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// A file is a standard's only when every manifest above it in the root says
// so: anything in doubt is a code Realm's, whose rules are evaluated.
func TestKindOf(t *testing.T) {
	const (
		code     = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0"}}`
		standard = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"}}`
		withSub  = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0"},"sub_realms":["sub"]}`
	)
	tests := []struct {
		name      string
		manifests map[string]string
		file      string
		want      string
	}{
		{"none", nil, "A.json", ""},
		{"standard", map[string]string{".": standard}, "A.json", entity.KindStandard},
		{"declared standard sub-realm", map[string]string{".": withSub, "sub": standard}, "sub/A.json", entity.KindStandard},
		{"declared sub-realm without a manifest", map[string]string{".": withSub}, "sub/A.json", ""},
		{"code under standard", map[string]string{".": standard, "a": code}, "a/b/A.json", ""},
		{"unreadable under standard", map[string]string{".": standard, "a": "{"}, "a/A.json", ""},
		{"standard under code", map[string]string{".": code, "a": standard}, "a/A.json", ""},
		{"standard below the root", map[string]string{"a": standard}, "a/A.json", entity.KindStandard},
		{"outside the root", map[string]string{".": standard}, "../A.json", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parent := t.TempDir()
			t.Chdir(parent)
			for dir, content := range tc.manifests {
				if err := os.MkdirAll(filepath.Join("root", dir), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join("root", dir, "realm.json"), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			// An absolute root and a relative file, as realm-resolve has them.
			f := New(filepath.Join(parent, "root"))
			if got := f.KindOf(filepath.Join("root", filepath.FromSlash(tc.file))); got != tc.want {
				t.Errorf("KindOf = %q, want %q", got, tc.want)
			}
		})
	}
}
