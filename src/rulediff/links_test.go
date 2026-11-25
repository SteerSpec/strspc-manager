package rulediff

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

func hasDiag(res *result.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

// link makes a link at rel below dir pointing to target.
func link(t *testing.T, dir, rel, target string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// Finding 1: a working directory reached through a link must not blind the
// comparison, with "." or with "..".
func TestCompareRealms_LinkedWorkingDirectory(t *testing.T) {
	// The review's case: a code child undeclared and deleted, seen only by
	// scanning the files below the root.
	base := tree{"realm.json": stdParent, childManifest: codeChild, entityInApp: someEntity}.write(t)
	real := tree{"realm.json": stdRealm, entityInApp: someEntity}.write(t)
	outer := t.TempDir()
	link(t, outer, "rules", real)
	// A shell leaves the logical path in PWD, which os.Getwd prefers.
	for _, tc := range []struct{ dir, head string }{
		{filepath.Join(outer, "rules"), "."},
		{filepath.Join(outer, "rules", "app"), ".."},
	} {
		t.Chdir(tc.dir)
		t.Setenv("PWD", tc.dir)
		if wd, _ := os.Getwd(); wd != tc.dir {
			t.Fatalf("Getwd = %q, want the logical %q", wd, tc.dir)
		}
		assertHasCode(t, CompareRealms(base, tc.head), "RD028")
	}
}

// Findings 3, 5, 13: a link anywhere in head is RD000, whatever it names.
func TestCompareRealms_LinkInHeadIsRefused(t *testing.T) {
	base := tree{"realm.json": codeParent, childManifest: codeChild, entityInApp: someEntity}
	outside := tree{"realm.json": codeChild, "APP.json": someEntity}.write(t)
	for _, tc := range []struct{ name, rel, target string }{
		{"linked sub-realm directory", "app", outside},
		{"absolute manifest link", "realm.json", filepath.Join(outside, "realm.json")},
		{"linked entity file", "app/APP.json", filepath.Join(outside, "APP.json")},
		{"link to a device", "app/dev.json", os.DevNull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			head := base.write(t)
			path := filepath.Join(head, filepath.FromSlash(tc.rel))
			if tc.rel == "app" {
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			link(t, head, tc.rel, tc.target)
			assertHasCode(t, CompareRealms(base.write(t), head), "RD000")
		})
	}
}

// A base link out of the tree is never followed: replacing it with a file
// fails closed, and deleting it passes.
func TestCompareRealms_BaseLinkOutOfTheTree(t *testing.T) {
	head := tree{"realm.json": codeRealm}
	baseDir := t.TempDir()
	link(t, baseDir, "realm.json", filepath.Join(head.write(t), "realm.json"))
	assertHasCode(t, CompareRealms(baseDir, head.write(t)), "RD000")
	if res := CompareRealms(baseDir, t.TempDir()); hasDiag(res, "RD000") {
		t.Errorf("deleting a base link: %v", res.Diagnostics)
	}
}

// Finding 4: a manifest name that differs from realm.json only by case is
// RD000 on every system, as are two names that collide when case is ignored.
func TestCompareRealms_ManifestNameCase(t *testing.T) {
	base := tree{"realm.json": codeRealm}
	for _, name := range []string{"realm.JSON", "REALM.JSON", "Realm.json", "realm.jſon", "app/Realm.json"} {
		t.Run(name, func(t *testing.T) {
			assertHasCode(t, compareTrees(t, base, tree{name: stdRealm}), "RD000")
		})
	}
	t.Run("colliding names", func(t *testing.T) {
		head := t.TempDir()
		if err := os.WriteFile(filepath.Join(head, "APP.json"), []byte(someEntity), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(head, "app.json"), []byte(someEntity), 0o600); err != nil {
			t.Fatal(err)
		}
		entries, _ := os.ReadDir(head)
		if len(entries) < 2 {
			t.Skip("case-insensitive file system")
		}
		assertHasCode(t, CompareRealms(base.write(t), head), "RD000")
	})
}

// CompareDir refuses a link in head and lets a change remove one from base.
func TestCompareDir_Links(t *testing.T) {
	outside := tree{"OUT.json": someEntity}.write(t)
	head := t.TempDir()
	link(t, head, "APP.json", filepath.Join(outside, "OUT.json"))
	assertHasCode(t, CompareDir(t.TempDir(), head), "RD000")

	base := t.TempDir()
	link(t, base, "APP.json", filepath.Join(outside, "OUT.json"))
	if res := CompareDir(base, t.TempDir()); hasDiag(res, "RD005") || hasDiag(res, "RD000") {
		t.Errorf("removing a base link: %v", res.Diagnostics)
	}
}

// Finding 19: paths are relative to headDir, as CompareRealms documents.
func TestCompareRealms_PathsAreRelativeToHead(t *testing.T) {
	head := tree{"app/Realm.json": stdRealm}.write(t)
	res := CompareRealms(tree{"realm.json": codeRealm}.write(t), head)
	if len(res.Diagnostics) == 0 {
		t.Fatal("no diagnostics")
	}
	for _, d := range res.Diagnostics {
		if filepath.IsAbs(d.Path) {
			t.Errorf("absolute path %q in %s", d.Path, d.Code)
		}
	}
	missing := filepath.Join(t.TempDir(), "missing")
	for _, d := range CompareRealms(head, missing).Diagnostics {
		if d.Path != "." {
			t.Errorf("head root error path = %q, want .", d.Path)
		}
	}
}

// F3: a link in base to a file inside the base tree is read, so the change
// replacing it with a regular file is compared with what it held: a faithful
// repair passes and a weakening doesn't. A link out of the tree is not read.
func TestCompareDir_BaseLinkInsideTheTree(t *testing.T) {
	const (
		held     = `{"entity":{"id":"ACC"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},"rules":[{"id":"ACC-001","revision":0,"state":"I","body":"MUST hold","added_by":"github:a","added_at":"2026-01-01"}]}`
		weakened = `{"entity":{"id":"ACC"},"rule_set":{"version":"1.0.1","timestamp":"2026-01-02T00:00:00Z","hash":null},"rules":[{"id":"ACC-001","revision":0,"state":"D","body":"MUST hold","added_by":"github:a","added_at":"2026-01-01"}]}`
	)
	base := tree{"store/ACC.json": held}.write(t)
	link(t, base, "ACC.json", filepath.Join("store", "ACC.json"))

	repaired := tree{"ACC.json": held, "store/ACC.json": held}.write(t)
	if res := CompareDir(base, repaired); !res.OK() {
		t.Errorf("faithful repair: %v", res.Diagnostics)
	}
	weak := tree{"ACC.json": weakened, "store/ACC.json": held}.write(t)
	if res := CompareDir(base, weak); res.OK() {
		t.Error("weakening through the repair passed")
	}
	// Deleting the link and the file it leads to deletes the rule.
	assertHasCode(t, CompareDir(base, t.TempDir()), "RD005")

	outside := tree{"ACC.json": held}.write(t)
	out := t.TempDir()
	link(t, out, "ACC.json", filepath.Join(outside, "ACC.json"))
	if res := CompareDir(out, t.TempDir()); hasDiag(res, "RD005") || hasDiag(res, "RD000") {
		t.Errorf("removing a link out of the base tree: %v", res.Diagnostics)
	}
}

// F3 for manifests: a base manifest linked inside the tree is compared.
func TestCompareRealms_BaseManifestLinkInsideTheTree(t *testing.T) {
	base := tree{"store/realm.json": codeRealm}.write(t)
	link(t, base, "realm.json", filepath.Join("store", "realm.json"))
	assertHasCode(t, CompareRealms(base, tree{"realm.json": stdRealm, "store/realm.json": codeRealm}.write(t)), "RD028")
}

// F5: a link inside _schema/ in head is RD000 too.
func TestCompareRealms_SchemaLinkInHead(t *testing.T) {
	head := tree{"realm.json": codeRealm, "_schema/realm.v1.schema.json": "{}"}.write(t)
	link(t, head, "_schema/entity.v1.schema.json", os.DevNull)
	assertHasCode(t, CompareRealms(tree{"realm.json": codeRealm}.write(t), head), "RD000")
}

// F8: a non-JSON link to a file is left alone in head too.
func TestCompareRealms_NonJSONFileLink(t *testing.T) {
	head := tree{"realm.json": codeRealm, "README.md": "x"}.write(t)
	link(t, head, "README", "README.md")
	if res := CompareRealms(tree{"realm.json": codeRealm}.write(t), head); !res.OK() {
		t.Errorf("%v", res.Diagnostics)
	}
}

// Re-review F6: deleting a base link whose file is still in head deletes no
// rule; deleting both is RD005.
func TestCompareDir_DeletingADuplicateLink(t *testing.T) {
	const acc = `{"entity":{"id":"ACC"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},"rules":[{"id":"ACC-001","revision":0,"state":"I","body":"MUST hold","added_by":"github:a","added_at":"2026-01-01"}]}`
	base := tree{"ACC.json": acc}.write(t)
	link(t, base, "DUP.json", "ACC.json")
	if res := CompareDir(base, tree{"ACC.json": acc}.write(t)); hasDiag(res, "RD005") {
		t.Errorf("deleting the duplicate link: %v", res.Diagnostics)
	}
	assertHasCode(t, CompareDir(base, t.TempDir()), "RD005")
}

// The RD005 exception for a deleted link covers only a target CompareDir
// compares: one in a subdirectory could be weakened unseen.
func TestCompareDir_DeletedLinkToANestedFile(t *testing.T) {
	const acc = `{"entity":{"id":"ACC"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},"rules":[{"id":"ACC-001","revision":0,"state":"I","body":"MUST hold","added_by":"github:a","added_at":"2026-01-01"}]}`
	base := tree{"store/ACC.json": acc}.write(t)
	link(t, base, "ACC.json", filepath.Join("store", "ACC.json"))
	assertHasCode(t, CompareDir(base, tree{"store/ACC.json": "{}"}.write(t)), "RD005")
}

// A head file replacing a base link the comparison can't read fails closed:
// what the link held can't be compared. Deleting the link still passes.
func TestUnreadableBaseLinkReplacedFailsClosed(t *testing.T) {
	outside := tree{"ACC.json": someEntity, "realm.json": codeRealm}.write(t)
	base := tree{"realm.json": codeRealm}.write(t)
	link(t, base, "ACC.json", filepath.Join(outside, "ACC.json"))
	assertHasCode(t, CompareDir(base, tree{"ACC.json": someEntity, "realm.json": codeRealm}.write(t)), "RD000")
	if res := CompareDir(base, tree{"realm.json": codeRealm}.write(t)); hasDiag(res, "RD000") || hasDiag(res, "RD005") {
		t.Errorf("deleting the link: %v", res.Diagnostics)
	}

	manifestBase := t.TempDir()
	link(t, manifestBase, "realm.json", filepath.Join(outside, "realm.json"))
	assertHasCode(t, CompareRealms(manifestBase, tree{"realm.json": stdRealm}.write(t)), "RD000")
}
