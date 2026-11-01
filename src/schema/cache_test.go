package schema_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// occupy puts something other than a usable schema at path.
type occupy func(t *testing.T, path string)

func occupyWithOversizedFile(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, schemaSizeLimit+1); err != nil {
		t.Fatal(err)
	}
}

func occupyWithDirectory(t *testing.T, path string) {
	t.Helper()
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func occupyWithSymlink(t *testing.T, path string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(`"elsewhere"`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if got, err := os.ReadFile(target); err != nil || string(got) != `"elsewhere"` {
			t.Errorf("symlink target was touched: %q, %v", got, err)
		}
	})
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestFetch_ReplacesAnUnusableCacheEntry(t *testing.T) {
	tests := []struct {
		name  string
		setup occupy
	}{
		{"symlink", occupyWithSymlink},
		{"oversized file", occupyWithOversizedFile},
		{"empty directory", occupyWithDirectory},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, calls := site(t, map[string]string{"/schemas/entity/v1.json": `"served"`})
			cache := t.TempDir()
			entry := filepath.Join(cache, "entity", "v1.json")
			if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, entry)

			f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(cache))
			got, err := f.EntityV1(context.Background())
			if err != nil {
				t.Fatalf("EntityV1: %v", err)
			}
			if string(got) != `"served"` {
				t.Errorf("got %s", got)
			}
			if *calls != 1 {
				t.Errorf("expected 1 server call, got %d", *calls)
			}

			fi, err := os.Lstat(entry)
			if err != nil {
				t.Fatal(err)
			}
			if !fi.Mode().IsRegular() {
				t.Fatalf("cache entry is still %v", fi.Mode())
			}
			if on, _ := os.ReadFile(entry); string(on) != `"served"` {
				t.Errorf("cache holds %s", on)
			}
		})
	}
}

func TestFetch_ServesACachedSchemaAtTheSizeLimit(t *testing.T) {
	srv, calls := site(t, nil)
	cache := t.TempDir()
	entry := filepath.Join(cache, "entity", "v1.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	want := bytes.Repeat([]byte("x"), schemaSizeLimit)
	if err := os.WriteFile(entry, want, 0o600); err != nil {
		t.Fatal(err)
	}

	f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(cache))
	got, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %d bytes, want %d", len(got), len(want))
	}
	if *calls != 0 {
		t.Errorf("expected no server call, got %d", *calls)
	}
}

func TestWithLocalDir_SkipsAnUnusableSchema(t *testing.T) {
	tests := []struct {
		name  string
		setup occupy
	}{
		{"nothing there", func(*testing.T, string) {}},
		{"directory", occupyWithDirectory},
		{"oversized file", occupyWithOversizedFile},
		{"symlink", occupyWithSymlink},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.Mkdir(filepath.Join(dir, "_schema"), 0o755); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, filepath.Join(dir, "_schema", "entity.v1.schema.json"))
			srv, _ := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})

			got, err := schema.New(
				schema.WithBaseURL(srv.URL+"/schemas"),
				schema.WithCacheDir(t.TempDir()),
				schema.WithLocalDir(dir),
			).EntityV1(context.Background())
			if err != nil {
				t.Fatalf("EntityV1: %v", err)
			}
			if string(got) != `"latest"` {
				t.Errorf("got %s", got)
			}
		})
	}
}

func TestFetch_RefusesACacheDirectoryThatLeavesTheCache(t *testing.T) {
	srv, calls := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})
	cache, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(cache, "entity")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(cache))
	_, err := f.EntityV1(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := `invalid schema path: "entity/v1.json"`; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
	if *calls != 0 {
		t.Errorf("expected no server call, got %d", *calls)
	}
	if entries, _ := os.ReadDir(outside); len(entries) != 0 {
		t.Errorf("wrote outside the cache: %v", entries)
	}
}

func TestFetch_CacheFailureIsNotFatal(t *testing.T) {
	tests := []struct {
		name          string
		needsFilePerm bool
		setup         func(t *testing.T, dir string) // dir is <cache>/entity
	}{
		{
			name: "cache directory cannot be created",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.WriteFile(dir, nil, 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:          "cache directory is read-only",
			needsFilePerm: true,
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.Mkdir(dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			},
		},
		{
			name: "entry is a directory that cannot be removed",
			setup: func(t *testing.T, dir string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Join(dir, "v1.json", "keep"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.needsFilePerm && (os.Geteuid() == 0 || runtime.GOOS == "windows") {
				t.Skip("needs directory modes enforced: not on Windows or as root")
			}
			srv, calls := site(t, map[string]string{"/schemas/entity/v1.json": `"served"`})
			cache := t.TempDir()
			dir := filepath.Join(cache, "entity")
			tt.setup(t, dir)
			f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(cache))

			// Nothing can be cached, so each fetch goes back to the server.
			for want := 1; want <= 2; want++ {
				got, err := f.EntityV1(context.Background())
				if err != nil {
					t.Fatalf("fetch %d: %v", want, err)
				}
				if string(got) != `"served"` {
					t.Errorf("fetch %d: got %s", want, got)
				}
				if *calls != want {
					t.Errorf("fetch %d: %d server calls", want, *calls)
				}
			}

			leftovers, err := filepath.Glob(filepath.Join(dir, ".schema-cache-*"))
			if err != nil {
				t.Fatal(err)
			}
			if len(leftovers) != 0 {
				t.Errorf("temp files left behind: %v", leftovers)
			}
		})
	}
}
