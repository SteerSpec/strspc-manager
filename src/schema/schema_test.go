package schema_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

func TestFetch_FromServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/entity/v1.json" {
			_, _ = w.Write([]byte(`{"type": "object"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	f := schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(t.TempDir()),
	)

	data, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty schema data")
	}
}

func TestFetch_ServesFromCache(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"cached": true}`))
	}))
	defer srv.Close()

	f := schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(t.TempDir()),
	)

	ctx := context.Background()

	// First fetch — hits server.
	_, err := f.Fetch(ctx, "entity/v1.json")
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}

	// Second fetch — should serve from cache.
	_, err = f.Fetch(ctx, "entity/v1.json")
	if err != nil {
		t.Fatalf("second fetch: %v", err)
	}

	if calls != 1 {
		t.Errorf("expected 1 server call, got %d", calls)
	}
}

func TestFetch_InvalidPaths(t *testing.T) {
	f := schema.New(
		schema.WithBaseURL("http://unused"),
		schema.WithCacheDir(t.TempDir()),
	)

	cases := []struct {
		name string
		path string
	}{
		{"empty", ""},
		{"dot", "."},
		{"traversal", "../etc/passwd"},
		{"nested traversal", "entity/../../etc/passwd"},
		{"absolute", "/etc/passwd"},
		{"backslash traversal", `entity\..\..\etc\passwd`},
		{"backslash", `entity\v1.json`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.Fetch(context.Background(), tc.path)
			if err == nil {
				t.Errorf("expected error for path %q, got nil", tc.path)
			}
		})
	}
}

func TestFetch_HTTP404(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	f := schema.New(
		schema.WithBaseURL(srv.URL),
		schema.WithCacheDir(t.TempDir()),
	)

	_, err := f.Fetch(context.Background(), "nonexistent.json")
	if err == nil {
		t.Error("expected error for 404 response")
	}
}
