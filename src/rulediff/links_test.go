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

// A link is never read. In head one with a JSON name is refused; in base one,
// whether the change keeps, deletes, replaces or renames it, fails closed,
// since what it held can't be compared. Other links are left alone.
func TestLinksFailClosed(t *testing.T) {
	const acc = `{"entity":{"id":"ACC"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},"rules":[{"id":"ACC-001","revision":0,"state":"I","body":"MUST hold","added_by":"github:a","added_at":"2026-01-01"}]}`
	outside := tree{"OUT.json": acc}.write(t)

	head := t.TempDir()
	link(t, head, "APP.json", filepath.Join(outside, "OUT.json"))
	assertHasCode(t, CompareDir(t.TempDir(), head), "RD000")

	for _, target := range []string{filepath.Join(outside, "OUT.json"), "ACC.json"} {
		base := tree{"ACC.json": acc}.write(t)
		link(t, base, "LINK.json", target)
		for name, h := range map[string]tree{
			"deleted":  {"ACC.json": acc},
			"replaced": {"ACC.json": acc, "LINK.json": acc},
			"renamed":  {"ACC.json": acc, "LINK2.json": `{"entity":{"id":"LNK"},"rules":[]}`},
		} {
			if res := CompareDir(base, h.write(t)); !hasDiag(res, "RD000") {
				t.Errorf("base link to %s %s: %v", target, name, res.Diagnostics)
			}
		}
	}

	base := tree{"realm.json": codeRealm, "README.md": "x"}.write(t)
	link(t, base, "NOTES", "README.md")
	if res := CompareDir(base, tree{"realm.json": codeRealm, "README.md": "x"}.write(t)); !res.OK() {
		t.Errorf("a non-JSON base link: %v", res.Diagnostics)
	}
}

// A directory link in base is never followed, also to judge the kind of a
// file the change adds where it was.
func TestCompareRealms_BaseDirectoryLinkNotFollowed(t *testing.T) {
	outside := tree{"realm.json": stdRealm}.write(t)
	base := tree{"realm.json": codeRealm}.write(t)
	link(t, base, "sub", outside)
	head := tree{"realm.json": codeRealm, "sub/x.json": someEntity}.write(t)
	if res := CompareRealms(base, head); hasDiag(res, "RD028") {
		t.Errorf("kind read through a base directory link: %v", res.Diagnostics)
	}
}

// A JSON file name in another case is RD000 in rule-diff too.
func TestCompareRealms_JSONNameInAnotherCase(t *testing.T) {
	assertHasCode(t, compareTrees(t, tree{"realm.json": codeRealm}, tree{"realm.json": codeRealm, "NEW.JSON": someEntity}), "RD000")
}
