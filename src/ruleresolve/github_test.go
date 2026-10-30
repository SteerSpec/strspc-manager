package ruleresolve

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

	"github.com/SteerSpec/strspc-manager/src/remote"
)

// tarballFromDirs builds a GitHub-style tarball (single top-level prefix)
// whose contents are the given local directories mounted at the given
// repository sub-paths.
func tarballFromDirs(t *testing.T, prefix string, mounts map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for repoPath, localDir := range mounts {
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
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fakeGitHubServer serves the given tarball for every /tarball/ request and
// 404 for anything else.
func fakeGitHubServer(t *testing.T, tarball []byte) *remote.Fetcher {
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

func TestGitHubSource_Success(t *testing.T) {
	tb := tarballFromDirs(t, "SteerSpec-rules-abc", map[string]string{
		"rules/core": testdataPath("valid"),
	})
	fetcher := fakeGitHubServer(t, tb)

	r, err := New([]SourceEntry{
		{Source: "github://SteerSpec/rules@v1.0.0/rules/core/", Scope: ScopeGlobal},
	}, WithRemoteFetcher(fetcher))
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
	for _, f := range files {
		if f.ResolvedSource != "github://SteerSpec/rules@v1.0.0/rules/core" {
			t.Errorf("ResolvedSource = %q, want normalised URI", f.ResolvedSource)
		}
		if !strings.HasPrefix(f.Path, fetcher.CacheDir) {
			t.Errorf("Path %q should live under the cache dir", f.Path)
		}
	}
}

func TestGitHubSource_HashMismatchIsRSV004(t *testing.T) {
	tb := tarballFromDirs(t, "o-r-1", map[string]string{
		"rules": testdataPath("invalid"),
	})
	fetcher := fakeGitHubServer(t, tb)

	r, err := New([]SourceEntry{
		{Source: "github://o/r@v1/rules", Scope: ScopeGlobal},
	}, WithRemoteFetcher(fetcher))
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV004")
	assertHasCode(t, res, "RSV003") // broken.json in the same fixture
	if hasCode(res, "RSV007") {
		t.Errorf("fetch succeeded; RSV007 must not be reported: %v", res.Diagnostics)
	}
}

func TestGitHubSource_FetchFailureIsRSV007(t *testing.T) {
	fetcher := fakeGitHubServer(t, nil) // every request 404s

	r, err := New([]SourceEntry{
		{Source: "github://o/missing@v1/rules", Scope: ScopeGlobal},
	}, WithRemoteFetcher(fetcher))
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV007")
	if len(files) != 0 {
		t.Errorf("expected no files, got %d", len(files))
	}
	for _, d := range res.Diagnostics {
		if d.Code == "RSV007" && !strings.Contains(d.Message, "missing token") {
			t.Errorf("RSV007 message should carry the remote hint, got %q", d.Message)
		}
	}
}

func TestGitHubSource_CollisionWithLocal(t *testing.T) {
	tb := tarballFromDirs(t, "o-r-1", map[string]string{
		"rules": testdataPath("collision_a"),
	})
	fetcher := fakeGitHubServer(t, tb)

	r, err := New([]SourceEntry{
		{Source: "github://o/r@v1/rules", Scope: ScopeGlobal},
		{Source: testdataPath("collision_b"), Scope: ScopeLocal},
	}, WithRemoteFetcher(fetcher))
	if err != nil {
		t.Fatal(err)
	}

	_, res := r.Resolve(context.Background())
	assertHasCode(t, res, "RSV005")
}

func TestNew_BuildsDefaultFetcherFromConfig(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "tok")
	cacheDir := t.TempDir()
	r, err := New([]SourceEntry{
		{Source: "github://o/r@v1/rules", Scope: ScopeGlobal},
	}, WithCacheDir(cacheDir), WithForceSync(true))
	if err != nil {
		t.Fatal(err)
	}
	f := r.cfg.Fetcher
	if f == nil {
		t.Fatal("expected a default fetcher")
	}
	if f.CacheDir != cacheDir || !f.Force || f.Token != "tok" || f.TTL != r.cfg.CacheTTL {
		t.Errorf("fetcher not built from config: %+v", f)
	}
}

func TestParseSource_DefaultFetcherLimitsToken(t *testing.T) {
	cfg := &Config{CacheDir: t.TempDir()}
	if _, err := parseSource(SourceEntry{Source: "github://o/r@v1"}, cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Fetcher == nil || len(cfg.Fetcher.TokenOwners) == 0 {
		t.Errorf("default fetcher has no TokenOwners: %+v", cfg.Fetcher)
	}
}
