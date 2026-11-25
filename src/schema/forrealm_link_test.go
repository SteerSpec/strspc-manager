package schema

import (
	"os"
	"path/filepath"
	"testing"
)

// ForRealm reads a regular realm.json only: a link, which could lead to a
// FIFO or an endless device, is never read.
func TestForRealmIgnoresALinkedManifest(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "realm.json")
	manifest := `{"realm":{"id":"x"},"dependencies":[{"realm_id":"dev.steerspec.core","version":"1.0.0","source":"github://SteerSpec/strspc-rules@v1.0.0/rules/core"}]}`
	if err := os.WriteFile(target, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if len(ForRealm(dir)) == 0 {
		t.Skip("ForRealm gives no options for this manifest; nothing to compare")
	}
	linked := t.TempDir()
	if err := os.Symlink(target, filepath.Join(linked, "realm.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if got := ForRealm(linked); len(got) != 0 {
		t.Errorf("ForRealm through a link = %d options, want none", len(got))
	}
}
