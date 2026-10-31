package remote

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"
)

// writeLatestPointer stores a "latest" pointer for o/r.
func writeLatestPointer(t *testing.T, f *Fetcher, tag string, at time.Time) {
	t.Helper()
	data, err := json.Marshal(latestRecord{Tag: tag, ResolvedAt: at})
	if err != nil {
		t.Fatal(err)
	}
	writePointerBytes(t, f, data)
}

func writePointerBytes(t *testing.T, f *Fetcher, data []byte) {
	t.Helper()
	dir := filepath.Join(f.CacheDir, "github", "o", "r", metaDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, latestPointer), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeCachedDir creates the directory EnsureDir would use for a concrete ref.
func makeCachedDir(t *testing.T, f *Fetcher, ref, path string) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(f.CacheDir, "github", "o", "r", ref, pathKey(path)))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCached(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	fresh := now.Add(-30 * time.Minute)
	stale := now.Add(-2 * time.Hour)
	atTTL := now.Add(-time.Hour)

	tests := []struct {
		name  string
		setup func(t *testing.T, f *Fetcher) string // returns the wanted Dir
		ref   string
		want  CacheEntry
	}{
		{
			name:  "pinned, not cached",
			setup: func(*testing.T, *Fetcher) string { return "" },
			ref:   "v1.0.0",
			want:  CacheEntry{Ref: "v1.0.0"},
		},
		{
			name: "pinned, cached",
			setup: func(t *testing.T, f *Fetcher) string {
				return makeCachedDir(t, f, "v1.0.0", "rules/core")
			},
			ref:  "v1.0.0",
			want: CacheEntry{Ref: "v1.0.0"},
		},
		{
			name: "pinned, cached for another path",
			setup: func(t *testing.T, f *Fetcher) string {
				makeCachedDir(t, f, "v1.0.0", "rules/other")
				return ""
			},
			ref:  "v1.0.0",
			want: CacheEntry{Ref: "v1.0.0"},
		},
		{
			name:  "latest, never resolved",
			setup: func(*testing.T, *Fetcher) string { return "" },
			ref:   LatestRef,
			want:  CacheEntry{},
		},
		{
			name: "latest, fresh",
			setup: func(t *testing.T, f *Fetcher) string {
				writeLatestPointer(t, f, "v2.0.0", fresh)
				return makeCachedDir(t, f, "v2.0.0", "rules/core")
			},
			ref:  LatestRef,
			want: CacheEntry{Ref: "v2.0.0", ResolvedAt: fresh},
		},
		{
			name: "latest, stale",
			setup: func(t *testing.T, f *Fetcher) string {
				writeLatestPointer(t, f, "v2.0.0", stale)
				return makeCachedDir(t, f, "v2.0.0", "rules/core")
			},
			ref:  LatestRef,
			want: CacheEntry{Ref: "v2.0.0", ResolvedAt: stale, Stale: true},
		},
		{
			name: "latest, exactly at the TTL is stale, as EnsureDir treats it",
			setup: func(t *testing.T, f *Fetcher) string {
				writeLatestPointer(t, f, "v2.0.0", atTTL)
				return makeCachedDir(t, f, "v2.0.0", "rules/core")
			},
			ref:  LatestRef,
			want: CacheEntry{Ref: "v2.0.0", ResolvedAt: atTTL, Stale: true},
		},
		{
			name: "latest, resolved but this path is not cached",
			setup: func(t *testing.T, f *Fetcher) string {
				writeLatestPointer(t, f, "v2.0.0", fresh)
				return ""
			},
			ref:  LatestRef,
			want: CacheEntry{Ref: "v2.0.0", ResolvedAt: fresh},
		},
		{
			name: "latest, corrupt pointer counts as never resolved",
			setup: func(t *testing.T, f *Fetcher) string {
				writePointerBytes(t, f, []byte("not json"))
				return ""
			},
			ref:  LatestRef,
			want: CacheEntry{},
		},
		{
			name: "latest, unsafe tag in the pointer is ignored",
			setup: func(t *testing.T, f *Fetcher) string {
				writeLatestPointer(t, f, "../../etc", fresh)
				return ""
			},
			ref:  LatestRef,
			want: CacheEntry{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &Fetcher{CacheDir: t.TempDir(), TTL: time.Hour, now: func() time.Time { return now }}
			wantDir := tt.setup(t, f)
			got, err := f.Cached(Ref{Owner: "o", Repo: "r", Ref: tt.ref, Path: "rules/core"})
			if err != nil {
				t.Fatal(err)
			}
			want := tt.want
			want.Dir = wantDir
			if !got.ResolvedAt.Equal(want.ResolvedAt) {
				t.Errorf("ResolvedAt = %v, want %v", got.ResolvedAt, want.ResolvedAt)
			}
			got.ResolvedAt, want.ResolvedAt = time.Time{}, time.Time{}
			if got != want {
				t.Errorf("got %+v, want %+v", got, want)
			}
		})
	}
}

func TestCachedPinnedIsNeverStale(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir(), TTL: time.Nanosecond}
	makeCachedDir(t, f, "v1.0.0", "")
	got, err := f.Cached(Ref{Owner: "o", Repo: "r", Ref: "v1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stale || !got.ResolvedAt.IsZero() {
		t.Errorf("got %+v", got)
	}
}

// The default TTL applies when none is set, as it does in EnsureDir.
func TestCachedDefaultTTL(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	f := &Fetcher{CacheDir: t.TempDir(), now: func() time.Time { return now }}
	writeLatestPointer(t, f, "v2.0.0", now.Add(-DefaultTTL+time.Minute))
	got, err := f.Cached(Ref{Owner: "o", Repo: "r", Ref: LatestRef})
	if err != nil {
		t.Fatal(err)
	}
	if got.Stale {
		t.Errorf("stale inside the default TTL: %+v", got)
	}
}

// Cached and EnsureDir must agree on where a reference lives.
func TestCachedFindsWhatEnsureDirFetched(t *testing.T) {
	g := &fakeGitHub{
		t:         t,
		latestTag: "v1.0.0",
		tarballs:  map[string][]byte{"v1.0.0": rulesTarball(t, "o-r-1")},
	}
	f, _ := newFetcher(t, g)
	for _, name := range []string{"v1.0.0", LatestRef} {
		t.Run(name, func(t *testing.T) {
			ref := Ref{Owner: "o", Repo: "r", Ref: name, Path: "rules/core"}
			dir, err := f.EnsureDir(context.Background(), ref)
			if err != nil {
				t.Fatal(err)
			}
			got, err := f.Cached(ref)
			if err != nil {
				t.Fatal(err)
			}
			if got.Dir != dir || got.Ref != "v1.0.0" {
				t.Errorf("got %+v, want Dir %s", got, dir)
			}
		})
	}
}

// listTree returns every path under dir.
func listTree(t *testing.T, dir string) []string {
	t.Helper()
	var paths []string
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func TestCachedTouchesNeitherNetworkNorDisk(t *testing.T) {
	g := &fakeGitHub{
		t:         t,
		latestTag: "v2.0.0",
		tarballs:  map[string][]byte{"v1.0.0": rulesTarball(t, "o-r-1")},
	}
	f, _ := newFetcher(t, g)
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	f.now = func() time.Time { return now }
	writeLatestPointer(t, f, "v1.0.0", now.Add(-48*time.Hour)) // stale, and upstream moved
	before := listTree(t, f.CacheDir)

	for _, name := range []string{"v1.0.0", "v9.9.9", LatestRef} {
		if _, err := f.Cached(Ref{Owner: "o", Repo: "r", Ref: name, Path: "rules/core"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.Cached(Ref{Owner: "o", Repo: "other", Ref: LatestRef}); err != nil {
		t.Fatal(err)
	}

	if hits := atomic.LoadInt32(&g.latestHits) + atomic.LoadInt32(&g.tarballHits); hits != 0 {
		t.Errorf("%d HTTP calls", hits)
	}
	if after := listTree(t, f.CacheDir); !slices.Equal(before, after) {
		t.Errorf("cache changed:\nbefore %v\nafter  %v", before, after)
	}
}

func TestCachedRejectsWhatEnsureDirRejects(t *testing.T) {
	f := &Fetcher{CacheDir: t.TempDir()}
	bad := []Ref{
		{Owner: "..", Repo: "r", Ref: "v1"},
		{Owner: "o", Repo: "../x", Ref: "v1"},
		{Owner: "o", Repo: "r", Ref: "v1/../.."},
		{Owner: "o", Repo: "r", Ref: "v1", Path: "../etc"},
		{Owner: "", Repo: "r", Ref: "v1"},
	}
	for _, ref := range bad {
		if _, err := f.Cached(ref); err == nil {
			t.Errorf("Cached(%+v) should fail", ref)
		}
	}
	if _, err := (&Fetcher{}).Cached(Ref{Owner: "o", Repo: "r", Ref: "v1"}); err == nil {
		t.Error("Cached without a cache directory should fail")
	}
}
