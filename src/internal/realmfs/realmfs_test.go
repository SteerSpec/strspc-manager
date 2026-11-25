package realmfs

import (
	"errors"
	"os"
	"path/filepath"
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

func TestCheckEntry(t *testing.T) {
	dir := tree(t)
	entries, err := os.ReadDir(filepath.Join(dir, "real"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		err := CheckEntry(e)
		if bad := e.Name() == "l.json"; bad != errors.Is(err, ErrNotRegular) {
			t.Errorf("CheckEntry(%s) = %v", e.Name(), err)
		}
	}
}

// A root that resolves to a network share or device path is refused, so a link
// can't route around the check on local sources.
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

func TestReadInTree(t *testing.T) {
	dir := tree(t)
	real := filepath.Join(dir, "real")
	if data, err := ReadInTree(real, filepath.Join(real, "l.json")); err != nil || string(data) != "{}" {
		t.Errorf("in-tree link = %q, %v", data, err)
	}
	if data, err := ReadInTree(real, filepath.Join(real, "a.json")); err != nil || string(data) != "{}" {
		t.Errorf("regular = %q, %v", data, err)
	}
	outside := filepath.Join(t.TempDir(), "o.json")
	if err := os.WriteFile(outside, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"out.json": outside, "dangling.json": "gone", "dir.json": "sub"} {
		if err := os.Symlink(target, filepath.Join(real, name)); err != nil {
			t.Fatal(err)
		}
		if _, err := ReadInTree(real, filepath.Join(real, name)); !errors.Is(err, ErrNotRegular) {
			t.Errorf("%s: err = %v, want ErrNotRegular", name, err)
		}
	}
	if _, err := ReadInTree(real, filepath.Join(real, "sub")); !errors.Is(err, ErrNotRegular) {
		t.Errorf("directory: err = %v", err)
	}
	if _, err := ReadInTree(real, filepath.Join(real, "none")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: err = %v", err)
	}
}
