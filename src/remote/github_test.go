package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// tarEntry describes one entry in a generated tarball.
type tarEntry struct {
	name string
	body string
	dir  bool
	link string // non-empty → symlink
}

// buildTarball produces a gzip-compressed tarball with a GitHub-style
// top-level prefix directory.
func buildTarball(t *testing.T, prefix string, entries []tarEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	if err := tw.WriteHeader(&tar.Header{Name: prefix + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		name := prefix + "/" + e.name
		switch {
		case e.link != "":
			hdr := &tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: e.link, Mode: 0o777}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
		case e.dir:
			if err := tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
				t.Fatal(err)
			}
		default:
			hdr := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(e.body))}
			if err := tw.WriteHeader(hdr); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
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

// fakeGitHub is a minimal stand-in for the GitHub REST API.
type fakeGitHub struct {
	t         *testing.T
	tarballs  map[string][]byte // ref → tarball body
	latestTag string
	latest404 bool
	wantToken string // when set, requests without this bearer token get 404

	tarballHits int32
	latestHits  int32
	seenAuth    atomic.Value
}

func (g *fakeGitHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.seenAuth.Store(r.Header.Get("Authorization"))
		if g.wantToken != "" && r.Header.Get("Authorization") != "Bearer "+g.wantToken {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/releases/latest"):
			atomic.AddInt32(&g.latestHits, 1)
			if g.latest404 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"` + g.latestTag + `"}`))
		case strings.Contains(r.URL.Path, "/tarball/"):
			atomic.AddInt32(&g.tarballHits, 1)
			ref := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			body, ok := g.tarballs[ref]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/x-gzip")
			_, _ = w.Write(body)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func newFetcher(t *testing.T, g *fakeGitHub) (*Fetcher, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(g.handler())
	t.Cleanup(srv.Close)
	return &Fetcher{
		Client:   srv.Client(),
		BaseURL:  srv.URL,
		CacheDir: t.TempDir(),
		TTL:      time.Hour,
	}, srv
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("reading %s: %v", p, err)
	}
	return string(data)
}

// --- ParseGitHub ---

func TestParseGitHub(t *testing.T) {
	cases := []struct {
		in      string
		want    Ref
		wantErr bool
	}{
		{"github://SteerSpec/strspc-rules@v1.6.0/rules/core", Ref{"SteerSpec", "strspc-rules", "v1.6.0", "rules/core"}, false},
		{"github://SteerSpec/strspc-rules@v1.6.0/rules/core/", Ref{"SteerSpec", "strspc-rules", "v1.6.0", "rules/core"}, false},
		{"github://o/r@abc123", Ref{"o", "r", "abc123", ""}, false},
		{"github://o/r@latest/rules", Ref{"o", "r", "latest", "rules"}, false},
		{"github://o/r@v1/./rules//core", Ref{"o", "r", "v1", "rules/core"}, false},
		{"github://o/r", Ref{}, true},           // missing @ref
		{"github://o/r/rules", Ref{}, true},     // missing @ref with path
		{"github://o/r@", Ref{}, true},          // empty ref
		{"github://o/@v1", Ref{}, true},         // empty repo
		{"github:///r@v1", Ref{}, true},         // empty owner
		{"github://o", Ref{}, true},             // no repo
		{"github://o/r@v1/../etc", Ref{}, true}, // traversal
		{"github://o/r@v1/rules/../..", Ref{}, true},
		{"https://github.com/o/r", Ref{}, true}, // wrong scheme
		{"github://o/r@v1/ru les", Ref{}, true}, // whitespace
		{"github://o/r@v1\\x", Ref{}, true},     // backslash
	}
	for _, tc := range cases {
		got, err := ParseGitHub(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseGitHub(%q) = %+v, want error", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseGitHub(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("ParseGitHub(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

func TestRefString_Normalises(t *testing.T) {
	r, err := ParseGitHub("github://o/r@v1/rules/core/")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.String(); got != "github://o/r@v1/rules/core" {
		t.Errorf("String() = %q", got)
	}
	if !r.IsImmutable() {
		t.Error("tag ref should be immutable")
	}
	if (Ref{Owner: "o", Repo: "r", Ref: "latest"}).IsImmutable() {
		t.Error("latest should be mutable")
	}
}

// --- EnsureDir ---

func rulesTarball(t *testing.T, prefix string) []byte {
	return buildTarball(t, prefix, []tarEntry{
		{name: "README.md", body: "ignored"},
		{name: "rules", dir: true},
		{name: "rules/core", dir: true},
		{name: "rules/core/realm.json", body: `{"realm":{"id":"x"}}`},
		{name: "rules/core/ENT.json", body: `{"entity":{"id":"ENT"}}`},
		{name: "rules/core/sub/SUB.json", body: `{"entity":{"id":"SUB"}}`},
		{name: "rules/other/OTH.json", body: "nope"},
		{name: "rules/core/link.json", link: "../../README.md"},
	})
}

func TestEnsureDir_HappyPath(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1.0.0": rulesTarball(t, "o-r-deadbeef")}}
	f, _ := newFetcher(t, g)

	ref := Ref{Owner: "o", Repo: "r", Ref: "v1.0.0", Path: "rules/core"}
	dir, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}

	wantDir := filepath.Join(f.CacheDir, "github", "o", "r", "v1.0.0", pathKey("rules/core"))
	if abs, _ := filepath.Abs(wantDir); dir != abs {
		t.Errorf("dir = %s, want %s", dir, abs)
	}
	if got := readFile(t, filepath.Join(dir, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
		t.Errorf("ENT.json = %q", got)
	}
	if got := readFile(t, filepath.Join(dir, "sub", "SUB.json")); got != `{"entity":{"id":"SUB"}}` {
		t.Errorf("sub/SUB.json = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "realm.json")); err != nil {
		t.Errorf("realm.json missing: %v", err)
	}
	// Entries outside the requested path and symlinks must not be extracted.
	if _, err := os.Stat(filepath.Join(dir, "OTH.json")); !os.IsNotExist(err) {
		t.Error("OTH.json leaked into cache dir")
	}
	if _, err := os.Lstat(filepath.Join(dir, "link.json")); !os.IsNotExist(err) {
		t.Error("symlink was materialised")
	}
	// No temp dirs left behind.
	entries, _ := os.ReadDir(filepath.Dir(dir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("stale temp dir %s", e.Name())
		}
	}
	if auth, _ := g.seenAuth.Load().(string); auth != "" {
		t.Errorf("unexpected Authorization header %q without token", auth)
	}
}

func TestEnsureDir_RootPath(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)

	dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "README.md")); got != "ignored" {
		t.Errorf("README.md = %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "rules", "core", "ENT.json")); err != nil {
		t.Errorf("nested file missing: %v", err)
	}
}

func TestEnsureDir_ImmutableRefCachedWithoutTTL(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	f.TTL = time.Nanosecond
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}

	first, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	second, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("dirs differ: %s vs %s", first, second)
	}
	if got := atomic.LoadInt32(&g.tarballHits); got != 1 {
		t.Errorf("tarball fetched %d times, want 1", got)
	}

	// Force re-downloads even for immutable refs.
	f.Force = true
	if _, err := f.EnsureDir(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&g.tarballHits); got != 2 {
		t.Errorf("tarball fetched %d times after Force, want 2", got)
	}
	if got := readFile(t, filepath.Join(second, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
		t.Errorf("content after Force = %q", got)
	}
}

func TestEnsureDir_LatestResolvesAndHonoursTTL(t *testing.T) {
	g := &fakeGitHub{
		t:         t,
		latestTag: "v1.0.0",
		tarballs: map[string][]byte{
			"v1.0.0": rulesTarball(t, "o-r-1"),
			"v2.0.0": buildTarball(t, "o-r-2", []tarEntry{{name: "rules/core/ENT.json", body: "v2"}}),
		},
	}
	f, _ := newFetcher(t, g)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	f.TTL = time.Hour
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}

	dir, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, string(filepath.Separator)+"v1.0.0"+string(filepath.Separator)) {
		t.Errorf("latest should resolve into the tag dir, got %s", dir)
	}
	if got := atomic.LoadInt32(&g.latestHits); got != 1 {
		t.Fatalf("latest hits = %d, want 1", got)
	}

	// Within TTL: pointer reused, no API call, even if upstream moved.
	g.latestTag = "v2.0.0"
	now = now.Add(30 * time.Minute)
	dir2, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if dir2 != dir {
		t.Errorf("within TTL dir changed: %s", dir2)
	}
	if got := atomic.LoadInt32(&g.latestHits); got != 1 {
		t.Errorf("latest hits within TTL = %d, want 1", got)
	}

	// After TTL: re-resolved to the new tag.
	now = now.Add(time.Hour)
	dir3, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir3, "v2.0.0") {
		t.Errorf("after TTL expected v2.0.0 dir, got %s", dir3)
	}
	if got := readFile(t, filepath.Join(dir3, "ENT.json")); got != "v2" {
		t.Errorf("ENT.json = %q", got)
	}
	if got := atomic.LoadInt32(&g.latestHits); got != 2 {
		t.Errorf("latest hits after TTL = %d, want 2", got)
	}
}

func TestEnsureDir_LatestOfflineFallsBackToStalePointer(t *testing.T) {
	g := &fakeGitHub{t: t, latestTag: "v1.0.0", tarballs: map[string][]byte{"v1.0.0": rulesTarball(t, "o-r-1")}}
	f, srv := newFetcher(t, g)
	now := time.Now()
	f.now = func() time.Time { return now }
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}

	if _, err := f.EnsureDir(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	now = now.Add(48 * time.Hour)
	dir, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatalf("expected stale fallback, got %v", err)
	}
	if !strings.Contains(dir, "v1.0.0") {
		t.Errorf("dir = %s", dir)
	}
}

func TestEnsureDir_LatestNoReleases(t *testing.T) {
	g := &fakeGitHub{t: t, latest404: true}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestEnsureDir_PathTraversalRejected(t *testing.T) {
	evil := buildTarball(t, "o-r-1", []tarEntry{
		{name: "rules/core/ok.json", body: "{}"},
		{name: "rules/core/../../../escape.json", body: "pwned"},
	})
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": evil}}
	f, _ := newFetcher(t, g)

	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"})
	if err == nil || !strings.Contains(err.Error(), "escapes") {
		t.Fatalf("expected zip-slip error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(f.CacheDir, "escape.json")); !os.IsNotExist(statErr) {
		t.Error("traversal entry was written")
	}
	// Nothing must be left in the cache for the failed ref.
	refDir := filepath.Join(f.CacheDir, "github", "o", "r", "v1")
	entries, _ := os.ReadDir(refDir)
	if len(entries) != 0 {
		t.Errorf("expected empty ref dir after failure, got %d entries", len(entries))
	}
}

func TestEnsureDir_AbsoluteEntryRejected(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := "x"
	_ = tw.WriteHeader(&tar.Header{Name: "o-r-1//etc/passwd", Typeflag: tar.TypeReg, Size: int64(len(body)), Mode: 0o644})
	_, _ = tw.Write([]byte(body))
	_ = tw.Close()
	_ = gz.Close()

	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": buf.Bytes()}}
	f, _ := newFetcher(t, g)
	dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	// "//etc/passwd" is cleaned to "etc/passwd" inside the cache dir.
	if _, err := os.Stat(filepath.Join(dir, "etc", "passwd")); err != nil {
		t.Errorf("expected cleaned entry inside cache dir: %v", err)
	}
}

func TestEnsureDir_RejectsUnsafeRefComponents(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	bad := []Ref{
		{Owner: "..", Repo: "r", Ref: "v1"},
		{Owner: "o", Repo: "../x", Ref: "v1"},
		{Owner: "o", Repo: "r", Ref: "v1/../.."},
		{Owner: "o", Repo: "r", Ref: "v1", Path: "../etc"},
	}
	for _, ref := range bad {
		if _, err := f.EnsureDir(context.Background(), ref); err == nil {
			t.Errorf("EnsureDir(%+v) should fail", ref)
		}
	}
}

func TestEnsureDir_SizeCapExceeded(t *testing.T) {
	// Fake a huge size via a synthetic header: the tarball lies about the
	// entry size so we don't need to materialise 50 MB in the test.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "o-r-1/big.bin", Typeflag: tar.TypeReg, Size: MaxExtractBytes + 1, Mode: 0o644})
	// Write nothing; tar.Writer.Close will complain but the header is flushed.
	_ = tw.Flush()
	_ = gz.Close()

	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": buf.Bytes()}}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
	if err == nil || !strings.Contains(err.Error(), "extraction cap") {
		t.Fatalf("expected cap error, got %v", err)
	}
}

func TestEnsureDir_SizeCapCumulative(t *testing.T) {
	// Many small files whose sum crosses the cap.
	chunk := strings.Repeat("a", 1<<20) // 1 MB
	entries := make([]tarEntry, 0, 51)
	for i := 0; i < 51; i++ {
		entries = append(entries, tarEntry{name: "f" + string(rune('a'+i%26)) + string(rune('a'+i/26)) + ".bin", body: chunk})
	}
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": buildTarball(t, "o-r-1", entries)}}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
	if err == nil || !strings.Contains(err.Error(), "extraction cap") {
		t.Fatalf("expected cumulative cap error, got %v", err)
	}
}

func TestEnsureDir_PathNotInTarball(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "does/not/exist"})
	if err == nil || !strings.Contains(err.Error(), "not found in") {
		t.Fatalf("expected path-not-found error, got %v", err)
	}
}

func TestEnsureDir_404WithoutToken(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{}}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "private", Ref: "v1"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "private repo or missing token") {
		t.Errorf("error should hint at missing token, got %q", err)
	}
}

func TestEnsureDir_404WithToken(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{}}
	f, _ := newFetcher(t, g)
	f.Token = "ghp_test"
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "nope"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if strings.Contains(err.Error(), "missing token") {
		t.Errorf("error should not blame the token when one is set, got %q", err)
	}
	if auth, _ := g.seenAuth.Load().(string); auth != "Bearer ghp_test" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestEnsureDir_TokenUnlocksPrivateRepo(t *testing.T) {
	g := &fakeGitHub{t: t, wantToken: "secret", tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}

	if _, err := f.EnsureDir(context.Background(), ref); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected 404 without token, got %v", err)
	}
	f.Token = "secret"
	if _, err := f.EnsureDir(context.Background(), ref); err != nil {
		t.Fatalf("expected success with token, got %v", err)
	}
}

func TestEnsureDir_RequiresCacheDir(t *testing.T) {
	f := &Fetcher{}
	if _, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"}); err == nil {
		t.Fatal("expected error without CacheDir")
	}
}

func TestEnsureDir_ContextCancelled(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.EnsureDir(ctx, Ref{Owner: "o", Repo: "r", Ref: "v1"}); err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestTokenFromEnv(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	if got := TokenFromEnv(); got != "" {
		t.Errorf("empty env → %q", got)
	}
	t.Setenv("GH_TOKEN", "gh")
	if got := TokenFromEnv(); got != "gh" {
		t.Errorf("GH_TOKEN → %q", got)
	}
	t.Setenv("GITHUB_TOKEN", "github")
	if got := TokenFromEnv(); got != "github" {
		t.Errorf("GITHUB_TOKEN should win → %q", got)
	}
}

func TestEnsureDir_EscapesRefInURL(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1#build": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1#build", Path: "rules/core"}

	dir, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
		t.Errorf("ENT.json = %q", got)
	}
	if got := atomic.LoadInt32(&g.tarballHits); got != 1 {
		t.Errorf("tarball fetched %d times, want 1 (the ref must reach the server intact)", got)
	}
}

func TestEnsureDir_ForceLeavesNoStagingDirs(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}

	dir, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatal(err)
	}
	f.Force = true
	if _, err := f.EnsureDir(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".old-") || strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("staging dir %q left behind after Force", e.Name())
		}
	}
	if got := readFile(t, filepath.Join(dir, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
		t.Errorf("ENT.json after Force = %q", got)
	}
}

func TestEnsureDir_TagNamedLatestJSONDoesNotCollideWithPointer(t *testing.T) {
	// A release tag literally named "latest.json" must coexist with the
	// @latest pointer file, in both directions.
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"latest.json": rulesTarball(t, "o-r-1")}, latestTag: "latest.json"}
	f, _ := newFetcher(t, g)

	// @latest resolves to the tag "latest.json": pointer written, then download.
	dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"})
	if err != nil {
		t.Fatalf("EnsureDir(@latest → latest.json): %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
		t.Errorf("ENT.json = %q", got)
	}
	if _, err := os.Stat(filepath.Join(f.CacheDir, "github", "o", "r", metaDir, latestPointer)); err != nil {
		t.Errorf("pointer not written under %s: %v", metaDir, err)
	}

	// Pinning @latest.json while the pointer exists must hit the same cache.
	pinned, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "latest.json", Path: "rules/core"})
	if err != nil {
		t.Fatalf("EnsureDir(@latest.json): %v", err)
	}
	if pinned != dir {
		t.Errorf("pinned dir %s differs from resolved dir %s", pinned, dir)
	}
	if got := atomic.LoadInt32(&g.tarballHits); got != 1 {
		t.Errorf("tarball fetched %d times, want 1", got)
	}
}
