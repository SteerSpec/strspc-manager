package realmfs

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// tree makes dir/real/a.json, dir/real/sub/, a link dir/link -> real and a
// file link dir/real/l.json -> a.json, and returns the physical dir.
func tree(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "link")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if err := os.Symlink("a.json", filepath.Join(real, "l.json")); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRoot(t *testing.T) {
	dir := tree(t)
	real := filepath.Join(dir, "real")
	for _, tc := range []struct{ in, want string }{
		{real, real},
		{filepath.Join(dir, "link"), real},               // a link naming the root is resolved
		{filepath.Join(dir, "link", "..", "real"), real}, // resolved before any lexical clean
	} {
		got, err := Root(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("Root(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
	t.Chdir(filepath.Join(dir, "link"))
	if got, err := Root("."); err != nil || got != real {
		t.Errorf(`Root(".") through a linked working directory = %q, %v; want %q`, got, err, real)
	}
	for _, bad := range []string{"", filepath.Join(real, "a.json"), filepath.Join(dir, "missing")} {
		if _, err := Root(bad); err == nil {
			t.Errorf("Root(%q): no error", bad)
		}
	}
}

// A root that resolves to a network share or device path is refused.
func TestRootRefusesANonLocalTarget(t *testing.T) {
	dir := tree(t)
	real := filepath.Join(dir, "real")
	orig := isLocal
	t.Cleanup(func() { isLocal = orig })
	isLocal = func(p string) bool { return p != real }
	if _, err := Root(filepath.Join(dir, "link")); err == nil {
		t.Error("Root through a link to a non-local path: no error")
	}
}

// Where links in a root's path aren't followed (Windows), one is refused
// before anything below it is looked at, and a plain path still resolves.
func TestRootWithoutFollowingLinks(t *testing.T) {
	dir := tree(t)
	orig := followRootLinks
	t.Cleanup(func() { followRootLinks = orig })
	followRootLinks = false
	for _, p := range []string{filepath.Join(dir, "link"), filepath.Join(dir, "link", "sub")} {
		if _, err := Root(p); err == nil || !strings.Contains(err.Error(), "is a link") {
			t.Errorf("Root(%q) = %v, want a link error", p, err)
		}
	}
	real := filepath.Join(dir, "real")
	if got, err := Root(filepath.Join(real, "sub", "..")); err != nil || got != real {
		t.Errorf("Root(real/sub/..) = %q, %v", got, err)
	}
	t.Chdir(real)
	if got, err := Root("."); err != nil || got != real {
		t.Errorf(`Root(".") = %q, %v`, got, err)
	}
	if err := CheckLocal(filepath.Join(dir, "missing", "x")); err != nil {
		t.Errorf("CheckLocal of a missing path: %v", err)
	}
	orig2 := isLocal
	t.Cleanup(func() { isLocal = orig2 })
	isLocal = func(string) bool { return false }
	if err := CheckLocal(real); err == nil {
		t.Error("CheckLocal of a non-local path: no error")
	}
}

func TestReadFile(t *testing.T) {
	dir := tree(t)
	if data, err := ReadFile(filepath.Join(dir, "real", "a.json")); err != nil || string(data) != "{}" {
		t.Errorf("ReadFile regular = %q, %v", data, err)
	}
	if _, err := ReadFile(filepath.Join(dir, "real", "l.json")); !errors.Is(err, ErrNotRegular) {
		t.Errorf("ReadFile link: err = %v, want ErrNotRegular", err)
	}
	if _, err := ReadFile(filepath.Join(dir, "real", "sub")); !errors.Is(err, ErrNotRegular) {
		t.Errorf("ReadFile dir: err = %v, want ErrNotRegular", err)
	}
	if _, err := ReadFile(filepath.Join(dir, "real", "none.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadFile missing: err = %v, want ErrNotExist", err)
	}
}

func TestDir(t *testing.T) {
	dir := tree(t)
	if err := Dir(filepath.Join(dir, "real", "sub")); err != nil {
		t.Errorf("Dir real: %v", err)
	}
	if err := Dir(filepath.Join(dir, "link")); !errors.Is(err, ErrNotRegular) {
		t.Errorf("Dir link: err = %v, want ErrNotRegular", err)
	}
	if err := Dir(filepath.Join(dir, "real", "a.json")); !errors.Is(err, ErrNotRegular) {
		t.Errorf("Dir file: err = %v, want ErrNotRegular", err)
	}
	if err := Dir(filepath.Join(dir, "none")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Dir missing: err = %v, want ErrNotExist", err)
	}
}

// A link is never followed: one with a JSON name in any case is refused, any
// other is skipped, wherever it leads.
func TestCheckWalked(t *testing.T) {
	dir := tree(t)
	real := filepath.Join(dir, "real")
	for name, text := range map[string]string{
		"ACC.JSON": "a.json", "gone.json": "missing", "dirlink": "sub",
		"README": "a.json", "out": filepath.Join(dir, "elsewhere"), "up": "../../x",
	} {
		if err := os.Symlink(text, filepath.Join(real, name)); err != nil {
			t.Fatal(err)
		}
	}
	want := map[string][2]bool{ // name → skip, error
		"a.json": {false, false}, "sub": {false, false},
		"l.json": {true, true}, "ACC.JSON": {true, true}, "gone.json": {true, true},
		"dirlink": {true, false}, "README": {true, false}, "out": {true, false}, "up": {true, false},
	}
	entries, err := os.ReadDir(real)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		skip, err := CheckWalked(filepath.Join(real, e.Name()), e)
		if got := [2]bool{skip, err != nil}; got != want[e.Name()] {
			t.Errorf("%s: skip, err = %v, %v; want %v", e.Name(), skip, err, want[e.Name()])
		}
		if err != nil && !errors.Is(err, ErrNotRegular) {
			t.Errorf("%s: err = %v, want ErrNotRegular", e.Name(), err)
		}
	}
	if err := CheckEntry(entries[0]); err != nil && entries[0].Name() == "a.json" {
		t.Errorf("CheckEntry regular: %v", err)
	}
}

func TestFoldKey(t *testing.T) {
	for _, pair := range [][2]string{{"realm.json", "REALM.JSON"}, {"realm.json", "realm.jſon"}, {"k", "K"}, {"App", "aPP"}} {
		if FoldKey(pair[0]) != FoldKey(pair[1]) {
			t.Errorf("FoldKey(%q) != FoldKey(%q)", pair[0], pair[1])
		}
	}
	if FoldKey("app") == FoldKey("apps") {
		t.Error("different names fold alike")
	}
	if !IsJSONName("X.Json") || IsJSONName("x.jsonl") {
		t.Error("IsJSONName")
	}
}

// ReadUnder reads through real directories only.
func TestReadUnder(t *testing.T) {
	dir := tree(t)
	if data, err := ReadUnder(dir, "real/a.json"); err != nil || string(data) != "{}" {
		t.Errorf("ReadUnder real = %q, %v", data, err)
	}
	for _, rel := range []string{"link/a.json", "real/l.json", "real/a.json/x"} {
		if _, err := ReadUnder(dir, rel); !errors.Is(err, ErrNotRegular) {
			t.Errorf("ReadUnder(%s): err = %v, want ErrNotRegular", rel, err)
		}
	}
	if _, err := ReadUnder(dir, "none/a.json"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("ReadUnder missing dir: err = %v", err)
	}
}

func TestDirFromAndRegularUnder(t *testing.T) {
	dir := tree(t)
	real := filepath.Join(dir, "real")
	if got, err := DirFrom(filepath.Join(real, "sub"), "../sub"); err != nil || got != filepath.Join(real, "sub") {
		t.Errorf("DirFrom real = %q, %v", got, err)
	}
	if _, err := DirFrom(dir, "link"); !errors.Is(err, ErrNotRegular) {
		t.Errorf("DirFrom through a link: %v", err)
	}
	if _, err := DirFrom(dir, "missing"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("DirFrom missing: %v", err)
	}
	for rel, want := range map[string]bool{"real/a.json": true, "link/a.json": false, "real/l.json": false, "real/none.json": false, "real/sub": false} {
		if got, err := RegularUnder(dir, rel); err != nil || got != want {
			t.Errorf("RegularUnder(%s) = %v, %v; want %v", rel, got, err, want)
		}
	}
}

// DirFrom starts from the absolute directory, so ".." from a relative one goes
// up as the OS goes.
func TestDirFromRelativeStart(t *testing.T) {
	dir := tree(t)
	t.Chdir(filepath.Join(dir, "real"))
	got, err := DirFrom(".", "..")
	if err != nil || got != dir {
		t.Errorf(`DirFrom(".", "..") = %q, %v; want %q`, got, err, dir)
	}
}
