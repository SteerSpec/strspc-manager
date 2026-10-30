package schema_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// site serves the layout of steerspec.dev: the latest schemas below /schemas
// and the published schemas of each release below /rules/vX.Y.Z/_schema.
func site(t *testing.T, routes map[string]string) (*httptest.Server, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls
}

func writeRealm(t *testing.T, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFetch_Pinned_UsesReleaseSchema(t *testing.T) {
	srv, _ := site(t, map[string]string{
		"/schemas/entity/v1.json":                      `"latest"`,
		"/rules/v1.10.0/_schema/entity.v1.schema.json": `"1.10.0"`,
		"/rules/v1.10.0/_schema/realm.v1.schema.json":  `"realm 1.10.0"`,
		"/rules/v1.10.0/_schema/bootstrap.schema.json": `"bootstrap 1.10.0"`,
	})
	f := schema.New(
		schema.WithBaseURL(srv.URL+"/schemas"),
		schema.WithCacheDir(t.TempDir()),
		schema.WithCoreVersion("1.10.0"),
	)

	for schemaPath, want := range map[string]string{
		schema.EntityV1Path:  `"1.10.0"`,
		schema.RealmV1Path:   `"realm 1.10.0"`,
		schema.BootstrapPath: `"bootstrap 1.10.0"`,
	} {
		got, err := f.Fetch(context.Background(), schemaPath)
		if err != nil {
			t.Fatalf("Fetch(%s): %v", schemaPath, err)
		}
		if string(got) != want {
			t.Errorf("Fetch(%s) = %s, want %s", schemaPath, got, want)
		}
	}
}

func TestFetch_Pinned_CachedCopyNeverExpires(t *testing.T) {
	srv, calls := site(t, map[string]string{
		"/rules/v1.10.0/_schema/entity.v1.schema.json": `"1.10.0"`,
	})
	cache := t.TempDir()
	f := schema.New(
		schema.WithBaseURL(srv.URL+"/schemas"),
		schema.WithCacheDir(cache),
		schema.WithCoreVersion("1.10.0"),
	)
	if _, err := f.EntityV1(context.Background()); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-365 * 24 * time.Hour)
	entry := filepath.Join(cache, "versions", "1.10.0", "entity", "v1.json")
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}
	if _, err := f.EntityV1(context.Background()); err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Errorf("expected 1 server call, got %d", *calls)
	}
}

func TestFetch_Pinned_PinsDoNotShareACacheEntry(t *testing.T) {
	srv, _ := site(t, map[string]string{
		"/rules/v1.10.0/_schema/entity.v1.schema.json": `"1.10.0"`,
		"/rules/v1.11.0/_schema/entity.v1.schema.json": `"1.11.0"`,
	})
	cache := t.TempDir()
	for _, version := range []string{"1.10.0", "1.11.0"} {
		f := schema.New(
			schema.WithBaseURL(srv.URL+"/schemas"),
			schema.WithCacheDir(cache),
			schema.WithCoreVersion(version),
		)
		got, err := f.EntityV1(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if want := `"` + version + `"`; string(got) != want {
			t.Errorf("pin %s: got %s", version, got)
		}
	}
}

func TestFetch_Pinned_FallsBackToLatestWhenReleaseHasNoSchema(t *testing.T) {
	srv, _ := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})
	f := schema.New(
		schema.WithBaseURL(srv.URL+"/schemas"),
		schema.WithCacheDir(t.TempDir()),
		schema.WithCoreVersion("1.0.0"),
	)

	got, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"latest"` {
		t.Errorf("got %s", got)
	}
}

func TestFetch_Pinned_ServerErrorDoesNotFallBack(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/schemas/entity/v1.json" {
			_, _ = w.Write([]byte(`"latest"`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	f := schema.New(
		schema.WithBaseURL(srv.URL+"/schemas"),
		schema.WithCacheDir(t.TempDir()),
		schema.WithCoreVersion("1.10.0"),
	)

	if got, err := f.EntityV1(context.Background()); err == nil {
		t.Fatalf("expected an error, got %s", got)
	}
}

func TestWithCoreVersion_IgnoresAnythingButAReleaseVersion(t *testing.T) {
	for _, version := range []string{"../../etc", "1.10", "v1.10.0", "1.10.0/../..", ""} {
		srv, _ := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})
		f := schema.New(
			schema.WithBaseURL(srv.URL+"/schemas"),
			schema.WithCacheDir(t.TempDir()),
			schema.WithCoreVersion(version),
		)
		got, err := f.EntityV1(context.Background())
		if err != nil {
			t.Fatalf("version %q: %v", version, err)
		}
		if string(got) != `"latest"` {
			t.Errorf("version %q: got %s", version, got)
		}
	}
}

func TestFetch_Latest_RefetchesAnExpiredCopy(t *testing.T) {
	srv, calls := site(t, map[string]string{"/schemas/entity/v1.json": `"new"`})
	cache := t.TempDir()
	entry := filepath.Join(cache, "entity", "v1.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`"old"`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}

	f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(cache))
	got, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"new"` {
		t.Errorf("got %s", got)
	}
	if *calls != 1 {
		t.Errorf("expected 1 server call, got %d", *calls)
	}
	if on, _ := os.ReadFile(entry); string(on) != `"new"` {
		t.Errorf("cache not refreshed: %s", on)
	}
}

func TestFetch_Latest_KeepsAnExpiredCopyWhenOffline(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()

	cache := t.TempDir()
	entry := filepath.Join(cache, "entity", "v1.json")
	if err := os.MkdirAll(filepath.Dir(entry), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte(`"old"`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(entry, old, old); err != nil {
		t.Fatal(err)
	}

	f := schema.New(schema.WithBaseURL(url+"/schemas"), schema.WithCacheDir(cache))
	got, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"old"` {
		t.Errorf("got %s", got)
	}
}

func TestForRealm_PinsADependencyOnCore(t *testing.T) {
	dir := writeRealm(t, `{
		"realm": {"id": "dev.example.bank"},
		"dependencies": [
			{"realm_id": "dev.example.other", "version": "9.9.9"},
			{"realm_id": "dev.steerspec.core", "version": "1.10.0"}
		]
	}`)
	srv, _ := site(t, map[string]string{
		"/schemas/entity/v1.json":                      `"latest"`,
		"/rules/v1.10.0/_schema/entity.v1.schema.json": `"1.10.0"`,
	})
	opts := append([]schema.Option{
		schema.WithBaseURL(srv.URL + "/schemas"),
		schema.WithCacheDir(t.TempDir()),
	}, schema.ForRealm(dir)...)

	got, err := schema.New(opts...).EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"1.10.0"` {
		t.Errorf("got %s", got)
	}
}

func TestForRealm_CoreUsesItsOwnSchemas(t *testing.T) {
	dir := writeRealm(t, `{"realm": {"id": "dev.steerspec.core"}, "dependencies": []}`)
	if err := os.Mkdir(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`"local"`), 0o600); err != nil {
		t.Fatal(err)
	}
	srv, calls := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})
	opts := append([]schema.Option{
		schema.WithBaseURL(srv.URL + "/schemas"),
		schema.WithCacheDir(t.TempDir()),
	}, schema.ForRealm(dir)...)

	got, err := schema.New(opts...).EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"local"` {
		t.Errorf("got %s", got)
	}
	if *calls != 0 {
		t.Errorf("expected no server call, got %d", *calls)
	}
}

func TestForRealm_IgnoresANonCoreRealmsOwnSchemas(t *testing.T) {
	for name, tt := range map[string]struct{ manifest, want string }{
		"pinned": {
			`{"realm": {"id": "dev.example.bank"}, "dependencies": [{"realm_id": "dev.steerspec.core", "version": "1.10.0"}]}`,
			`"1.10.0"`,
		},
		"unpinned": {`{"realm": {"id": "dev.example.bank"}, "dependencies": []}`, `"latest"`},
	} {
		t.Run(name, func(t *testing.T) {
			dir := writeRealm(t, tt.manifest)
			if err := os.Mkdir(filepath.Join(dir, "_schema"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "_schema", "entity.v1.schema.json"), []byte(`"local"`), 0o600); err != nil {
				t.Fatal(err)
			}
			srv, _ := site(t, map[string]string{
				"/schemas/entity/v1.json":                      `"latest"`,
				"/rules/v1.10.0/_schema/entity.v1.schema.json": `"1.10.0"`,
			})
			opts := append([]schema.Option{
				schema.WithBaseURL(srv.URL + "/schemas"),
				schema.WithCacheDir(t.TempDir()),
			}, schema.ForRealm(dir)...)

			got, err := schema.New(opts...).EntityV1(context.Background())
			if err != nil {
				t.Fatalf("EntityV1: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestForRealm_NoPinWithoutAManifest(t *testing.T) {
	if opts := schema.ForRealm(t.TempDir()); len(opts) != 0 {
		t.Errorf("expected no options, got %d", len(opts))
	}
	dir := writeRealm(t, `{not json`)
	if opts := schema.ForRealm(dir); len(opts) != 0 {
		t.Errorf("expected no options for a broken manifest, got %d", len(opts))
	}
}
