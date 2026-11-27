package rulelint

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// A sub_realms entry that is not a directory directly inside the Realm is
// never walked, so LintRealm reads no entity file outside the Realm (RM008
// reports the entry).
func TestLintRealm_SubRealmOutsideNotWalked(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "realm")
	writeJSON(t, filepath.Join(dir, "realm.json"), &entity.RealmFile{Schema: "./_schema/realm.v1.schema.json", Realm: entity.RealmMeta{ID: "dev.test.code", Title: "C", Version: "1.0.0"}, SubRealms: []string{"../outside", "a/b", ".", ""}})
	if err := os.MkdirAll(filepath.Join(root, "outside"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "outside", "OUT.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, d := range New().LintRealm(context.Background(), dir).Diagnostics {
		if strings.Contains(filepath.ToSlash(d.Path), "outside") {
			t.Errorf("diagnostic for a file outside the Realm: %s %s: %s", d.Code, d.Path, d.Message)
		}
	}
}

// A path whose first element matches a sub_realms entry that is not a
// directory directly inside the Realm, such as "..", is a file of the Realm
// itself: the entry's manifest is never read.
func TestRealmStandardScopes_SubRealmOutsideNotRead(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "realm")
	writeJSON(t, filepath.Join(root, "realm.json"), realmFile("dev.test.outside", rfc2119))
	writeJSON(t, filepath.Join(dir, "realm.json"), realmFile("dev.test.root", asvs, ".."))
	got := RealmStandardScopes(dir, filepath.Join(root, "x", "A.json"))
	if want := [][]entity.StandardRef{asvs, nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}
