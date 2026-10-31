package ruleresolve

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// sourceFunc adapts a function to the Source interface.
type sourceFunc func(ctx context.Context, ref string) ([]SourceFile, *result.Result)

func (f sourceFunc) Fetch(ctx context.Context, ref string) ([]SourceFile, *result.Result) {
	return f(ctx, ref)
}

// diagnostic returns the first diagnostic with the given code.
func diagnostic(t *testing.T, res *result.Result, code string) result.Diagnostic {
	t.Helper()
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return d
		}
	}
	t.Fatalf("expected diagnostic %s, got: %v", code, res.Diagnostics)
	return result.Diagnostic{}
}

func TestWithBaseDir_AnchorsRelativeSources(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	testdata := testdataPath("")

	tests := []struct {
		name       string
		source     string
		opts       []Option
		wantSource string
		wantFiles  int // 0 means the source must be reported missing
	}{
		{
			name:       "base dir anchors a relative source",
			source:     "valid",
			opts:       []Option{WithBaseDir(testdata)},
			wantSource: filepath.Join(testdata, "valid"),
			wantFiles:  2,
		},
		{
			name:       "the same source misses without the base dir",
			source:     "valid",
			wantSource: filepath.Join(wd, "valid"),
		},
		{
			name:       "working directory is the default",
			source:     "testdata/valid",
			wantSource: filepath.Join(wd, "testdata", "valid"),
			wantFiles:  2,
		},
		{
			name:       "base dir replaces the working directory",
			source:     "testdata/valid",
			opts:       []Option{WithBaseDir(testdata)},
			wantSource: filepath.Join(testdata, "testdata", "valid"),
		},
		{
			name:       "absolute source ignores the base dir",
			source:     filepath.Join(testdata, "valid"),
			opts:       []Option{WithBaseDir(t.TempDir())},
			wantSource: filepath.Join(testdata, "valid"),
			wantFiles:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, err := New([]SourceEntry{{Source: tt.source, Scope: ScopeGlobal}}, tt.opts...)
			if err != nil {
				t.Fatal(err)
			}

			files, res := r.Resolve(context.Background())
			if len(files) != tt.wantFiles {
				t.Fatalf("got %d files, want %d: %v", len(files), tt.wantFiles, res.Diagnostics)
			}
			if tt.wantFiles == 0 {
				if d := diagnostic(t, res, "RSV002"); d.Path != tt.wantSource {
					t.Errorf("RSV002 path = %q, want %q", d.Path, tt.wantSource)
				}
				return
			}
			if !res.OK() {
				t.Fatalf("expected OK, got errors: %v", res.Errors())
			}
			for _, f := range files {
				if f.ResolvedSource != tt.wantSource {
					t.Errorf("ResolvedSource = %q, want %q", f.ResolvedSource, tt.wantSource)
				}
				if f.Origin.Source != tt.source {
					t.Errorf("Origin.Source = %q, want the configured %q", f.Origin.Source, tt.source)
				}
			}
		})
	}
}

func TestResolve_OneDirectorySpelledTwiceIsNotACollision(t *testing.T) {
	testdata := testdataPath("")
	r, err := New([]SourceEntry{
		{Source: "valid", Scope: ScopeGlobal},
		{Source: filepath.Join(testdata, "empty", "..", "valid"), Scope: ScopeLocal},
	}, WithBaseDir(testdata))
	if err != nil {
		t.Fatal(err)
	}

	files, res := r.Resolve(context.Background())
	if len(files) != 4 {
		t.Fatalf("expected 4 files, got %d", len(files))
	}
	if hasCode(res, "RSV005") {
		t.Errorf("both entries name the same directory: %v", res.Diagnostics)
	}
}

// movingRelease serves GitHub's releases API for a repository whose latest
// release can be moved by the test.
type movingRelease struct {
	tag         atomic.Value // string
	latestCalls atomic.Int32
}

func (m *movingRelease) server(t *testing.T, tarballs map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/releases/latest") {
			m.latestCalls.Add(1)
			_, _ = w.Write([]byte(`{"tag_name":"` + m.tag.Load().(string) + `"}`))
			return
		}
		if body, ok := tarballs[path.Base(r.URL.Path)]; ok {
			_, _ = w.Write(body)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWithCacheTTL_BoundsHowLongLatestIsTrusted(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")

	tarballs := map[string][]byte{
		"v1.0.0": tarballFromDirs(t, "o-r-1", map[string]string{"rules": testdataPath("with_realm")}),
		"v2.0.0": tarballFromDirs(t, "o-r-2", map[string]string{"rules": testdataPath("valid")}),
	}

	tests := []struct {
		name            string
		ttl             time.Duration
		wantFilesLater  int
		wantLatestCalls int32
	}{
		{"inside the ttl the resolved tag is kept", time.Hour, 1, 1},
		{"past the ttl the moved tag is picked up", time.Nanosecond, 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := &movingRelease{}
			release.tag.Store("v1.0.0")
			srv := release.server(t, tarballs)

			r, err := New([]SourceEntry{
				{Source: "github://o/r@latest/rules", Scope: ScopeGlobal},
			}, WithCacheDir(t.TempDir()), WithCacheTTL(tt.ttl))
			if err != nil {
				t.Fatal(err)
			}
			// New built the fetcher from the options; only its endpoint is
			// redirected, so the TTL under test is the one WithCacheTTL set.
			r.cfg.Fetcher.Client = srv.Client()
			r.cfg.Fetcher.BaseURL = srv.URL

			files, res := r.Resolve(context.Background())
			if !res.OK() {
				t.Fatalf("first resolve: %v", res.Errors())
			}
			if len(files) != 1 {
				t.Fatalf("v1.0.0 has 1 entity file, got %d", len(files))
			}

			release.tag.Store("v2.0.0")
			files, res = r.Resolve(context.Background())
			if !res.OK() {
				t.Fatalf("second resolve: %v", res.Errors())
			}
			if len(files) != tt.wantFilesLater {
				t.Errorf("second resolve found %d files, want %d", len(files), tt.wantFilesLater)
			}
			if got := release.latestCalls.Load(); got != tt.wantLatestCalls {
				t.Errorf("releases API called %d times, want %d", got, tt.wantLatestCalls)
			}
		})
	}
}

func TestResolve_NormalisesTheRefGivenToASource(t *testing.T) {
	base := t.TempDir()

	tests := []struct {
		name    string
		source  string
		wantRef string
	}{
		{"github uri", "github://o/r@v1/rules/", "github://o/r@v1/rules"},
		{"unparsable uri is passed through", "custom://bucket/rules/", "custom://bucket/rules/"},
		{"relative path", "./a/../rules", filepath.Join(base, "rules")},
		{"absolute path", filepath.Join(base, "a") + "/../rules/", filepath.Join(base, "rules")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotRef string
			src := sourceFunc(func(_ context.Context, ref string) ([]SourceFile, *result.Result) {
				gotRef = ref
				// A source may report nothing at all.
				return []SourceFile{{File: &entity.File{}, Path: "file"}}, nil
			})
			entry := SourceEntry{Source: tt.source, Scope: ScopeGlobal}
			r := &Resolver{
				bindings: []sourceBinding{{source: src, entry: entry}},
				cfg:      Config{BaseDir: base},
			}

			files, res := r.Resolve(context.Background())
			if !res.OK() || len(res.Diagnostics) != 0 {
				t.Fatalf("expected no diagnostics, got %v", res.Diagnostics)
			}
			if gotRef != tt.wantRef {
				t.Errorf("source fetched %q, want %q", gotRef, tt.wantRef)
			}
			if len(files) != 1 || files[0].ResolvedSource != tt.wantRef || files[0].Origin != entry {
				t.Errorf("unexpected resolved files: %+v", files)
			}
		})
	}
}

func TestResolve_CancelledBetweenSources(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	first := sourceFunc(func(context.Context, string) ([]SourceFile, *result.Result) {
		cancel()
		return []SourceFile{{File: &entity.File{}, Path: "first"}}, nil
	})
	second := sourceFunc(func(context.Context, string) ([]SourceFile, *result.Result) {
		t.Error("second source fetched after cancellation")
		return nil, nil
	})
	r := &Resolver{bindings: []sourceBinding{
		{source: first, entry: SourceEntry{Source: "/first"}},
		{source: second, entry: SourceEntry{Source: "/second"}},
	}}

	files, res := r.Resolve(ctx)
	if files != nil {
		t.Errorf("a cancelled resolve must not return partial results, got %d files", len(files))
	}
	if d := diagnostic(t, res, "RSV000"); d.Message != context.Canceled.Error() {
		t.Errorf("RSV000 message = %q", d.Message)
	}
}
