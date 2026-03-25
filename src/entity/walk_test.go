package entity_test

import (
	"path/filepath"
	"sort"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

const walkTestDir = "testdata/walk"

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
	if len(found) != 2 || found[0] != "ENT" || found[1] != "SUB" {
		t.Errorf("expected [ENT, SUB], got %v", found)
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

func TestWalkEntityFiles_NonexistentDir(t *testing.T) {
	err := entity.WalkEntityFiles("testdata/nonexistent", func(_ string, _ []byte, _ *entity.File, _ error) error {
		t.Error("should not be called")
		return nil
	})
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}
