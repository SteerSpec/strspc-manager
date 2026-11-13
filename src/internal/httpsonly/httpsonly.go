// Package httpsonly keeps the manager's HTTP clients on HTTPS. A URL must use
// https, or http to a loopback host (localhost, 127.0.0.0/8, ::1), which keeps
// local test servers and local proxies working. The rule covers both the URL
// a request is sent to and every redirect it follows.
package httpsonly

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxRedirects matches net/http's default redirect limit, which a custom
// CheckRedirect replaces.
const maxRedirects = 10

// ErrInsecureURL is wrapped into errors for a URL that is neither https nor
// http to a loopback host.
var ErrInsecureURL = errors.New("insecure URL: HTTPS is required unless the host is loopback")

// Validate parses raw and reports an error wrapping ErrInsecureURL unless it
// passes Check.
func Validate(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", raw, err)
	}
	return Check(u)
}

// Check reports an error wrapping ErrInsecureURL unless u is https, or http
// to a loopback host.
func Check(u *url.URL) error {
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		if IsLoopback(u.Hostname()) {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrInsecureURL, u.Redacted())
}

// IsLoopback reports whether host is "localhost" or a loopback IP literal. It
// never resolves names, so a name that merely resolves to 127.0.0.1 does not
// count.
func IsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// CheckRedirect is an http.Client.CheckRedirect that refuses a redirect to an
// insecure URL and, like net/http's default, stops after 10 redirects.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	if err := Check(req.URL); err != nil {
		return fmt.Errorf("refusing redirect: %w", err)
	}
	return nil
}

// Client returns a client that refuses insecure redirects. A nil c yields a
// new client with the given timeout. A non-nil c is copied, never modified:
// the copy keeps c's Transport, Jar and Timeout (timeout is ignored), and runs
// c's own CheckRedirect, or the default 10-redirect limit when it has none,
// after the HTTPS check.
func Client(c *http.Client, timeout time.Duration) *http.Client {
	if c == nil {
		return &http.Client{Timeout: timeout, CheckRedirect: CheckRedirect}
	}
	cp := *c
	inner := c.CheckRedirect
	if inner == nil {
		cp.CheckRedirect = CheckRedirect
		return &cp
	}
	cp.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if err := Check(req.URL); err != nil {
			return fmt.Errorf("refusing redirect: %w", err)
		}
		return inner(req, via)
	}
	return &cp
}
