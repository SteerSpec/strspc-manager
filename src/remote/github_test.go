package remote

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
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

func TestEnsureDir_TokenOwners(t *testing.T) {
	tests := []struct {
		name     string
		owners   []string
		owner    string
		wantAuth string
	}{
		{"nil sends to every owner", nil, "o", "Bearer secret"},
		{"listed owner", []string{"SteerSpec", "o"}, "o", "Bearer secret"},
		{"owner case is ignored", []string{"SteerSpec"}, "steerspec", "Bearer secret"},
		{"unlisted owner", []string{"SteerSpec"}, "o", ""},
		{"empty list sends to nobody", []string{}, "o", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
			f, _ := newFetcher(t, g)
			f.Token = "secret"
			f.TokenOwners = tt.owners
			ref := Ref{Owner: tt.owner, Repo: "r", Ref: "v1", Path: "rules/core"}
			if _, err := f.EnsureDir(context.Background(), ref); err != nil {
				t.Fatal(err)
			}
			if auth, _ := g.seenAuth.Load().(string); auth != tt.wantAuth {
				t.Errorf("Authorization = %q, want %q", auth, tt.wantAuth)
			}
		})
	}
}

func TestEnsureDir_TokenOwnersCoverReleaseLookup(t *testing.T) {
	g := &fakeGitHub{t: t, latest404: true}
	f, _ := newFetcher(t, g)
	f.Token = "secret"
	f.TokenOwners = []string{"SteerSpec"}
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if auth, _ := g.seenAuth.Load().(string); auth != "" {
		t.Errorf("Authorization = %q, want none", auth)
	}
}

func TestEnsureDir_404NamesTokenOwnersEnv(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{}}
	f, _ := newFetcher(t, g)
	f.Token = "secret"
	f.TokenOwners = []string{"SteerSpec"}
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "private", Ref: "v1"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), TokenOwnersEnv) {
		t.Errorf("error should name %s, got %q", TokenOwnersEnv, err)
	}
}

func TestEnsureDir_RateLimitNamesTokenOwnersEnv(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)
	f := &Fetcher{Client: srv.Client(), BaseURL: srv.URL, CacheDir: t.TempDir(), Token: "secret", TokenOwners: []string{"SteerSpec"}}
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
	if err == nil || !strings.Contains(err.Error(), TokenOwnersEnv) {
		t.Errorf("error should name %s, got %v", TokenOwnersEnv, err)
	}
}

func TestTokenOwnersFromEnv(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY_OWNER", "")
	t.Setenv(TokenOwnersEnv, "")
	if got := TokenOwnersFromEnv(); len(got) != 1 || got[0] != "SteerSpec" {
		t.Errorf("empty env → %q", got)
	}
	t.Setenv("GITHUB_REPOSITORY_OWNER", "acme")
	t.Setenv(TokenOwnersEnv, "foo, bar,,")
	want := []string{"SteerSpec", "acme", "foo", "bar"}
	if got := TokenOwnersFromEnv(); !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestTokenForOwner(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "secret")
	t.Setenv("GITHUB_REPOSITORY_OWNER", "")
	t.Setenv(TokenOwnersEnv, "acme")
	if got := TokenForOwner("ACME"); got != "secret" {
		t.Errorf("listed owner → %q", got)
	}
	if got := TokenForOwner("other"); got != "" {
		t.Errorf("unlisted owner → %q", got)
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

func TestEnsureDir_LatestCancelledContextDoesNotUseStalePointer(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}, latestTag: "v1"}
	f, _ := newFetcher(t, g)
	f.TTL = time.Nanosecond
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}

	if _, err := f.EnsureDir(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // pointer is now stale

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := f.EnsureDir(ctx, ref)
	if err == nil {
		t.Fatal("expected an error on a cancelled context, got success from the stale pointer")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v, want context.Canceled", err)
	}
}

func TestEnsureDir_LatestTagMustBeSinglePathComponent(t *testing.T) {
	// A tag such as "v1/<cache-key>/injected" is a legal Git ref but would
	// land inside the pinned @v1 cache entry. Both the API answer and a
	// poisoned pointer must be rejected.
	injected := "v1/" + pathKey("rules/core") + "/injected"
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{injected: rulesTarball(t, "o-r-1")}, latestTag: injected}
	f, _ := newFetcher(t, g)
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}

	if _, err := f.EnsureDir(context.Background(), ref); err == nil {
		t.Fatal("expected an error for a multi-component latest tag from the API")
	}
	if got := atomic.LoadInt32(&g.tarballHits); got != 0 {
		t.Errorf("tarball fetched %d times, want 0", got)
	}

	// Poisoned pointer on disk: ignored, then the API answer is rejected too.
	pointer := filepath.Join(f.CacheDir, "github", "o", "r", metaDir, latestPointer)
	if err := os.MkdirAll(filepath.Dir(pointer), 0o755); err != nil {
		t.Fatal(err)
	}
	rec, _ := json.Marshal(latestRecord{Tag: injected, ResolvedAt: time.Now()})
	if err := os.WriteFile(pointer, rec, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.EnsureDir(context.Background(), ref); err == nil {
		t.Fatal("expected an error with a poisoned pointer")
	}
	if _, err := os.Stat(filepath.Join(f.CacheDir, "github", "o", "r", "v1")); !os.IsNotExist(err) {
		t.Errorf("v1 cache entry must not exist, stat err = %v", err)
	}
}

func TestParseGitHub_ErrorReasons(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr string
	}{
		{"second @ in ref", "github://o/r@a@b", `illegal component "a@b"`},
		{"colon in owner", "github://o:x/r@v1", `illegal component "o:x"`},
		{"dot-dot owner", "github://../r@v1", `illegal component ".."`},
		{"dot repo", "github://o/.@v1", `illegal component "."`},
		{"dot-dot ref", "github://o/r@..", `illegal component ".."`},
		{"missing ref", "github://o/r/rules", "missing @ref"},
		{"empty ref", "github://o/r@", "owner, repo and ref must be non-empty"},
		{"traversal in path", "github://o/r@v1/a/../b", `path must not contain ".."`},
		{"wrong scheme", "https://github.com/o/r", "must start with github://"},
		{"backslash", `github://o/r@v1\x`, "illegal character"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGitHub(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ParseGitHub(%q) = %+v, %v; want error containing %q", tt.in, got, err, tt.wantErr)
			}
		})
	}
}

// --- Crafted archives ---

// rawEntry is a tar entry written exactly as given, for archives buildTarball
// cannot express: no implied prefix, arbitrary type flags, hostile names.
type rawEntry struct {
	hdr   tar.Header
	body  string
	zeros int64 // zero bytes appended to body, so cap tests stay out of memory
}

func regEntry(name, body string) rawEntry {
	return rawEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, body: body}
}

func zeroEntry(name string, size int64) rawEntry {
	return rawEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, zeros: size}
}

func dirEntry(name string) rawEntry {
	return rawEntry{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o755}}
}

func linkEntry(name string, typeflag byte, target string) rawEntry {
	return rawEntry{hdr: tar.Header{Name: name, Typeflag: typeflag, Linkname: target, Mode: 0o777}}
}

func rawTarball(t *testing.T, entries ...rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	chunk := make([]byte, 1<<20)
	for _, e := range entries {
		hdr := e.hdr
		if hdr.Typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.body)) + e.zeros
		}
		if err := tw.WriteHeader(&hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
		for left := e.zeros; left > 0; {
			n := min(left, int64(len(chunk)))
			if _, err := tw.Write(chunk[:n]); err != nil {
				t.Fatal(err)
			}
			left -= n
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

func gzipped(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// treeOf maps every file below root to its contents, keyed by slash path. A
// symlink or other non-regular entry fails the test: extraction never makes one.
func treeOf(t *testing.T, root string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			t.Errorf("non-regular entry %s (%v)", p, d.Type())
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func assertNoStagingDirs(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") || strings.HasPrefix(e.Name(), ".old-") {
			t.Errorf("staging dir %q left in %s", e.Name(), dir)
		}
	}
}

func skipWithoutUnixPerms(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 || runtime.GOOS == "windows" {
		t.Skip("needs Unix permissions enforced: not on Windows or as root")
	}
}

func TestExtractTarball_RejectsEscapingEntries(t *testing.T) {
	deep := "p/" + strings.Repeat("deep/", 30) // long enough to need a PAX path record
	tests := []struct {
		name    string
		entry   rawEntry
		sub     string
		wantErr string
	}{
		{"file above the archive", regEntry("../escape", "x"), "", `tar: entry "../escape" escapes extraction root`},
		{"file after the prefix", regEntry("p/../../escape", "x"), "", `tar: entry "p/../../escape" escapes extraction root`},
		{"file inside the sub path", regEntry("p/rules/../../../escape", "x"), "rules", "escapes extraction root"},
		{"file outside the sub path", regEntry("p/other/../../escape", "x"), "rules", "escapes extraction root"},
		{"directory", dirEntry("p/../../escape/"), "", "escapes extraction root"},
		{"symlink", linkEntry("p/../../escape", tar.TypeSymlink, "/etc/passwd"), "", "escapes extraction root"},
		{"long name in a PAX record", regEntry(deep+"../../escape", "x"), "", "escapes extraction root"},
		{"backslash traversal in a file", regEntry(`p/..\..\escape`, "x"), "", `tar: illegal entry name "..\\..\\escape"`},
		{"backslash in a directory", dirEntry(`p/a\b/`), "", `tar: illegal entry name "a\\b"`},
		{"file named dot", regEntry("p/.", "x"), "", `tar: illegal entry name "."`},
		{"directory named dot", dirEntry("p/./"), "", `tar: illegal entry name "."`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			dest := filepath.Join(root, "a", "b", "dest")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			n, err := extractTarball(bytes.NewReader(rawTarball(t, tt.entry)), tt.sub, dest)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if n != 0 {
				t.Errorf("files written = %d, want 0", n)
			}
			if got := treeOf(t, root); len(got) != 0 {
				t.Errorf("rejected entry left files behind: %v", got)
			}
		})
	}
}

func TestExtractTarball_SkipsSpecialEntries(t *testing.T) {
	tests := []struct {
		name  string
		entry rawEntry
	}{
		{"absolute symlink", linkEntry("p/rules/evil", tar.TypeSymlink, "/etc/passwd")},
		{"relative symlink", linkEntry("p/rules/evil", tar.TypeSymlink, "../../../../escape")},
		{"hard link", linkEntry("p/rules/evil", tar.TypeLink, "/etc/passwd")},
		{"character device", rawEntry{hdr: tar.Header{Name: "p/rules/evil", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3}}},
		{"block device", rawEntry{hdr: tar.Header{Name: "p/rules/evil", Typeflag: tar.TypeBlock, Devmajor: 8}}},
		{"fifo", rawEntry{hdr: tar.Header{Name: "p/rules/evil", Typeflag: tar.TypeFifo}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			archive := rawTarball(t, tt.entry, regEntry("p/rules/ok.json", "{}"))
			n, err := extractTarball(bytes.NewReader(archive), "rules", dest)
			if err != nil {
				t.Fatalf("extractTarball: %v", err)
			}
			if n != 1 {
				t.Errorf("files written = %d, want 1", n)
			}
			if _, err := os.Lstat(filepath.Join(dest, "evil")); !os.IsNotExist(err) {
				t.Errorf("special entry was materialised, Lstat err = %v", err)
			}
			if got, want := treeOf(t, dest), map[string]string{"ok.json": "{}"}; !maps.Equal(got, want) {
				t.Errorf("tree = %v, want %v", got, want)
			}
		})
	}
}

// A link entry must not become a path a later entry writes through.
func TestExtractTarball_LinksCannotRedirectWrites(t *testing.T) {
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret")
	if err := os.WriteFile(secret, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		link     rawEntry
		fileName string
		wantPath string
	}{
		{"absolute directory symlink", linkEntry("p/evil", tar.TypeSymlink, outside), "p/evil/pwned", "evil/pwned"},
		{"relative directory symlink", linkEntry("p/evil", tar.TypeSymlink, "../../.."), "p/evil/pwned", "evil/pwned"},
		{"file symlink", linkEntry("p/evil", tar.TypeSymlink, secret), "p/evil", "evil"},
		{"hard link", linkEntry("p/evil", tar.TypeLink, secret), "p/evil", "evil"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			archive := rawTarball(t, tt.link, regEntry(tt.fileName, "pwned"))
			if _, err := extractTarball(bytes.NewReader(archive), "", dest); err != nil {
				t.Fatalf("extractTarball: %v", err)
			}
			if got, want := treeOf(t, dest), map[string]string{tt.wantPath: "pwned"}; !maps.Equal(got, want) {
				t.Errorf("tree = %v, want %v", got, want)
			}
			if got, want := treeOf(t, outside), map[string]string{"secret": "original"}; !maps.Equal(got, want) {
				t.Errorf("outside dir = %v, want %v", got, want)
			}
		})
	}
}

func TestExtractTarball_SelectsEntries(t *testing.T) {
	globalHeader := rawEntry{hdr: tar.Header{
		Name:       "pax_global_header",
		Typeflag:   tar.TypeXGlobalHeader,
		PAXRecords: map[string]string{"comment": "deadbeef"},
	}}
	tests := []struct {
		name    string
		entries []rawEntry
		sub     string
		wantN   int
		want    map[string]string
	}{
		{
			name:    "GitHub global header at the root",
			entries: []rawEntry{globalHeader, regEntry("p/a.json", "x")},
			wantN:   1,
			want:    map[string]string{"a.json": "x"},
		},
		{
			name:    "GitHub global header with a sub path",
			entries: []rawEntry{globalHeader, regEntry("p/rules/a.json", "x")},
			sub:     "rules",
			wantN:   1,
			want:    map[string]string{"a.json": "x"},
		},
		{
			name:    "file beside the top-level directory is dropped",
			entries: []rawEntry{regEntry("stray", "no"), regEntry("p/a.json", "x")},
			wantN:   1,
			want:    map[string]string{"a.json": "x"},
		},
		{
			name:    "requested path is a file",
			entries: []rawEntry{regEntry("p/rules", "x")},
			sub:     "rules",
			want:    map[string]string{},
		},
		{
			name:    "leading dot-slash",
			entries: []rawEntry{regEntry("./p/rules/a.json", "x")},
			sub:     "rules",
			wantN:   1,
			want:    map[string]string{"a.json": "x"},
		},
		{
			name:    "absolute name stays inside",
			entries: []rawEntry{regEntry("/etc/passwd", "x")},
			wantN:   1,
			want:    map[string]string{"etc/passwd": "x"},
		},
		{
			name:    "sibling sharing the sub path as a prefix",
			entries: []rawEntry{regEntry("p/rules-old/a.json", "no"), regEntry("p/rules/b.json", "x")},
			sub:     "rules",
			wantN:   1,
			want:    map[string]string{"b.json": "x"},
		},
		{
			name:    "duplicate name keeps the last body",
			entries: []rawEntry{regEntry("p/a.json", "first"), regEntry("p/a.json", "last")},
			wantN:   2,
			want:    map[string]string{"a.json": "last"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			n, err := extractTarball(bytes.NewReader(rawTarball(t, tt.entries...)), tt.sub, dest)
			if err != nil {
				t.Fatalf("extractTarball: %v", err)
			}
			if n != tt.wantN {
				t.Errorf("files written = %d, want %d", n, tt.wantN)
			}
			if got := treeOf(t, dest); !maps.Equal(got, tt.want) {
				t.Errorf("tree = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractTarball_IgnoresHeaderMode(t *testing.T) {
	entry := regEntry("p/run.sh", "#!/bin/sh")
	entry.hdr.Mode = 0o6777
	dest := t.TempDir()
	if _, err := extractTarball(bytes.NewReader(rawTarball(t, entry)), "", dest); err != nil {
		t.Fatalf("extractTarball: %v", err)
	}
	info, err := os.Stat(filepath.Join(dest, "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if bad := info.Mode() & (os.ModeSetuid | os.ModeSetgid | 0o111); bad != 0 {
		t.Errorf("mode = %v keeps %v from the tar header", info.Mode(), bad)
	}
}

func TestExtractTarball_SizeCapBoundary(t *testing.T) {
	const capErr = "archive exceeds 52428800 bytes extraction cap"
	tests := []struct {
		name    string
		entries []rawEntry
		sub     string
		wantN   int
		wantErr string
		sizes   map[string]int64 // files expected in dest, nothing else
	}{
		{
			name:    "one file exactly at the cap",
			entries: []rawEntry{zeroEntry("p/a", MaxExtractBytes)},
			wantN:   1,
			sizes:   map[string]int64{"a": MaxExtractBytes},
		},
		{
			name:    "one honest file over the cap",
			entries: []rawEntry{zeroEntry("p/a", MaxExtractBytes+1)},
			wantErr: capErr,
			sizes:   map[string]int64{},
		},
		{
			name:    "second file crosses the cap",
			entries: []rawEntry{zeroEntry("p/a", MaxExtractBytes), zeroEntry("p/b", 1)},
			wantN:   1,
			wantErr: capErr,
			sizes:   map[string]int64{"a": MaxExtractBytes},
		},
		{
			name:    "entries outside the sub path do not count",
			entries: []rawEntry{zeroEntry("p/other/big", MaxExtractBytes+1), regEntry("p/rules/a", "x")},
			sub:     "rules",
			wantN:   1,
			sizes:   map[string]int64{"a": 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			n, err := extractTarball(bytes.NewReader(rawTarball(t, tt.entries...)), tt.sub, dest)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("extractTarball: %v", err)
			}
			if tt.wantErr != "" && (err == nil || err.Error() != tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
			if n != tt.wantN {
				t.Errorf("files written = %d, want %d", n, tt.wantN)
			}
			entries, err := os.ReadDir(dest)
			if err != nil {
				t.Fatal(err)
			}
			got := map[string]int64{}
			for _, e := range entries {
				info, err := e.Info()
				if err != nil {
					t.Fatal(err)
				}
				got[e.Name()] = info.Size()
			}
			if !maps.Equal(got, tt.sizes) {
				t.Errorf("sizes = %v, want %v", got, tt.sizes)
			}
		})
	}
}

func TestExtractTarball_RejectsCorruptStreams(t *testing.T) {
	valid := rawTarball(t, regEntry("p/a.json", strings.Repeat("rule ", 200)))

	// A header promising 10 bytes followed by 3, inside a well-formed gzip stream.
	var short bytes.Buffer
	gz := gzip.NewWriter(&short)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "p/a.json", Typeflag: tar.TypeReg, Size: 10, Mode: 0o644}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{"not gzip", []byte("<html>rate limited</html>"), "gzip: gzip: invalid header"},
		{"empty body", nil, "gzip: EOF"},
		{"gzip of garbage", gzipped(t, bytes.Repeat([]byte("x"), 1024)), "tar: archive/tar: invalid tar header"},
		{"gzip cut short", valid[:len(valid)/2], "unexpected EOF"},
		{"entry shorter than its header", short.Bytes(), "unexpected EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			n, err := extractTarball(bytes.NewReader(tt.data), "", t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if n != 0 {
				t.Errorf("files written = %d, want 0", n)
			}
		})
	}
}

func TestExtractTarball_PathCollisions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("asserts Unix errno values")
	}
	tests := []struct {
		name    string
		entries []rawEntry
		wantErr error
	}{
		{"directory over a file", []rawEntry{regEntry("p/a", "x"), dirEntry("p/a/")}, syscall.ENOTDIR},
		{"directory below a file", []rawEntry{regEntry("p/a", "x"), dirEntry("p/a/b/")}, syscall.ENOTDIR},
		{"file below a file", []rawEntry{regEntry("p/a", "x"), regEntry("p/a/b", "y")}, syscall.ENOTDIR},
		{"file over a directory", []rawEntry{dirEntry("p/a/"), regEntry("p/a", "x")}, syscall.EISDIR},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := extractTarball(bytes.NewReader(rawTarball(t, tt.entries...)), "", t.TempDir())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestStripPrefix(t *testing.T) {
	tests := []struct {
		name, entry, sub string
		wantRel          string
		wantOK           bool
	}{
		{"bare top-level name at root", "p", "", "", true},
		{"bare top-level name with sub", "p", "rules", "", false},
		{"top-level directory at root", "p/", "", "", true},
		{"top-level directory with sub", "p/", "rules", "", false},
		{"file at root", "p/a/b.json", "", "a/b.json", true},
		{"directory at root", "p/a/", "", "a", true},
		{"dot-slash prefix", "./p/rules/a.json", "rules", "a.json", true},
		{"the sub directory itself", "p/rules/", "rules", "", true},
		{"file below a nested sub", "p/rules/core/a.json", "rules/core", "a.json", true},
		{"outside sub", "p/other/a.json", "rules", "", false},
		{"shared name prefix is not containment", "p/rules-old/a.json", "rules", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel, ok := stripPrefix(tt.entry, tt.sub)
			if rel != tt.wantRel || ok != tt.wantOK {
				t.Errorf("stripPrefix(%q, %q) = %q, %v; want %q, %v", tt.entry, tt.sub, rel, ok, tt.wantRel, tt.wantOK)
			}
		})
	}
}

func TestSafeJoin(t *testing.T) {
	base := filepath.Join(string(filepath.Separator), "cache", "dest")
	tests := []struct {
		name    string
		rel     string
		want    string
		wantErr string
	}{
		{name: "plain file", rel: "a.json", want: filepath.Join(base, "a.json")},
		{name: "nested file", rel: "a/b.json", want: filepath.Join(base, "a", "b.json")},
		{name: "absolute is rebased", rel: "/etc/passwd", want: filepath.Join(base, "etc", "passwd")},
		{name: "doubled and dot segments", rel: ".//a/./b", want: filepath.Join(base, "a", "b")},
		{name: "dots inside a name", rel: "..a/b..", want: filepath.Join(base, "..a", "b..")},
		{name: "empty", rel: "", wantErr: `tar: illegal entry name ""`},
		{name: "dot", rel: ".", wantErr: `tar: illegal entry name "."`},
		{name: "root", rel: "/", wantErr: `tar: illegal entry name "/"`},
		{name: "backslash", rel: `a\b`, wantErr: `tar: illegal entry name "a\\b"`},
		{name: "backslash traversal", rel: `..\..\x`, wantErr: `tar: illegal entry name "..\\..\\x"`},
		{name: "dot-dot", rel: "..", wantErr: `tar: entry ".." escapes extraction root`},
		{name: "leading dot-dot", rel: "../x", wantErr: `tar: entry "../x" escapes extraction root`},
		{name: "dot-dot that stays inside", rel: "a/../b", wantErr: `tar: entry "a/../b" escapes extraction root`},
		{name: "trailing dot-dot", rel: "a/..", wantErr: `tar: entry "a/.." escapes extraction root`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeJoin(base, tt.rel)
			if tt.wantErr != "" {
				if err == nil || err.Error() != tt.wantErr {
					t.Fatalf("safeJoin(%q) = %q, %v; want error %q", tt.rel, got, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("safeJoin(%q): %v", tt.rel, err)
			}
			if got != tt.want {
				t.Errorf("safeJoin(%q) = %q, want %q", tt.rel, got, tt.want)
			}
		})
	}
}

func TestWriteFileAtomic(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T, p string)
		perms   bool // needs Unix permissions or errno values
		wantErr error
	}{
		{
			name:  "creates missing parents",
			setup: func(*testing.T, string) {},
		},
		{
			name: "replaces an existing file",
			setup: func(t *testing.T, p string) {
				mustMkdir(t, filepath.Dir(p))
				mustWrite(t, p, "old")
			},
		},
		{
			name:  "replaces an empty directory",
			setup: func(t *testing.T, p string) { mustMkdir(t, p) },
		},
		{
			name: "refuses a non-empty directory",
			setup: func(t *testing.T, p string) {
				mustMkdir(t, p)
				mustWrite(t, filepath.Join(p, "child"), "kept")
			},
			wantErr: fs.ErrExist,
		},
		{
			name: "parent is a file",
			setup: func(t *testing.T, p string) {
				grandparent := filepath.Dir(filepath.Dir(p))
				mustMkdir(t, filepath.Dir(grandparent))
				mustWrite(t, grandparent, "blocker")
			},
			perms:   true, // ENOTDIR is a Unix errno.
			wantErr: syscall.ENOTDIR,
		},
		{
			name: "read-only directory",
			setup: func(t *testing.T, p string) {
				mustMkdir(t, filepath.Dir(p))
				makeReadOnly(t, filepath.Dir(p))
			},
			perms:   true,
			wantErr: fs.ErrPermission,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.perms {
				skipWithoutUnixPerms(t)
			}
			p := filepath.Join(t.TempDir(), "x", "y", "latest.json")
			tt.setup(t, p)

			err := writeFileAtomic(p, []byte("new"))
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr == nil {
				if got := readFile(t, p); got != "new" {
					t.Errorf("content = %q, want %q", got, "new")
				}
			}
			entries, _ := os.ReadDir(filepath.Dir(p))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".pointer-") {
					t.Errorf("temp file %s left behind", e.Name())
				}
			}
		})
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeReadOnly drops write permission on dir until the test ends, so that
// t.TempDir can still clean up.
func makeReadOnly(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

// --- HTTP behaviour ---

func newRawFetcher(t *testing.T, h http.HandlerFunc) *Fetcher {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &Fetcher{Client: srv.Client(), BaseURL: srv.URL, CacheDir: t.TempDir(), TTL: time.Hour}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestEnsureDir_StatusErrors(t *testing.T) {
	const token = "ghp_secret"
	rateLimited := map[string]string{"X-RateLimit-Remaining": "0"}
	tests := []struct {
		name         string
		status       int
		headers      map[string]string
		token        string
		tokenOwners  []string
		want         string
		wantNotFound bool
	}{
		{
			name:         "404 without a token",
			status:       http.StatusNotFound,
			want:         "not found (private repo or missing token; set GITHUB_TOKEN)",
			wantNotFound: true,
		},
		{
			name:         "404 with the token sent",
			status:       http.StatusNotFound,
			token:        token,
			want:         "not found (check owner, repo and ref)",
			wantNotFound: true,
		},
		{
			name:         "404 with the token withheld",
			status:       http.StatusNotFound,
			token:        token,
			tokenOwners:  []string{"SteerSpec"},
			want:         `not found (token not sent to owner "o"; add it to STRSPC_TOKEN_OWNERS if this repo is private)`,
			wantNotFound: true,
		},
		{
			name:   "401",
			status: http.StatusUnauthorized,
			token:  token,
			want:   "HTTP 401 unauthorized (invalid token)",
		},
		{
			name:    "403 rate limited",
			status:  http.StatusForbidden,
			headers: rateLimited,
			want:    "HTTP 403 rate limited (set GITHUB_TOKEN to raise the limit)",
		},
		{
			name:        "403 rate limited with the token withheld",
			status:      http.StatusForbidden,
			headers:     rateLimited,
			token:       token,
			tokenOwners: []string{"SteerSpec"},
			want:        `HTTP 403 rate limited (token not sent to owner "o"; add it to STRSPC_TOKEN_OWNERS to raise the limit)`,
		},
		{
			name:    "403 with quota left",
			status:  http.StatusForbidden,
			headers: map[string]string{"X-RateLimit-Remaining": "12"},
			want:    "HTTP 403 forbidden",
		},
		{
			name:   "403 without rate limit headers",
			status: http.StatusForbidden,
			want:   "HTTP 403 forbidden",
		},
		{name: "429", status: http.StatusTooManyRequests, want: "HTTP 429"},
		{name: "500", status: http.StatusInternalServerError, want: "HTTP 500"},
		{name: "unexpected success code", status: http.StatusNoContent, want: "HTTP 204"},
	}
	endpoints := []struct {
		name string
		ref  string
		what string
	}{
		{"tarball", "v1", "downloading tarball"},
		{"latest", LatestRef, "resolving @latest"},
	}
	for _, tt := range tests {
		for _, ep := range endpoints {
			t.Run(tt.name+"/"+ep.name, func(t *testing.T) {
				f := newRawFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
					for k, v := range tt.headers {
						w.Header().Set(k, v)
					}
					w.WriteHeader(tt.status)
				})
				f.Token = tt.token
				f.TokenOwners = tt.tokenOwners

				_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: ep.ref})
				if err == nil {
					t.Fatal("expected an error")
				}
				want := "remote: " + ep.what + " for o/r@" + ep.ref + ": " + tt.want
				if err.Error() != want {
					t.Errorf("err = %q\nwant  %q", err, want)
				}
				if got := errors.Is(err, ErrNotFound); got != tt.wantNotFound {
					t.Errorf("errors.Is(err, ErrNotFound) = %v, want %v", got, tt.wantNotFound)
				}
				if _, statErr := os.Stat(filepath.Join(f.CacheDir, "github")); !os.IsNotExist(statErr) {
					t.Errorf("a failed request touched the cache, stat err = %v", statErr)
				}
			})
		}
	}
}

func TestEnsureDir_LatestRejectsBadReleaseBody(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{"not JSON", "<html>", "remote: decoding latest release for o/r: "},
		{"larger than the 1 MiB read limit", `{"x":"` + strings.Repeat("a", 2<<20) + `"}`, "remote: decoding latest release for o/r: "},
		{"no tag_name", `{}`, "remote: latest release of o/r has no tag_name"},
		{"empty tag_name", `{"tag_name":""}`, "remote: latest release of o/r has no tag_name"},
		{"tag with a slash", `{"tag_name":"release/1"}`, `has tag "release/1", which cannot be cached as a single path component`},
		{"traversal tag", `{"tag_name":".."}`, `has tag "..", which cannot be cached`},
		{"hidden tag", `{"tag_name":".meta"}`, `has tag ".meta", which cannot be cached`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newRawFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tt.body))
			})
			_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if _, statErr := os.Stat(filepath.Join(f.CacheDir, "github")); !os.IsNotExist(statErr) {
				t.Errorf("a rejected release touched the cache, stat err = %v", statErr)
			}
		})
	}
}

func pointerPath(f *Fetcher) string {
	return filepath.Join(f.CacheDir, "github", "o", "r", metaDir, latestPointer)
}

func seedPointer(t *testing.T, f *Fetcher, tag string, resolvedAt time.Time) {
	t.Helper()
	rec, err := json.Marshal(latestRecord{Tag: tag, ResolvedAt: resolvedAt})
	if err != nil {
		t.Fatal(err)
	}
	mustMkdir(t, filepath.Dir(pointerPath(f)))
	mustWrite(t, pointerPath(f), string(rec))
}

func TestEnsureDir_LatestStalePointerFallback(t *testing.T) {
	tests := []struct {
		name         string
		latest       http.HandlerFunc
		wantNotFound bool
	}{
		{
			name:   "server error falls back",
			latest: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusBadGateway) },
		},
		{
			name: "rate limit falls back",
			latest: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(http.StatusForbidden)
			},
		},
		{
			name:   "malformed answer falls back",
			latest: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
		},
		{
			name:         "404 means the releases are gone",
			latest:       func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantNotFound: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tarball := rulesTarball(t, "o-r-1")
			f := newRawFetcher(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/releases/latest") {
					tt.latest(w, r)
					return
				}
				_, _ = w.Write(tarball)
			})
			now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
			f.now = func() time.Time { return now }
			stale := now.Add(-2 * time.Hour)
			seedPointer(t, f, "v1", stale)

			dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"})
			if tt.wantNotFound {
				if !errors.Is(err, ErrNotFound) {
					t.Fatalf("err = %v, want ErrNotFound", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected the stale tag, got %v", err)
			}
			if !strings.Contains(dir, string(filepath.Separator)+"v1"+string(filepath.Separator)) {
				t.Errorf("dir = %s, want the v1 entry", dir)
			}
			// The fallback must not refresh the pointer: the next run retries.
			var rec latestRecord
			if err := json.Unmarshal([]byte(readFile(t, pointerPath(f))), &rec); err != nil {
				t.Fatal(err)
			}
			if !rec.ResolvedAt.Equal(stale) {
				t.Errorf("pointer resolved_at = %v, want it left at %v", rec.ResolvedAt, stale)
			}
		})
	}
}

// Every pointer here is inside the TTL, so only its content can disqualify it.
func TestEnsureDir_LatestIgnoresBadPointer(t *testing.T) {
	tests := []struct {
		name string
		tag  string
		raw  string // written verbatim instead of a record for tag
	}{
		{name: "not JSON", raw: "{"},
		{name: "empty tag", tag: ""},
		{name: "tag leaving the repo dir", tag: "../other"},
		{name: "tag naming the metadata dir", tag: ".meta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGitHub{t: t, latestTag: "v2", tarballs: map[string][]byte{"v2": rulesTarball(t, "o-r-2")}}
			f, _ := newFetcher(t, g)
			seedPointer(t, f, tt.tag, time.Now())
			if tt.raw != "" {
				mustWrite(t, pointerPath(f), tt.raw)
			}

			dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"})
			if err != nil {
				t.Fatalf("EnsureDir: %v", err)
			}
			if !strings.Contains(dir, string(filepath.Separator)+"v2"+string(filepath.Separator)) {
				t.Errorf("dir = %s, want the v2 entry", dir)
			}
			var rec latestRecord
			if err := json.Unmarshal([]byte(readFile(t, pointerPath(f))), &rec); err != nil {
				t.Fatalf("pointer was not rewritten: %v", err)
			}
			if rec.Tag != "v2" {
				t.Errorf("pointer tag = %q, want v2", rec.Tag)
			}
		})
	}
}

func TestEnsureDir_LatestZeroTTLSelectsDefault(t *testing.T) {
	g := &fakeGitHub{t: t, latestTag: "v1", tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	f.TTL = 0
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}
	start := now

	tests := []struct {
		name     string
		age      time.Duration
		wantHits int32
	}{
		{"first resolve", 0, 1},
		{"just inside the default TTL", DefaultTTL - time.Second, 1},
		{"at the default TTL", DefaultTTL, 2},
	}
	for _, tt := range tests {
		now = start.Add(tt.age)
		if _, err := f.EnsureDir(context.Background(), ref); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := atomic.LoadInt32(&g.latestHits); got != tt.wantHits {
			t.Errorf("%s: latest hits = %d, want %d", tt.name, got, tt.wantHits)
		}
	}
}

func TestEnsureDir_LatestForceIgnoresFreshPointer(t *testing.T) {
	g := &fakeGitHub{t: t, latestTag: "v2", tarballs: map[string][]byte{"v2": rulesTarball(t, "o-r-2")}}
	f, _ := newFetcher(t, g)
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	f.Force = true
	seedPointer(t, f, "v1", now)

	dir, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dir, string(filepath.Separator)+"v2"+string(filepath.Separator)) {
		t.Errorf("dir = %s, want the v2 entry", dir)
	}
	if got := atomic.LoadInt32(&g.latestHits); got != 1 {
		t.Errorf("latest hits = %d, want 1", got)
	}
}

func TestEnsureDir_LatestPointerWriteIsBestEffort(t *testing.T) {
	g := &fakeGitHub{t: t, latestTag: "v1", tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
	f, _ := newFetcher(t, g)
	ref := Ref{Owner: "o", Repo: "r", Ref: LatestRef, Path: "rules/core"}

	// A file where the metadata directory belongs blocks the pointer only.
	metaPath := filepath.Dir(pointerPath(f))
	mustMkdir(t, filepath.Dir(metaPath))
	mustWrite(t, metaPath, "blocker")

	for range 2 {
		dir, err := f.EnsureDir(context.Background(), ref)
		if err != nil {
			t.Fatalf("EnsureDir: %v", err)
		}
		if got := readFile(t, filepath.Join(dir, "ENT.json")); got != `{"entity":{"id":"ENT"}}` {
			t.Errorf("ENT.json = %q", got)
		}
	}
	// With no pointer stored, each call has to ask the API again.
	if got := atomic.LoadInt32(&g.latestHits); got != 2 {
		t.Errorf("latest hits = %d, want 2", got)
	}
}

func TestEnsureDir_ForceFailureKeepsCachedGeneration(t *testing.T) {
	tests := []struct {
		name    string
		tarball func(t *testing.T) []byte
		wantErr string
	}{
		{
			name:    "hostile replacement",
			tarball: func(t *testing.T) []byte { return rawTarball(t, regEntry("o-r-2/rules/core/../../../escape", "x")) },
			wantErr: "escapes extraction root",
		},
		{
			name:    "replacement is not a tarball",
			tarball: func(*testing.T) []byte { return []byte("<html>") },
			wantErr: "remote: extracting github://o/r@v1/rules/core: gzip:",
		},
		{
			name: "replacement lacks the path",
			tarball: func(t *testing.T) []byte {
				return rawTarball(t, regEntry("o-r-2/README.md", "moved"))
			},
			wantErr: `remote: path "rules/core" not found in o/r@v1`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
			f, _ := newFetcher(t, g)
			ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}
			dir, err := f.EnsureDir(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			before := treeOf(t, dir)

			g.tarballs["v1"] = tt.tarball(t)
			f.Force = true
			_, err = f.EnsureDir(context.Background(), ref)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			if got := treeOf(t, dir); !maps.Equal(got, before) {
				t.Errorf("cached generation changed: %v, want %v", got, before)
			}
			assertNoStagingDirs(t, filepath.Dir(dir))
		})
	}
}

func TestEnsureDir_CacheInstallFailures(t *testing.T) {
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}
	tests := []struct {
		name    string
		plant   func(t *testing.T, refDir, dir string)
		perms   bool // needs Unix permissions or errno values
		wantErr string
	}{
		{
			name: "file where the ref directory belongs",
			plant: func(t *testing.T, refDir, _ string) {
				mustMkdir(t, filepath.Dir(refDir))
				mustWrite(t, refDir, "blocker")
			},
			wantErr: "remote: creating cache dir: ",
		},
		{
			name: "read-only ref directory",
			plant: func(t *testing.T, refDir, _ string) {
				mustMkdir(t, refDir)
				makeReadOnly(t, refDir)
			},
			perms:   true,
			wantErr: "remote: creating temp dir: ",
		},
		{
			// A regular file, because renaming the staged directory onto one
			// fails with ENOTDIR. A directory here would be a cache hit, and
			// EnsureDir would return it without downloading.
			name: "file where the cache entry belongs",
			plant: func(t *testing.T, refDir, dir string) {
				mustMkdir(t, refDir)
				mustWrite(t, dir, "blocker")
			},
			wantErr: "remote: installing cached dir: ",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.perms {
				skipWithoutUnixPerms(t)
			}
			g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": rulesTarball(t, "o-r-1")}}
			f, _ := newFetcher(t, g)
			refDir := filepath.Join(f.CacheDir, "github", "o", "r", "v1")
			tt.plant(t, refDir, filepath.Join(refDir, pathKey(ref.Path)))

			_, err := f.EnsureDir(context.Background(), ref)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
			assertNoStagingDirs(t, refDir)
		})
	}
}

// The first install wins: a fetch that loses the rename accepts the winner's
// directory rather than failing or merging into it.
func TestEnsureDir_ConcurrentFetchWins(t *testing.T) {
	ref := Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules/core"}
	cacheDir := t.TempDir()
	dir, err := filepath.Abs(filepath.Join(cacheDir, "github", "o", "r", "v1", pathKey(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	tarball := rulesTarball(t, "o-r-1")

	// The handler installs the rival entry after EnsureDir has seen the cache
	// empty, which is the window a real concurrent fetch would hit.
	f := newRawFetcher(t, func(w http.ResponseWriter, _ *http.Request) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Error(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "winner.json"), []byte("first"), 0o644); err != nil {
			t.Error(err)
		}
		_, _ = w.Write(tarball)
	})
	f.CacheDir = cacheDir

	got, err := f.EnsureDir(context.Background(), ref)
	if err != nil {
		t.Fatalf("EnsureDir: %v", err)
	}
	if got != dir {
		t.Errorf("dir = %s, want %s", got, dir)
	}
	if tree, want := treeOf(t, dir), map[string]string{"winner.json": "first"}; !maps.Equal(tree, want) {
		t.Errorf("tree = %v, want %v", tree, want)
	}
	assertNoStagingDirs(t, filepath.Dir(dir))
}

func TestEnsureDir_ValidatesRefBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name    string
		ref     Ref
		wantErr string
	}{
		{"no owner", Ref{Repo: "r", Ref: "v1"}, "remote: incomplete reference"},
		{"no repo", Ref{Owner: "o", Ref: "v1"}, "remote: incomplete reference"},
		{"no ref", Ref{Owner: "o", Repo: "r"}, "remote: incomplete reference"},
		{"hidden owner", Ref{Owner: ".git", Repo: "r", Ref: "v1"}, `remote: illegal component ".git"`},
		{"slash in repo", Ref{Owner: "o", Repo: "a/b", Ref: "v1"}, `remote: illegal component "a/b"`},
		{"backslash in ref", Ref{Owner: "o", Repo: "r", Ref: `a\b`}, `remote: illegal component "a\\b"`},
		{"hidden ref", Ref{Owner: "o", Repo: "r", Ref: ".meta"}, `remote: illegal component ".meta"`},
		{"dot-dot in path", Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "a/../b"}, "remote: illegal path in reference"},
		{"backslash in path", Ref{Owner: "o", Repo: "r", Ref: "v1", Path: `a\b`}, "remote: illegal path in reference"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{
				CacheDir: t.TempDir(),
				Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					t.Errorf("unexpected request to %s", r.URL)
					return nil, errors.New("unexpected request")
				})},
			}
			_, err := f.EnsureDir(context.Background(), tt.ref)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestEnsureDir_DefaultBaseURLAndHeaders(t *testing.T) {
	tests := []struct {
		name    string
		ref     Ref
		wantURL string
	}{
		{"tarball", Ref{Owner: "o", Repo: "r", Ref: "v1"}, "https://api.github.com/repos/o/r/tarball/v1"},
		{"latest", Ref{Owner: "o", Repo: "r", Ref: LatestRef}, "https://api.github.com/repos/o/r/releases/latest"},
		{"escaped ref", Ref{Owner: "o", Repo: "r", Ref: "v1#b"}, "https://api.github.com/repos/o/r/tarball/v1%23b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got *http.Request
			f := &Fetcher{
				CacheDir: t.TempDir(),
				Token:    "secret",
				Client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
					got = r
					return &http.Response{StatusCode: http.StatusNotFound, Header: http.Header{}, Body: http.NoBody, Request: r}, nil
				})},
			}
			if _, err := f.EnsureDir(context.Background(), tt.ref); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if got == nil {
				t.Fatal("no request sent")
			}
			if got.URL.String() != tt.wantURL {
				t.Errorf("URL = %s, want %s", got.URL, tt.wantURL)
			}
			wantHeaders := map[string]string{
				"Accept":               "application/vnd.github+json",
				"X-GitHub-Api-Version": "2022-11-28",
				"User-Agent":           "strspc-manager",
				"Authorization":        "Bearer secret",
			}
			for k, want := range wantHeaders {
				if v := got.Header.Get(k); v != want {
					t.Errorf("%s = %q, want %q", k, v, want)
				}
			}
		})
	}
}

func TestEnsureDir_RequestFailures(t *testing.T) {
	errDial := errors.New("dial refused")
	failing := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errDial
	})}
	tests := []struct {
		name    string
		client  *http.Client
		baseURL string
		wantErr string
		wantIs  error
	}{
		{name: "malformed base URL", client: failing, baseURL: "http://[::1", wantErr: "remote: building request: "},
		{name: "transport failure", client: failing, wantErr: "remote: ", wantIs: errDial},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{Client: tt.client, BaseURL: tt.baseURL, CacheDir: t.TempDir()}
			_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1"})
			if err == nil || !strings.HasPrefix(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want prefix %q", err, tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("err = %v, want it to wrap %v", err, tt.wantIs)
			}
		})
	}
}

func TestEnsureDir_ExtractionErrorNamesTheRef(t *testing.T) {
	g := &fakeGitHub{t: t, tarballs: map[string][]byte{"v1": []byte("<html>rate limited</html>")}}
	f, _ := newFetcher(t, g)
	_, err := f.EnsureDir(context.Background(), Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules"})
	want := "remote: extracting github://o/r@v1/rules: gzip: gzip: invalid header"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
	refDir := filepath.Join(f.CacheDir, "github", "o", "r", "v1")
	if entries, _ := os.ReadDir(refDir); len(entries) != 0 {
		t.Errorf("expected empty ref dir after failure, got %d entries", len(entries))
	}
}

func TestFetcher_BaseURL(t *testing.T) {
	tests := []struct {
		name string
		base string
		want string
	}{
		{"unset selects GitHub", "", DefaultBaseURL},
		{"custom", "http://ghe.example/api/v3", "http://ghe.example/api/v3"},
		{"trailing slashes trimmed", "http://ghe.example/api/v3//", "http://ghe.example/api/v3"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{BaseURL: tt.base}
			if got := f.baseURL(); got != tt.want {
				t.Errorf("baseURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetcher_Client(t *testing.T) {
	if got := (&Fetcher{}).client().Timeout; got != defaultTimeout {
		t.Errorf("default client timeout = %v, want %v", got, defaultTimeout)
	}
	custom := &http.Client{}
	if got := (&Fetcher{Client: custom}).client(); got != custom {
		t.Errorf("client() = %p, want the configured client %p", got, custom)
	}
}
