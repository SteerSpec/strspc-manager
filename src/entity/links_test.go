package entity_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/internal/realmfs"
)

const linkEntity = `{"entity": {"id": "ENT"}}`

// A link inside a Realm tree, to a file or to a directory, is reported to the
// callback as ErrNotRegular with no data, and the rest of the tree is walked.
func TestWalkEntityFiles_ReportsLinksInTheTree(t *testing.T) {
	for _, mode := range walkModes {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWalkFile(t, dir, "ENT.json", linkEntity)
			other := t.TempDir()
			writeWalkFile(t, other, "OUT.json", `{"entity": {"id": "OUT"}}`)
			fileLink := filepath.Join(dir, "LINK.json")
			if err := os.Symlink(filepath.Join(other, "OUT.json"), fileLink); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			dirLink := filepath.Join(dir, "sub")
			if err := os.Symlink(other, dirLink); err != nil {
				t.Fatal(err)
			}
			var parsed, refused []string
			err := entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
				switch {
				case errors.Is(parseErr, realmfs.ErrNotRegular):
					if data != nil || ef != nil {
						t.Errorf("%s: got data for a link", path)
					}
					refused = append(refused, path)
				case parseErr != nil:
					t.Errorf("%s: %v", path, parseErr)
				default:
					parsed = append(parsed, ef.Entity.ID)
				}
				return nil
			}, entity.WithRecursive(mode.recursive))
			if err != nil {
				t.Fatalf("WalkEntityFiles: %v", err)
			}
			slices.Sort(refused)
			if want := []string{fileLink, dirLink}; !slices.Equal(refused, want) {
				t.Errorf("refused %v, want %v", refused, want)
			}
			if !slices.Equal(parsed, []string{"ENT"}) {
				t.Errorf("parsed %v, want [ENT]", parsed)
			}
		})
	}
}

// A link naming the root is resolved, and paths stay under the root as given.
func TestWalkEntityFiles_ResolvesALinkedRoot(t *testing.T) {
	for _, mode := range walkModes {
		t.Run(mode.name, func(t *testing.T) {
			real := t.TempDir()
			writeWalkFile(t, real, "ENT.json", linkEntity)
			root := filepath.Join(t.TempDir(), "rules")
			if err := os.Symlink(real, root); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			var paths []string
			err := entity.WalkEntityFiles(root, func(path string, _ []byte, _ *entity.File, parseErr error) error {
				if parseErr != nil {
					t.Errorf("%s: %v", path, parseErr)
				}
				paths = append(paths, path)
				return nil
			}, entity.WithRecursive(mode.recursive))
			if err != nil {
				t.Fatalf("WalkEntityFiles: %v", err)
			}
			if want := []string{filepath.Join(root, "ENT.json")}; !slices.Equal(paths, want) {
				t.Errorf("paths %v, want %v", paths, want)
			}
		})
	}
}

func TestLoadRefusesALink(t *testing.T) {
	dir := t.TempDir()
	writeWalkFile(t, dir, "ENT.json", linkEntity)
	writeWalkFile(t, dir, "realm.json", `{"realm": {"id": "x"}}`)
	for _, name := range []string{"ENT.json", "realm.json"} {
		link := filepath.Join(dir, "l-"+name)
		if err := os.Symlink(filepath.Join(dir, name), link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	if _, err := entity.Load(filepath.Join(dir, "l-ENT.json")); !errors.Is(err, realmfs.ErrNotRegular) {
		t.Errorf("Load link: %v", err)
	}
	if _, err := entity.LoadRealm(filepath.Join(dir, "l-realm.json")); !errors.Is(err, realmfs.ErrNotRegular) {
		t.Errorf("LoadRealm link: %v", err)
	}
}

// F8: a link that names no JSON file and leads to no directory holds no
// entity file, so it is left alone, as other non-JSON files are.
func TestWalkEntityFiles_IgnoresNonJSONFileLinks(t *testing.T) {
	dir := t.TempDir()
	writeWalkFile(t, dir, "ENT.json", linkEntity)
	writeWalkFile(t, dir, "README.md", "x")
	if err := os.Symlink("README.md", filepath.Join(dir, "README")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "bin")); err != nil {
		t.Fatal(err)
	}
	var refused []string
	err := entity.WalkEntityFiles(dir, func(path string, _ []byte, _ *entity.File, parseErr error) error {
		if errors.Is(parseErr, realmfs.ErrNotRegular) {
			refused = append(refused, filepath.Base(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(refused, []string{"bin"}) {
		t.Errorf("refused %v, want only the directory link bin", refused)
	}
}
