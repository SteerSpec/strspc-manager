package ruleresolve

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func hasCode(res *result.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func assertHasCode(t *testing.T, res *result.Result, code string) {
	t.Helper()
	if !hasCode(res, code) {
		t.Errorf("expected diagnostic %s, got: %v", code, res.Diagnostics)
	}
}

func TestResolve_LocalValid(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("valid"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	if !res.OK() {
		t.Fatalf("expected OK, got errors: %v", res.Errors())
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}

	ids := map[string]bool{}
	for _, f := range files {
		ids[f.File.Entity.ID] = true
	}
	if !ids["NTE"] || !ids["RUL"] {
		t.Errorf("expected NTE and RUL, got %v", ids)
	}
}

func TestResolve_LocalEmpty(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("empty"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}
	assertHasCode(t, res, "RSV006")
	// RSV006 is a warning, so OK should still be true.
	if !res.OK() {
		t.Error("expected OK (warning only)")
	}
}

func TestResolve_LocalNotExist(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("does-not-exist"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV002")
	if res.OK() {
		t.Error("expected error")
	}
}

func TestResolve_LocalInvalidJSON(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("invalid"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV003")
}

func TestResolve_LocalHashMismatch(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("invalid"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV004")
}

func TestResolve_EUIDCollision(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("collision_a"), Scope: ScopeGlobal},
		{Source: testdataPath("collision_b"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV005")
}

func TestResolve_SkipsRealmJSON(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("with_realm"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	if !res.OK() {
		t.Fatalf("expected OK, got errors: %v", res.Errors())
	}
	// Only the entity file, not realm.json.
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].File.Entity.ID != "NTE" {
		t.Errorf("expected NTE, got %s", files[0].File.Entity.ID)
	}
}

func TestResolve_Recursive(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("valid"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, _ := r.Resolve(context.Background())
	// valid/ has NTE.json at root and RUL.json in sub/.
	if len(files) < 2 {
		t.Errorf("expected at least 2 files (recursive), got %d", len(files))
	}
}

func TestResolve_UnsupportedScheme(t *testing.T) {
	_, err := New([]SourceEntry{
		{Source: "github://SteerSpec/rules@v1", Scope: ScopeGlobal},
	})
	if err == nil {
		t.Error("expected error for github:// scheme")
	}
}

func TestResolve_MultipleSources(t *testing.T) {
	r, err := New([]SourceEntry{
		{Source: testdataPath("valid"), Scope: ScopeGlobal},
		{Source: testdataPath("with_realm"), Scope: ScopeLocal},
	})
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	// valid/ has 2 files (NTE + RUL), with_realm/ has 1 (NTE).
	// NTE appears in both sources from same testdata — collision expected.
	if len(files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(files))
	}
	// Should detect collision on NTE across the two sources.
	assertHasCode(t, res, "RSV005")
}

func TestResolve_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r, err := New([]SourceEntry{
		{Source: testdataPath("valid"), Scope: ScopeGlobal},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(ctx)
	if res.OK() {
		t.Error("expected error from cancelled context")
	}
	assertHasCode(t, res, "RSV000")
}
