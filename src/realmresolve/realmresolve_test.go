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

	// Should have an info diagnostic (no error code).
	var hasInfo bool
	for _, d := range res.Diagnostics {
		if d.Severity == result.Info {
			hasInfo = true
		}
	}
	if !hasInfo {
		t.Fatal("expected Info diagnostic for skipped remote source")
	}
}

func TestResolve_AbsolutePathRejected(t *testing.T) {
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: "/absolute/path"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error for absolute path")
	}
	if !hasDiagCode(res, "RR001") {
		t.Errorf("expected RR001 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_DepVsDepCollision(t *testing.T) {
	// dep-a and dep-b both have EUID "DEP" — should produce RR005.
	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: "../dep-a/"},
			{RealmID: "dev.dep.b", Version: "0.1.0", Source: "../dep-b/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !hasDiagCode(res, "RR005") {
		t.Errorf("expected RR005 for dep-vs-dep EUID collision, got: %v", res.Diagnostics)
	}
}

// --- Sub-realm tests ---

func TestResolve_SubRealms_Success(t *testing.T) {
	rf := loadTestRealm(t, "parent-with-sub")
	baseDir := absTestdata(t, "parent-with-sub")

	resolver := New()
	got, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !res.OK() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if len(got.SubRealms) != 1 {
		t.Fatalf("expected 1 sub-realm, got %d", len(got.SubRealms))
	}

	sub := got.SubRealms[0]
	if sub.Realm.Realm.ID != "dev.steerspec.test.sync" {
		t.Errorf("expected sub-realm ID dev.steerspec.test.sync, got %s", sub.Realm.Realm.ID)
	}
	if _, ok := sub.EUIDs["SYNC"]; !ok {
		t.Errorf("expected EUID SYNC in sub-realm, got %v", sub.EUIDs)
	}
}

func TestResolve_SubRealm_MissingDir(t *testing.T) {
	rf := &entity.RealmFile{
		Realm:     entity.RealmMeta{ID: "dev.steerspec.test", Title: "Test", Version: "0.1.0"},
		SubRealms: []string{"nonexistent"},
	}
	baseDir := absTestdata(t, "parent-with-sub")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR007") {
		t.Errorf("expected RR007 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_MissingRealmJSON(t *testing.T) {
	rf := loadTestRealm(t, "sub-missing-realm")
	baseDir := absTestdata(t, "sub-missing-realm")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR008") {
		t.Errorf("expected RR008 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_IDMismatch(t *testing.T) {
	rf := loadTestRealm(t, "sub-id-mismatch")
	baseDir := absTestdata(t, "sub-id-mismatch")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR009") {
		t.Errorf("expected RR009 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_NestedSubRealms(t *testing.T) {
	rf := loadTestRealm(t, "sub-nested-sub")
	baseDir := absTestdata(t, "sub-nested-sub")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics")
	}
	if !hasDiagCode(res, "RR010") {
		t.Errorf("expected RR010 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_EUIDCollision_ParentVsSub(t *testing.T) {
	rf := loadTestRealm(t, "sub-collision-parent")
	baseDir := absTestdata(t, "sub-collision-parent")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !hasDiagCode(res, "RR011") {
		t.Errorf("expected RR011 diagnostic for parent-vs-sub EUID collision, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_EUIDCollision_SubVsSub(t *testing.T) {
	rf := loadTestRealm(t, "sub-collision-sibling")
	baseDir := absTestdata(t, "sub-collision-sibling")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !hasDiagCode(res, "RR011") {
		t.Errorf("expected RR011 diagnostic for sub-vs-sub EUID collision, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_EUIDCollision_SubVsDep(t *testing.T) {
	rf := loadTestRealm(t, "sub-collision-dep")
	baseDir := absTestdata(t, "sub-collision-dep")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if !hasDiagCode(res, "RR011") {
		t.Errorf("expected RR011 diagnostic for sub-vs-dep EUID collision, got: %v", res.Diagnostics)
	}
}

func TestResolve_SubRealm_PathTraversal(t *testing.T) {
	rf := &entity.RealmFile{
		Realm:     entity.RealmMeta{ID: "dev.steerspec.test", Title: "Test", Version: "0.1.0"},
		SubRealms: []string{"../escape"},
	}
	baseDir := absTestdata(t, "parent-with-sub")

	resolver := New()
	_, res := resolver.Resolve(context.Background(), rf, baseDir)

	if res.OK() {
		t.Fatal("expected error diagnostics for path traversal")
	}
	if !hasDiagCode(res, "RR007") {
		t.Errorf("expected RR007 diagnostic, got: %v", res.Diagnostics)
	}
}

func TestResolve_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	rf := &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: "dev.dep.a", Version: "0.1.0", Source: "../dep-a/"},
		},
	}
	baseDir := absTestdata(t, "parent")

	resolver := New()
	_, res := resolver.Resolve(ctx, rf, baseDir)

	if res.OK() {
		t.Fatal("expected error for cancelled context")
	}
	if !hasDiagCode(res, "RR000") {
		t.Errorf("expected RR000 diagnostic for cancelled context, got: %v", res.Diagnostics)
	}
}
