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
		subMeta  = `{"realm":{"id":"dev.x.sub","title":"X","version":"1.0.0","kind":"standard"}}`
		stdMeta  = `{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"},"sub_realms":`
	)
	tests := []struct {
		name      string
		manifests map[string]string
		file      string
		want      string
	}{
		{"none", nil, "A.json", ""},
		{"standard", map[string]string{".": standard}, "A.json", entity.KindStandard},
		{"declared standard sub-realm", map[string]string{".": withSub, "sub": subMeta}, "sub/A.json", entity.KindStandard},
		{"declared sub-realm without a manifest", map[string]string{".": withSub}, "sub/A.json", ""},
		{"code under standard", map[string]string{".": standard, "a": code}, "a/b/A.json", ""},
		{"unreadable under standard", map[string]string{".": standard, "a": "{"}, "a/A.json", ""},
		{"standard under code", map[string]string{".": code, "a": standard}, "a/A.json", ""},
		{"standard below the root", map[string]string{"a": standard}, "a/A.json", entity.KindStandard},
		{"undeclared standard under standard", map[string]string{".": standard, "a": standard}, "a/A.json", ""},
		{"sub-realm declaring one without a manifest", map[string]string{".": withSub, "sub": stdMeta + `["nested"]}`}, "sub/nested/A.json", ""},
		{"sub-realm declaring sub-realms", map[string]string{".": withSub, "sub": stdMeta + `["nested"]}`}, "sub/A.json", ""},
		{"declared path, not a name", map[string]string{".": stdMeta + `["a/b"]}`}, "a/b/A.json", ""},
		{"declared current directory", map[string]string{".": stdMeta + `["."]}`}, "A.json", ""},
		{"declared in another case", map[string]string{".": stdMeta + `["Sub"]}`}, "sub/A.json", ""},
		{"root of a standard with sub-realms", map[string]string{".": stdMeta + `["sub"]}`}, "a/A.json", entity.KindStandard},
		{"sub-realm with another Realm's ID", map[string]string{".": withSub, "sub": standard}, "sub/A.json", ""},
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

// A manifest that is there and can't be read is in doubt, also a dangling link.
func TestKindOfDanglingManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "realm.json"),
		[]byte(`{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "gone.json"), filepath.Join(root, "a", "realm.json")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if got := New(root).KindOf(filepath.Join(root, "a", "A.json")); got != "" {
		t.Errorf("KindOf = %q, want a code Realm", got)
	}
}

// A linked manifest is in doubt too: a review of the link doesn't show the kind.
func TestKindOfLinkedManifest(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "standard.json")
	if err := os.WriteFile(target, []byte(`{"realm":{"id":"dev.x","title":"X","version":"1.0.0","kind":"standard"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "realm.json")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if got := New(root).KindOf(filepath.Join(root, "A.json")); got != "" {
		t.Errorf("KindOf = %q, want a code Realm", got)
	}
}
