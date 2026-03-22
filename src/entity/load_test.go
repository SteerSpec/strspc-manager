package entity_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func testdataPath(parts ...string) string {
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(append([]string{dir, "testdata"}, parts...)...)
}

func TestLoad_ValidBasic(t *testing.T) {
	f, err := entity.Load(testdataPath("valid", "basic.json"))
	if err != nil {
		t.Fatalf("Load valid/basic.json: %v", err)
	}
	if f.Entity.ID == "" {
		t.Error("expected non-empty entity ID")
	}
}

func TestLoad_ValidNested(t *testing.T) {
	f, err := entity.Load(testdataPath("valid", "nested.json"))
	if err != nil {
		t.Fatalf("Load valid/nested.json: %v", err)
	}
	if len(f.SubEntities) == 0 {
		t.Error("expected sub_entities in nested fixture")
	}
}

func TestLoad_InvalidJSON(t *testing.T) {
	_, err := entity.Load(testdataPath("invalid", "bad_json.json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoad_NotFound(t *testing.T) {
	_, err := entity.Load(testdataPath("nonexistent.json"))
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestParse_Valid(t *testing.T) {
	data := []byte(`{
		"$schema": "entity.v1.schema.json",
		"entity": {"id": "TEST", "title": "Test Entity"},
		"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
		"rules": [],
		"notes": []
	}`)
	f, err := entity.Parse(data)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Entity.ID != "TEST" {
		t.Errorf("got entity ID %q, want %q", f.Entity.ID, "TEST")
	}
}

func TestParse_InvalidJSON(t *testing.T) {
	_, err := entity.Parse([]byte("{bad"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLoadRealm_Valid(t *testing.T) {
	r, err := entity.LoadRealm(testdataPath("valid", "realm.json"))
	if err != nil {
		t.Fatalf("LoadRealm: %v", err)
	}
	if r.Realm.ID == "" {
		t.Error("expected non-empty realm ID")
	}
}

func TestParseRealm_Valid(t *testing.T) {
	data := []byte(`{
		"$schema": "realm.v1.schema.json",
		"realm": {"id": "dev.test.example", "title": "Test Realm", "version": "0.1.0"},
		"dependencies": []
	}`)
	r, err := entity.ParseRealm(data)
	if err != nil {
		t.Fatalf("ParseRealm: %v", err)
	}
	if r.Realm.ID != "dev.test.example" {
		t.Errorf("got realm ID %q, want %q", r.Realm.ID, "dev.test.example")
	}
}
