package entity_test

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

const walkTestDir = "testdata/walk"

// walkModes lists the two traversals; behaviour they must share is tested
// against both.
var walkModes = []struct {
	name      string
	recursive bool
}{
	{"recursive", true},
	{"shallow", false},
}

// writeWalkFile writes content to name under dir.
func writeWalkFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestWalkEntityFiles_Recursive(t *testing.T) {
	var found []string
	var parseErrors []string

	err := entity.WalkEntityFiles(walkTestDir, func(path string, data []byte, ef *entity.File, parseErr error) error {
		rel, _ := filepath.Rel(walkTestDir, path)
		if parseErr != nil {
			parseErrors = append(parseErrors, rel)
			return nil
		}
		found = append(found, ef.Entity.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkEntityFiles: %v", err)
	}

	sort.Strings(found)
	if len(found) != 3 || found[0] != "ENT" || found[1] != "EXCL" || found[2] != "SUB" {
		t.Errorf("expected [ENT, EXCL, SUB], got %v", found)
	}

	if len(parseErrors) != 1 {
		t.Errorf("expected 1 parse error (invalid.json), got %d: %v", len(parseErrors), parseErrors)
	}
}

func TestWalkEntityFiles_Shallow(t *testing.T) {
	var found []string

	err := entity.WalkEntityFiles(walkTestDir, func(path string, data []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			return nil
		}
		found = append(found, ef.Entity.ID)
		return nil
	}, entity.WithRecursive(false))
	if err != nil {
		t.Fatalf("WalkEntityFiles: %v", err)
	}

	if len(found) != 1 || found[0] != "ENT" {
		t.Errorf("expected [ENT] (shallow), got %v", found)
	}
}

func TestWalkEntityFiles_SkipsSchemaDir(t *testing.T) {
	var paths []string

	err := entity.WalkEntityFiles(walkTestDir, func(path string, _ []byte, _ *entity.File, _ error) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkEntityFiles: %v", err)
	}

	for _, p := range paths {
		if filepath.Base(filepath.Dir(p)) == "_schema" {
			t.Errorf("should not visit _schema files, got %s", p)
		}
	}
}

func TestWalkEntityFiles_SkipsRealmJSON(t *testing.T) {
	var paths []string

	err := entity.WalkEntityFiles(walkTestDir, func(path string, _ []byte, _ *entity.File, _ error) error {
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkEntityFiles: %v", err)
	}

	for _, p := range paths {
		if filepath.Base(p) == "realm.json" {
			t.Errorf("should not visit realm.json, got %s", p)
		}
	}
}

func TestWalkEntityFiles_ProvidesRawData(t *testing.T) {
	err := entity.WalkEntityFiles(walkTestDir, func(path string, data []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			return nil
		}
		if len(data) == 0 {
			t.Errorf("expected non-empty data for %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkEntityFiles: %v", err)
	}
}

func TestWalkEntityFiles_ExcludeDirs(t *testing.T) {
	t.Run("recursive", func(t *testing.T) {
		var found []string
		err := entity.WalkEntityFiles(walkTestDir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
			if parseErr != nil {
				return nil
			}
			found = append(found, ef.Entity.ID)
			return nil
		}, entity.WithExcludeDirs([]string{"excluded", "subdir"}))
		if err != nil {
			t.Fatalf("WalkEntityFiles: %v", err)
		}

		sort.Strings(found)
		if len(found) != 1 || found[0] != "ENT" {
			t.Errorf("expected [ENT] (excluded dirs skipped), got %v", found)
		}
	})

	t.Run("shallow", func(t *testing.T) {
		// Shallow mode only processes files, not directories, so exclude
		// should not affect the results for files. This verifies the
		// filter is applied without error.
		var found []string
		err := entity.WalkEntityFiles(walkTestDir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
			if parseErr != nil {
				return nil
			}
			found = append(found, ef.Entity.ID)
			return nil
		}, entity.WithRecursive(false), entity.WithExcludeDirs([]string{"excluded"}))
		if err != nil {
			t.Fatalf("WalkEntityFiles: %v", err)
		}

		if len(found) != 1 || found[0] != "ENT" {
			t.Errorf("expected [ENT] (shallow), got %v", found)
		}
	})

	t.Run("nil_dirs", func(t *testing.T) {
		// Empty/nil dirs should not break anything.
		var found []string
		err := entity.WalkEntityFiles(walkTestDir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
			if parseErr != nil {
				return nil
			}
			found = append(found, ef.Entity.ID)
			return nil
		}, entity.WithExcludeDirs(nil))
		if err != nil {
			t.Fatalf("WalkEntityFiles: %v", err)
		}

		sort.Strings(found)
		// Should find all entities including EXCL, SUB, ENT.
		if len(found) != 3 {
			t.Errorf("expected 3 entities with no exclusions, got %v", found)
		}
	})
}

func TestWalkEntityFiles_NonexistentDir(t *testing.T) {
	err := entity.WalkEntityFiles("testdata/nonexistent", func(_ string, _ []byte, _ *entity.File, _ error) error {
		t.Error("should not be called")
		return nil
	})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

func TestWalkEntityFiles_MissingRoot(t *testing.T) {
	for _, mode := range walkModes {
		t.Run(mode.name, func(t *testing.T) {
			missing := filepath.Join(t.TempDir(), "missing")
			err := entity.WalkEntityFiles(missing, func(string, []byte, *entity.File, error) error {
				t.Error("callback must not run for a missing root")
				return nil
			}, entity.WithRecursive(mode.recursive))
			if !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("error = %v, want it to wrap fs.ErrNotExist", err)
			}
		})
	}
}

func TestWalkEntityFiles_CallbackResultControlsWalk(t *testing.T) {
	errStop := errors.New("stop")

	tests := []struct {
		name    string
		result  error
		wantErr error
	}{
		{"fs.SkipAll ends the walk cleanly", fs.SkipAll, nil},
		{"any other error aborts with that error", errStop, errStop},
	}
	for _, mode := range walkModes {
		for _, tt := range tests {
			t.Run(mode.name+"/"+tt.name, func(t *testing.T) {
				dir := t.TempDir()
				writeWalkFile(t, dir, "A.json", `{"entity": {"id": "AAA"}}`)
				writeWalkFile(t, dir, "B.json", `{"entity": {"id": "BBB"}}`)

				calls := 0
				err := entity.WalkEntityFiles(dir, func(string, []byte, *entity.File, error) error {
					calls++
					return tt.result
				}, entity.WithRecursive(mode.recursive))
				if !errors.Is(err, tt.wantErr) {
					t.Errorf("error = %v, want %v", err, tt.wantErr)
				}
				if calls != 1 {
					t.Errorf("callback ran %d times, want the walk to stop after 1", calls)
				}
			})
		}
	}
}

func TestWalkEntityFiles_SkipsRealmManifestUnderAnotherName(t *testing.T) {
	for _, mode := range walkModes {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWalkFile(t, dir, "ENT.json", `{"entity": {"id": "ENT"}}`)
			writeWalkFile(t, dir, "manifest.json", `{"realm": {"id": "dev.test.example"}}`)

			var seen []string
			err := entity.WalkEntityFiles(dir, func(path string, _ []byte, _ *entity.File, _ error) error {
				seen = append(seen, filepath.Base(path))
				return nil
			}, entity.WithRecursive(mode.recursive))
			if err != nil {
				t.Fatalf("WalkEntityFiles: %v", err)
			}
			if len(seen) != 1 || seen[0] != "ENT.json" {
				t.Errorf("visited %v, want only ENT.json", seen)
			}
		})
	}
}

func TestWalkEntityFiles_ReportsUnreadableFileAndContinues(t *testing.T) {
	for _, mode := range walkModes {
		t.Run(mode.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWalkFile(t, dir, "ENT.json", `{"entity": {"id": "ENT"}}`)
			dangling := filepath.Join(dir, "broken.json")
			if err := os.Symlink(filepath.Join(dir, "gone"), dangling); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			var ids []string
			var readErr error
			err := entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
				if path != dangling {
					ids = append(ids, ef.Entity.ID)
					return nil
				}
				if data != nil || ef != nil {
					t.Errorf("read failure got data %q and file %+v, want both nil", data, ef)
				}
				readErr = parseErr
				return nil
			}, entity.WithRecursive(mode.recursive))
			if err != nil {
				t.Fatalf("WalkEntityFiles: %v", err)
			}
			if !errors.Is(readErr, fs.ErrNotExist) {
				t.Errorf("parseErr = %v, want it to wrap fs.ErrNotExist", readErr)
			}
			if len(ids) != 1 || ids[0] != "ENT" {
				t.Errorf("parsed %v, want the walk to reach ENT", ids)
			}
		})
	}
}

func TestWalkEntityFiles_TraversalError(t *testing.T) {
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("needs directory modes enforced: not on Windows or as root")
	}
	errStop := errors.New("stop")

	tests := []struct {
		name      string
		result    error
		wantErr   error
		wantFiles []string
	}{
		{"callback continues past the directory", nil, nil, []string{"A.json", "z.json"}},
		{"callback aborts the walk", errStop, errStop, []string{"A.json"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeWalkFile(t, dir, "A.json", `{"entity": {"id": "AAA"}}`)
			writeWalkFile(t, dir, "z.json", `{"entity": {"id": "ZZZ"}}`)
			locked := filepath.Join(dir, "locked")
			if err := os.Mkdir(locked, 0o000); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(locked, 0o755); err != nil {
					t.Errorf("restore permissions: %v", err)
				}
			})

			var files []string
			var traversalErr error
			err := entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
				if path != locked {
					files = append(files, filepath.Base(path))
					return nil
				}
				if data != nil || ef != nil {
					t.Errorf("traversal failure got data %q and file %+v, want both nil", data, ef)
				}
				traversalErr = parseErr
				return tt.result
			})
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
			if !errors.Is(traversalErr, fs.ErrPermission) {
				t.Errorf("parseErr = %v, want it to wrap fs.ErrPermission", traversalErr)
			}
			if !slices.Equal(files, tt.wantFiles) {
				t.Errorf("visited %v, want %v", files, tt.wantFiles)
			}
		})
	}
}

func TestWalkEntityFiles_ShallowWrappedSkipAll(t *testing.T) {
	// A callback may wrap fs.SkipAll (fmt.Errorf("...: %w", fs.SkipAll)); the
	// shallow walk must still stop cleanly and not visit later entries.
	dir := t.TempDir()
	src, err := os.ReadFile(filepath.Join(walkTestDir, "ENT.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, name := range []string{"A.json", "B.json", "C.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	calls := 0
	err = entity.WalkEntityFiles(dir, func(path string, data []byte, ef *entity.File, parseErr error) error {
		calls++
		return fmt.Errorf("stop after %s: %w", filepath.Base(path), fs.SkipAll)
	}, entity.WithRecursive(false))
	if err != nil {
		t.Fatalf("wrapped fs.SkipAll must end the walk cleanly, got %v", err)
	}
	if calls != 1 {
		t.Errorf("expected the walk to stop after the first entry, callback ran %d times", calls)
	}
}
