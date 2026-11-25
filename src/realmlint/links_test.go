package realmlint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// copyValidSubRealm copies testdata/valid_sub_realm to a temporary directory.
func copyValidSubRealm(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "realm")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join(testdataPath(t), "valid_sub_realm"))); err != nil {
		t.Fatal(err)
	}
	return dir
}

// moveAndLink moves path elsewhere and leaves a link to it in its place.
func moveAndLink(t *testing.T, path string) {
	t.Helper()
	moved := filepath.Join(t.TempDir(), filepath.Base(path))
	if err := os.Rename(path, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(moved, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestLinksInTheRealmTreeAreRefused(t *testing.T) {
	for _, tc := range []struct{ name, rel, code string }{
		{"linked sub-realm directory", "sync", "RM008"},
		{"linked sub-realm manifest", "sync/realm.json", "RM009"},
		{"linked manifest", "realm.json", "RM001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := copyValidSubRealm(t)
			if res := New().Lint(context.Background(), dir); !res.OK() {
				t.Fatalf("copy is not valid: %v", res.Diagnostics)
			}
			moveAndLink(t, filepath.Join(dir, filepath.FromSlash(tc.rel)))
			assertHasCode(t, New().Lint(context.Background(), dir), tc.code)
		})
	}
}

// F5: a link inside _schema/, or _schema/ itself as a link, is RM003.
func TestSchemaLinksAreRefused(t *testing.T) {
	for _, rel := range []string{"_schema", "_schema/entity.v1.schema.json"} {
		t.Run(rel, func(t *testing.T) {
			dir := copyValidSubRealm(t)
			moveAndLink(t, filepath.Join(dir, filepath.FromSlash(rel)))
			assertHasCode(t, New().Lint(context.Background(), dir), "RM003")
		})
	}
}

// Copilot #378: a file the walker refuses for a "realm" key is RM005 even
// without a rule linter.
func TestWalkerRejectionWithoutRuleLinter(t *testing.T) {
	dir := copyValidSubRealm(t)
	if err := os.WriteFile(filepath.Join(dir, "LOOK.json"), []byte(`{"realm":{"id":"x"},"entity":{"id":"LOOK"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	res := New().Lint(context.Background(), dir)
	found := false
	for _, d := range res.Diagnostics {
		found = found || (d.Code == "RM005" && d.Severity == result.Error)
	}
	if !found {
		t.Errorf("no RM005 error: %v", res.Diagnostics)
	}
}
