package realmresolve

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/remote"
)

// tarballFromDir builds a GitHub-style tarball exposing localDir at
// repoPath under a single top-level prefix.
func tarballFromDir(t *testing.T, prefix, repoPath, localDir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	err := filepath.WalkDir(localDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(localDir, p)
		name := prefix + "/" + repoPath
		if rel != "." {
			name += "/" + filepath.ToSlash(rel)
		}
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(data))}); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// remoteFetcher serves tarball for every /tarball/ request (404 otherwise).
func remoteFetcher(t *testing.T, tarball []byte) *remote.Fetcher {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/tarball/") && tarball != nil {
			_, _ = w.Write(tarball)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return &remote.Fetcher{Client: srv.Client(), BaseURL: srv.URL, CacheDir: t.TempDir()}
}

func remoteParent(depID, version, source string) *entity.RealmFile {
	return &entity.RealmFile{
		Realm: entity.RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"},
		Dependencies: []entity.RealmDep{
			{RealmID: depID, Version: version, Source: source},
		},
	}
}

func TestResolve_RemoteDep_Success(t *testing.T) {
	tb := tarballFromDir(t, "o-r-abc", "rules/dep", absTestdata(t, "dep-a"))
	fetcher := remoteFetcher(t, tb)

	rf := remoteParent("dev.dep.a", "0.1.0", "github://o/r@v0.1.0/rules/dep")
	resolver := New(WithRemoteFetcher(fetcher))
	got, res := resolver.Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !res.OK() {
		t.Fatalf("expected no errors, got: %v", res.Diagnostics)
	}
	if len(got.Dependencies) != 1 {
		t.Fatalf("expected 1 dependency, got %d", len(got.Dependencies))
	}
	dep := got.Dependencies[0]
	if dep.Realm.Realm.ID != "dev.dep.a" {
		t.Errorf("realm ID = %s", dep.Realm.Realm.ID)
	}
	if _, ok := dep.EUIDs["DEP"]; !ok {
		t.Errorf("expected EUID DEP, got %v", dep.EUIDs)
	}
	if !strings.HasPrefix(dep.Dir, fetcher.CacheDir) {
		t.Errorf("Dir %q should be inside the cache", dep.Dir)
	}
}

func TestResolve_RemoteDep_RealmIDMismatch(t *testing.T) {
	tb := tarballFromDir(t, "o-r-abc", "rules/dep", absTestdata(t, "dep-a"))
	fetcher := remoteFetcher(t, tb)

	rf := remoteParent("dev.dep.other", "0.1.0", "github://o/r@v0.1.0/rules/dep")
	got, res := New(WithRemoteFetcher(fetcher)).Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR003") {
		t.Errorf("expected RR003 for remote realm ID mismatch, got: %v", res.Diagnostics)
	}
	if len(got.Dependencies) != 0 {
		t.Errorf("expected 0 resolved deps, got %d", len(got.Dependencies))
	}
}

func TestResolve_RemoteDep_VersionMismatch(t *testing.T) {
	tb := tarballFromDir(t, "o-r-abc", "rules/dep", absTestdata(t, "dep-a"))
	fetcher := remoteFetcher(t, tb)

	rf := remoteParent("dev.dep.a", "9.9.9", "github://o/r@v9.9.9/rules/dep")
	_, res := New(WithRemoteFetcher(fetcher)).Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR004") {
		t.Errorf("expected RR004 for remote version mismatch, got: %v", res.Diagnostics)
	}
}

func TestResolve_RemoteDep_FetchFailureIsRR012(t *testing.T) {
	fetcher := remoteFetcher(t, nil) // 404 everywhere

	rf := remoteParent("dev.dep.a", "0.1.0", "github://o/missing@v1/rules")
	_, res := New(WithRemoteFetcher(fetcher)).Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR012") {
		t.Fatalf("expected RR012, got: %v", res.Diagnostics)
	}
}

func TestResolve_RemoteDep_NoFetcherIsRR012(t *testing.T) {
	rf := remoteParent("dev.dep.a", "0.1.0", "github://o/r@v1/rules")
	_, res := New().Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR012") {
		t.Fatalf("expected RR012 without a fetcher, got: %v", res.Diagnostics)
	}
}

func TestResolve_RemoteDep_InvalidURIIsRR012(t *testing.T) {
	rf := remoteParent("dev.dep.a", "0.1.0", "github://o/r")
	_, res := New(WithCacheDir(t.TempDir())).Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR012") {
		t.Fatalf("expected RR012 for missing @ref, got: %v", res.Diagnostics)
	}
}

func TestResolve_RemoteDep_UnsupportedSchemeIsRR012(t *testing.T) {
	rf := remoteParent("dev.dep.a", "0.1.0", "https://example.com/rules")
	_, res := New(WithCacheDir(t.TempDir())).Resolve(context.Background(), rf, absTestdata(t, "parent"))

	if !hasDiagCode(res, "RR012") {
		t.Fatalf("expected RR012 for unsupported scheme, got: %v", res.Diagnostics)
	}
}

func TestNew_DefaultFetcherFromCacheDir(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "tok")
	dir := t.TempDir()
	r := New(WithCacheDir(dir))
	if r.cfg.Fetcher == nil {
		t.Fatal("expected default fetcher when cache dir is set")
	}
	if r.cfg.Fetcher.CacheDir != dir || r.cfg.Fetcher.Token != "tok" || r.cfg.Fetcher.TTL != r.cfg.CacheTTL {
		t.Errorf("fetcher not built from config: %+v", r.cfg.Fetcher)
	}
	if New().cfg.Fetcher != nil {
		t.Error("expected no fetcher without cache dir")
	}
}
