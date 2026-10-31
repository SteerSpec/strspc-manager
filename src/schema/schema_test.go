package schema_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// schemaSizeLimit mirrors the package's maxSchemaSize.
const schemaSizeLimit = 1 << 20

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// clientReturning builds a client that never touches the network and answers
// every request with body.
func clientReturning(body string, requested *string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*requested = r.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	})}
}

func TestAccessors_FetchTheirOwnSchema(t *testing.T) {
	srv, _ := site(t, map[string]string{
		"/schemas/entity/v1.json":        `"entity"`,
		"/schemas/realm/v1.json":         `"realm"`,
		"/schemas/entity/bootstrap.json": `"bootstrap"`,
	})
	f := schema.New(schema.WithBaseURL(srv.URL+"/schemas"), schema.WithCacheDir(t.TempDir()))

	tests := []struct {
		name  string
		fetch func(*schema.Fetcher, context.Context) ([]byte, error)
		want  string
	}{
		{"EntityV1", (*schema.Fetcher).EntityV1, `"entity"`},
		{"RealmV1", (*schema.Fetcher).RealmV1, `"realm"`},
		{"Bootstrap", (*schema.Fetcher).Bootstrap, `"bootstrap"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.fetch(f, context.Background())
			if err != nil {
				t.Fatalf("%s: %v", tt.name, err)
			}
			if string(got) != tt.want {
				t.Errorf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWithClient_SendsRequestsThroughTheClient(t *testing.T) {
	var requested string
	f := schema.New(
		schema.WithBaseURL("http://schemas.invalid/schemas"),
		schema.WithCacheDir(t.TempDir()),
		schema.WithClient(clientReturning(`"stubbed"`, &requested)),
	)

	got, err := f.EntityV1(context.Background())
	if err != nil {
		t.Fatalf("EntityV1: %v", err)
	}
	if string(got) != `"stubbed"` {
		t.Errorf("got %s", got)
	}
	if want := "http://schemas.invalid/schemas/entity/v1.json"; requested != want {
		t.Errorf("requested %s, want %s", requested, want)
	}
}

func TestNew_KeepsAUsableClient(t *testing.T) {
	tests := []struct {
		name string
		opt  schema.Option
	}{
		{"WithClient ignores nil", schema.WithClient(nil)},
		{"an option clearing the client", func(c *schema.Config) { c.Client = nil }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _ := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})
			f := schema.New(
				schema.WithBaseURL(srv.URL+"/schemas"),
				schema.WithCacheDir(t.TempDir()),
				tt.opt,
			)

			got, err := f.EntityV1(context.Background())
			if err != nil {
				t.Fatalf("EntityV1: %v", err)
			}
			if string(got) != `"latest"` {
				t.Errorf("got %s", got)
			}
		})
	}
}

func TestNew_DefaultCacheDir(t *testing.T) {
	tests := []struct {
		name     string
		home     func(t *testing.T) string
		wantRoot func(t *testing.T, tmp string) string
	}{
		{
			name: "user cache directory",
			home: func(t *testing.T) string { return t.TempDir() },
			wantRoot: func(t *testing.T, _ string) string {
				dir, err := os.UserCacheDir()
				if err != nil {
					t.Fatal(err)
				}
				return dir
			},
		},
		{
			name: "temp directory when the user cache directory is unknown",
			home: func(t *testing.T) string {
				t.Setenv("LocalAppData", "") // Windows reads this, not HOME.
				return ""
			},
			wantRoot: func(t *testing.T, tmp string) string {
				if dir, err := os.UserCacheDir(); err == nil {
					t.Fatalf("test setup: UserCacheDir unexpectedly returned %s", dir)
				}
				return tmp
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmp := t.TempDir()
			t.Setenv("HOME", tt.home(t))
			t.Setenv("XDG_CACHE_HOME", "")
			t.Setenv("TMPDIR", tmp)
			t.Setenv("TMP", tmp) // The Windows name for it.
			srv, _ := site(t, map[string]string{"/schemas/entity/v1.json": `"latest"`})

			if _, err := schema.New(schema.WithBaseURL(srv.URL + "/schemas")).EntityV1(context.Background()); err != nil {
				t.Fatalf("EntityV1: %v", err)
			}

			entry := filepath.Join(tt.wantRoot(t, tmp), "strspc", "schemas", "entity", "v1.json")
			got, err := os.ReadFile(entry)
			if err != nil {
				t.Fatalf("schema not cached in the default location: %v", err)
			}
			if string(got) != `"latest"` {
				t.Errorf("cached %s", got)
			}
		})
	}
}

func TestFetch_DownloadOutcomes(t *testing.T) {
	atLimit := bytes.Repeat([]byte("x"), schemaSizeLimit)

	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr string // empty means the fetch succeeds
	}{
		{
			name:    "not found",
			handler: http.NotFound,
			wantErr: "/entity/v1.json: HTTP 404: not found",
		},
		{
			name: "server error",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadGateway)
			},
			wantErr: "/entity/v1.json: HTTP 502",
		},
		{
			name: "connection cut short",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Length", "100")
				_, _ = w.Write([]byte("short"))
			},
			wantErr: "unexpected EOF",
		},
		{
			name: "one byte over the size limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(append([]byte("x"), atLimit...))
			},
			wantErr: "exceeds max allowed size of 1048576 bytes",
		},
		{
			name: "exactly the size limit",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write(atLimit)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			cache := t.TempDir()
			f := schema.New(schema.WithBaseURL(srv.URL), schema.WithCacheDir(cache))

			got, err := f.EntityV1(context.Background())
			entry := filepath.Join(cache, "entity", "v1.json")

			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("EntityV1: %v", err)
				}
				if !bytes.Equal(got, atLimit) {
					t.Errorf("got %d bytes, want %d", len(got), len(atLimit))
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error, got %d bytes", len(got))
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
			if _, statErr := os.Lstat(entry); !errors.Is(statErr, os.ErrNotExist) {
				t.Errorf("a failed download must not be cached: %v", statErr)
			}
		})
	}
}

func TestFetch_ReportsAReadFailure(t *testing.T) {
	boom := errors.New("boom")
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(iotest.ErrReader(boom)),
			Request:    r,
		}, nil
	})}
	f := schema.New(
		schema.WithBaseURL("http://schemas.invalid"),
		schema.WithCacheDir(t.TempDir()),
		schema.WithClient(client),
	)

	_, err := f.EntityV1(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("error %v does not wrap the read failure", err)
	}
	if want := "reading schema http://schemas.invalid/entity/v1.json: boom"; err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

func TestFetch_RejectsABaseURLThatIsNotAURL(t *testing.T) {
	f := schema.New(schema.WithBaseURL("http://127.0.0.1/%zz"), schema.WithCacheDir(t.TempDir()))

	_, err := f.EntityV1(context.Background())
	if err == nil {
		t.Fatal("expected an error")
	}
	if want := "creating request for http://127.0.0.1/%zz/entity/v1.json: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("error = %q, want prefix %q", err, want)
	}
}

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
