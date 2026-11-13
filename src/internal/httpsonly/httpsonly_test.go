package httpsonly_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
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
	tests := []struct {
		target string
		secure bool
	}{
		{"https://codeload.github.com/y", true},
		{"http://127.0.0.1/y", true},
		{"http://codeload.github.com/y", false},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			err := httpsonly.CheckRedirect(redirectTo(t, tt.target), via)
			if tt.secure && err != nil {
				t.Errorf("redirect to %s refused: %v", tt.target, err)
			}
			if !tt.secure && !errors.Is(err, httpsonly.ErrInsecureURL) {
				t.Errorf("redirect to %s = %v, want ErrInsecureURL", tt.target, err)
			}
		})
	}
}

func TestCheckRedirect_KeepsTheDefaultLimit(t *testing.T) {
	tests := []struct {
		hops    int // requests already made
		refused bool
	}{
		{9, false},
		{10, true},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.hops), func(t *testing.T) {
			err := httpsonly.CheckRedirect(redirectTo(t, "https://example.com/"), make([]*http.Request, tt.hops))
			if refused := err != nil && strings.Contains(err.Error(), "10 redirects"); refused != tt.refused {
				t.Errorf("after %d requests: err = %v, want refused = %v", tt.hops, err, tt.refused)
			}
		})
	}
}

func TestClient_Default(t *testing.T) {
	c := httpsonly.Client(nil, time.Minute)
	if c.Timeout != time.Minute || c.CheckRedirect == nil {
		t.Errorf("Client(nil) = %+v, want a 1m timeout and a CheckRedirect", c)
	}
}

func TestClient_CopiesAClientWithoutCheckRedirect(t *testing.T) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
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

// The HTTPS check runs first; the caller's own CheckRedirect decides only
// for a secure target.
func TestClient_ChainsTheCallersCheckRedirect(t *testing.T) {
	stop := errors.New("caller's policy")
	tests := []struct {
		target     string
		want       error
		callerRuns bool
	}{
		{"http://example.com/", httpsonly.ErrInsecureURL, false},
		{"https://example.com/", stop, true},
	}
	for _, tt := range tests {
		t.Run(tt.target, func(t *testing.T) {
			ran := false
			c := httpsonly.Client(&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
				ran = true
				return stop
			}}, 0)
			err := c.CheckRedirect(redirectTo(t, tt.target), nil)
			if !errors.Is(err, tt.want) || ran != tt.callerRuns {
				t.Errorf("err = %v, caller ran = %v; want %v, caller ran = %v", err, ran, tt.want, tt.callerRuns)
			}
		})
	}
}

// get sends a GET for target through c and returns the body.
func get(ctx context.Context, c *http.Client, target string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.Do(req)
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

	_, err := get(context.Background(), httpsonly.Client(srv.Client(), 0), srv.URL)
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
	body, err := get(context.Background(), httpsonly.Client(srv.Client(), 0), srv.URL)
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

	body, err := get(context.Background(), httpsonly.Client(nil, time.Minute), srv.URL)
	if err != nil || body != "arrived" {
		t.Fatalf("got %q, %v; want the redirect followed", body, err)
	}
}
