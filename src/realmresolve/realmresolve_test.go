package realmresolve

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

func absTestdata(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return p
}

func loadTestRealm(t *testing.T, rel string) *entity.RealmFile {
	t.Helper()
	p := filepath.Join("testdata", rel, "realm.json")
	rf, err := entity.LoadRealm(p)
	if err != nil {
		t.Fatalf("LoadRealm(%s): %v", p, err)
	}
	return rf
}

func hasDiagCode(res *result.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func diagWithCode(res *result.Result, code string) *result.Diagnostic {
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Code == code {
			return &res.Diagnostics[i]
		}
	}
	return nil
}

func TestResolve_LocalSuccess(t *testing.T) {
	rf := loadTestRealm(t, "parent")
	baseDir := absTestdata(t, "parent")

	resolver := New()
	got, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !res.OK() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(got.Dependencies))
	}

	dep := got.Dependencies[0]
	if dep.Realm.Realm.ID != "dev.dep.a" {
		t.Errorf("expected realm ID dev.dep.a, got %s", dep.Realm.Realm.ID)
	}
	if _, ok := dep.EUIDs["DEP"]; !ok {
		t.Errorf("expected EUID DEP in resolved dependency, got %v", dep.EUIDs)
	}
}

func TestResolve_NoDependencies(t *testing.T) {
	rf := &entity.RealmFile{
		Realm:        entity.RealmMeta{ID: "dev.empty", Title: "Empty", Version: "0.1.0"},
		Dependencies: nil,
	}
	baseDir := absTestdata(t, "dep-a") // any valid dir

	resolver := New()
	got, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !res.OK() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if len(got.Dependencies) != 0 {
		t.Errorf("expected 0 dependencies, got %d", len(got.Dependencies))
	}
}

func TestResolve_MissingDir(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.nonexistent", Version: "0.1.0", Source: "../nonexistent/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR002") {
		t.Errorf("expected RR002 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_RealmIDMismatch(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: "../dep-wrong-id/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR003") {
		t.Errorf("expected RR003 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_VersionMismatch(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: "../dep-wrong-version/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR004") {
		t.Errorf("expected RR004 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_EUIDCollision(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.collision", Version: "0.1.0", Source: "../dep-collision/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !hasDiagCode(res, "RR005") {
		t.Errorf("expected RR005 diagnostic for EUID collision, got: %v", res.Diagnostics)
	}
}

func TestResolve_SelfReference(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.steerspec.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.steerspec.test", Version: "0.1.0", Source: "../parent/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR006") {
		t.Errorf("expected RR006 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_EmptySource(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: ""},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR001") {
		t.Errorf("expected RR001 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_RemoteSourceSkipped(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.remote", Version: "1.0.0", Source: "github://org/repo"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	got, res := resolver.Resolve(context.Background(), rf, baseDir)

	// Should not produce errors, only info.
	if !res.OK() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if len(got.Dependencies) != 0 {
		t.Errorf("expected 0 resolved dependencies (remote skipped), got %d", len(got.Dependencies))
	}

	d := diagWithCode(res, "RR001")
	if d == nil {
		t.Fatal("expected RR001 info diagnostic for skipped remote source")
	}
	if d.Severity != result.Info {
		t.Errorf("expected Info severity, got %s", d.Severity)
	}
}
