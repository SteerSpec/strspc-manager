package httpsonly_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/internal/httpsonly"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		url    string
		secure bool
	}{
		{"https://api.github.com", true},
		{"HTTPS://example.com/x", true},
		{"https://example.com:8443", true},
		{"http://localhost", true},
		{"http://LOCALHOST:8080/api", true},
		{"http://127.0.0.1:1234", true},
		{"http://127.8.9.10", true},
		{"http://[::1]:8080", true},
		{"http://example.com", false},
		{"http://localhost.example.com", false},
		{"http://127.0.0.1.evil.example", false},
		{"http://localhost@evil.example", false},
		{"http://10.0.0.1", false},
		{"http://[::2]", false},
		{"ftp://example.com", false},
		{"//example.com", false},
		{"example.com", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			err := httpsonly.Validate(tt.url)
			if tt.secure && err != nil {
				t.Errorf("Validate(%q) = %v, want nil", tt.url, err)
			}
			if !tt.secure && !errors.Is(err, httpsonly.ErrInsecureURL) {
				t.Errorf("Validate(%q) = %v, want ErrInsecureURL", tt.url, err)
			}
		})
	}
}

func TestValidate_ParseError(t *testing.T) {
	err := httpsonly.Validate("http://[::1")
	if err == nil || errors.Is(err, httpsonly.ErrInsecureURL) {
		t.Errorf("Validate(malformed) = %v, want a parse error", err)
	}
}

func TestCheck_RedactsPassword(t *testing.T) {
	u, _ := url.Parse("http://user:secret@example.com")
	err := httpsonly.Check(u)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("Check = %v, want an error without the password", err)
	}
}

func redirectTo(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, target, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestCheckRedirect(t *testing.T) {
	via := []*http.Request{redirectTo(t, "https://api.github.com/x")}
	if err := httpsonly.CheckRedirect(redirectTo(t, "https://codeload.github.com/y"), via); err != nil {
		t.Errorf("https redirect refused: %v", err)
	}
	if err := httpsonly.CheckRedirect(redirectTo(t, "http://127.0.0.1/y"), via); err != nil {
		t.Errorf("loopback http redirect refused: %v", err)
	}
	if err := httpsonly.CheckRedirect(redirectTo(t, "http://codeload.github.com/y"), via); !errors.Is(err, httpsonly.ErrInsecureURL) {
		t.Errorf("http redirect = %v, want ErrInsecureURL", err)
	}
}

func TestCheckRedirect_KeepsTheDefaultLimit(t *testing.T) {
	next := redirectTo(t, "https://example.com/")
	via := make([]*http.Request, 9)
	if err := httpsonly.CheckRedirect(next, via); err != nil {
		t.Errorf("9th redirect refused: %v", err)
	}
	via = append(via, next)
	if err := httpsonly.CheckRedirect(next, via); err == nil || !strings.Contains(err.Error(), "10 redirects") {
		t.Errorf("11th request = %v, want the 10-redirect limit", err)
	}
}

func TestClient_Default(t *testing.T) {
	c := httpsonly.Client(nil, time.Minute)
	if c.Timeout != time.Minute || c.CheckRedirect == nil {
		t.Errorf("Client(nil) = %+v, want a 1m timeout and a CheckRedirect", c)
	}
}

func TestClient_CopiesAClientWithoutCheckRedirect(t *testing.T) {
	jar := &noJar{}
	in := &http.Client{Timeout: time.Second, Transport: http.DefaultTransport, Jar: jar}
	c := httpsonly.Client(in, time.Hour)
	if c == in || in.CheckRedirect != nil {
		t.Fatal("Client must copy, not modify, the caller's client")
	}
	if c.Timeout != time.Second || c.Transport != in.Transport || c.Jar != jar {
		t.Errorf("copy = %+v, want the caller's timeout, transport and jar", c)
	}
	if err := c.CheckRedirect(redirectTo(t, "http://example.com/"), nil); !errors.Is(err, httpsonly.ErrInsecureURL) {
		t.Errorf("CheckRedirect = %v, want ErrInsecureURL", err)
	}
}

func TestClient_ChainsTheCallersCheckRedirect(t *testing.T) {
	stop := errors.New("caller's policy")
	calls := 0
	in := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		calls++
		return stop
	}}
	c := httpsonly.Client(in, 0)
	if err := c.CheckRedirect(redirectTo(t, "http://example.com/"), nil); !errors.Is(err, httpsonly.ErrInsecureURL) || calls != 0 {
		t.Errorf("insecure redirect = %v after %d caller calls, want ErrInsecureURL before the caller runs", err, calls)
	}
	if err := c.CheckRedirect(redirectTo(t, "https://example.com/"), nil); !errors.Is(err, stop) || calls != 1 {
		t.Errorf("secure redirect = %v after %d caller calls, want the caller's verdict", err, calls)
	}
}

type noJar struct{}

func (*noJar) SetCookies(*url.URL, []*http.Cookie) {}
func (*noJar) Cookies(*url.URL) []*http.Cookie     { return nil }

func get(t *testing.T, c *http.Client, target string) (string, error) {
	t.Helper()
	resp, err := c.Do(redirectTo(t, target))
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

func TestClient_RefusesAnHTTPSToHTTPRedirect(t *testing.T) {
	// httptest servers are all on loopback, so the insecure target must be
	// a non-loopback name. CheckRedirect refuses it before any dial.
	srv := httptest.NewTLSServer(http.RedirectHandler("http://example.invalid/tarball", http.StatusFound))
	defer srv.Close()

	_, err := get(t, httpsonly.Client(srv.Client(), 0), srv.URL)
	if !errors.Is(err, httpsonly.ErrInsecureURL) {
		t.Fatalf("err = %v, want ErrInsecureURL", err)
	}
}

func TestClient_FollowsAnHTTPSToHTTPSRedirect(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "arrived")
	}))
	defer target.Close()
	srv := httptest.NewTLSServer(http.RedirectHandler(target.URL+"/x", http.StatusFound))
	defer srv.Close()

	// Both servers share httptest's certificate, so either client trusts both.
	body, err := get(t, httpsonly.Client(srv.Client(), 0), srv.URL)
	if err != nil || body != "arrived" {
		t.Fatalf("got %q, %v; want the redirect followed", body, err)
	}
}

func TestClient_FollowsALoopbackHTTPRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "arrived")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.RedirectHandler(target.URL+"/x", http.StatusFound))
	defer srv.Close()

	body, err := get(t, httpsonly.Client(nil, time.Minute), srv.URL)
	if err != nil || body != "arrived" {
		t.Fatalf("got %q, %v; want the redirect followed", body, err)
	}
}
