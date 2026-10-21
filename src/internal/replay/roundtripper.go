// Package replay provides an http.RoundTripper that records HTTP exchanges
// to a JSON cassette and replays them later without network access.
//
// Interactions are keyed by sha256(method + path + canonical JSON body), so
// a replayed request must be byte-for-byte equivalent (modulo JSON key
// order and whitespace) to the recorded one. Each key holds an ordered list
// of responses; repeated identical requests (for example an SDK retry after
// a 429) consume them in sequence and the last one repeats once exhausted.
//
// Request headers are never stored. Response headers are reduced to a small
// allow-list so credentials and cookies cannot leak into cassettes.
package replay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Mode selects record or replay behaviour.
type Mode int

// Modes.
const (
	// ModeReplay serves responses from the cassette and never touches the
	// network.
	ModeReplay Mode = iota
	// ModeRecord forwards requests to the upstream transport and saves the
	// exchanges.
	ModeRecord
)

// EnvRecord is the environment variable that switches tests to record mode.
const EnvRecord = "STRSPC_RECORD"

// ErrMissingInteraction is returned in replay mode when no recorded
// interaction matches the request.
var ErrMissingInteraction = errors.New("replay: no recorded interaction for request")

// responseHeaderAllowList lists the response headers kept in cassettes.
var responseHeaderAllowList = []string{
	"Content-Type",
	"Request-Id",
	"Retry-After",
	"Retry-After-Ms",
	"X-Should-Retry",
	"Anthropic-Ratelimit-Requests-Remaining",
}

// Cassette is the on-disk format.
type Cassette struct {
	// Comment is optional free text describing the cassette, set with
	// SetComment.
	Comment string `json:"comment,omitempty"`
	// Interactions lists the recorded request shapes with their responses, in
	// first-seen order.
	Interactions []Interaction `json:"interactions"`
}

// Interaction is one request shape and the ordered responses it received.
type Interaction struct {
	// Request is the header-less request shape that keys this interaction.
	Request Request `json:"request"`
	// Responses holds the responses received for Request, in the order they
	// were recorded.
	Responses []Response `json:"responses"`
}

// Request is the stored, header-less request. Body holds the canonical
// JSON (or the raw body when it is not JSON).
type Request struct {
	// Method is the HTTP method.
	Method string `json:"method"`
	// Path is the URL path of the request, without host or query string.
	Path string `json:"path"`
	// Body is the canonical JSON body, or the raw body when it is not JSON.
	Body string `json:"body"`
}

// Response is a stored response.
type Response struct {
	// Status is the HTTP status code.
	Status int `json:"status"`
	// Headers holds the allow-listed response headers.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the raw response body.
	Body string `json:"body"`
}

// Transport is the record/replay http.RoundTripper.
type Transport struct {
	path     string
	mode     Mode
	upstream http.RoundTripper

	mu       sync.Mutex
	cassette Cassette
	index    map[string]int // key → position in cassette.Interactions
	cursor   map[string]int // key → next response to serve (replay)
	requests int
}

// ModeFromEnv returns ModeRecord when STRSPC_RECORD=1, else ModeReplay.
func ModeFromEnv() Mode {
	if os.Getenv(EnvRecord) == "1" {
		return ModeRecord
	}
	return ModeReplay
}

// New creates a Transport for the cassette at path. In replay mode the
// cassette must exist. In record mode upstream is required; an existing
// cassette is loaded so new interactions are appended to it.
func New(path string, mode Mode, upstream http.RoundTripper) (*Transport, error) {
	t := &Transport{
		path:     path,
		mode:     mode,
		upstream: upstream,
		index:    map[string]int{},
		cursor:   map[string]int{},
	}
	switch mode {
	case ModeRecord:
		if upstream == nil {
			return nil, errors.New("replay: record mode requires an upstream transport")
		}
		if err := t.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	case ModeReplay:
		if err := t.load(); err != nil {
			return nil, fmt.Errorf("replay: cannot load cassette %s (run with %s=1 and an API key to record): %w", path, EnvRecord, err)
		}
	default:
		return nil, fmt.Errorf("replay: unknown mode %d", mode)
	}
	return t, nil
}

func (t *Transport) load() error {
	raw, err := os.ReadFile(t.path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &t.cassette); err != nil {
		return fmt.Errorf("replay: parse cassette %s: %w", t.path, err)
	}
	for i, in := range t.cassette.Interactions {
		t.index[Key(in.Request.Method, in.Request.Path, []byte(in.Request.Body))] = i
	}
	return nil
}

// Key computes the interaction key for a request.
func Key(method, path string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{0})
	h.Write([]byte(path))
	h.Write([]byte{0})
	h.Write([]byte(canonical(body)))
	return hex.EncodeToString(h.Sum(nil))
}

// canonical re-encodes JSON with sorted keys and no insignificant
// whitespace. Non-JSON input is returned verbatim.
func canonical(body []byte) string {
	if len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	var v any
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return string(body)
	}
	out, err := json.Marshal(v)
	if err != nil {
		return string(body)
	}
	return string(out)
}

// Requests returns the number of round trips seen so far.
func (t *Transport) Requests() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.requests
}

// RoundTrip implements http.RoundTripper.
func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		var err error
		body, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("replay: read request body: %w", err)
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	key := Key(req.Method, req.URL.Path, body)

	t.mu.Lock()
	t.requests++
	t.mu.Unlock()

	if t.mode == ModeReplay {
		return t.replay(req, key, body)
	}
	return t.record(req, key, body)
}

func (t *Transport) replay(req *http.Request, key string, body []byte) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	i, ok := t.index[key]
	if !ok || len(t.cassette.Interactions[i].Responses) == 0 {
		return nil, fmt.Errorf("%w: %s %s body=%s", ErrMissingInteraction, req.Method, req.URL.Path, snippet(canonical(body)))
	}
	responses := t.cassette.Interactions[i].Responses
	n := t.cursor[key]
	if n >= len(responses) {
		n = len(responses) - 1
	}
	t.cursor[key] = n + 1
	return build(req, responses[n]), nil
}

func (t *Transport) record(req *http.Request, key string, body []byte) (*http.Response, error) {
	resp, err := t.upstream.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("replay: read upstream response: %w", err)
	}
	stored := Response{Status: resp.StatusCode, Headers: map[string]string{}, Body: string(raw)}
	for _, h := range responseHeaderAllowList {
		if v := resp.Header.Get(h); v != "" {
			stored.Headers[strings.ToLower(h)] = v
		}
	}

	t.mu.Lock()
	i, ok := t.index[key]
	if !ok {
		t.cassette.Interactions = append(t.cassette.Interactions, Interaction{
			Request: Request{Method: req.Method, Path: req.URL.Path, Body: canonical(body)},
		})
		i = len(t.cassette.Interactions) - 1
		t.index[key] = i
	}
	t.cassette.Interactions[i].Responses = append(t.cassette.Interactions[i].Responses, stored)
	t.mu.Unlock()

	resp.Body = io.NopCloser(bytes.NewReader(raw))
	return resp, nil
}

// SetComment sets the free-text comment stored at the top of the cassette,
// for example to mark it as synthetic.
func (t *Transport) SetComment(c string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cassette.Comment = c
}

// Save writes the cassette to disk (record mode). It is a no-op in replay
// mode.
func (t *Transport) Save() error {
	if t.mode != ModeRecord {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return WriteCassette(t.path, &t.cassette)
}

// WriteCassette writes c to path as indented JSON with a trailing newline,
// creating parent directories as needed. Interactions are sorted by
// request so that concurrent recordings produce a stable file.
func WriteCassette(path string, c *Cassette) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	sort.SliceStable(c.Interactions, func(i, j int) bool {
		a, b := c.Interactions[i].Request, c.Interactions[j].Request
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		return a.Body < b.Body
	})
	out, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o644)
}

func build(req *http.Request, r Response) *http.Response {
	h := http.Header{}
	for k, v := range r.Headers {
		h.Set(k, v)
	}
	return &http.Response{
		Status:        fmt.Sprintf("%d %s", r.Status, http.StatusText(r.Status)),
		StatusCode:    r.Status,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        h,
		Body:          io.NopCloser(strings.NewReader(r.Body)),
		ContentLength: int64(len(r.Body)),
		Request:       req,
	}
}

func snippet(s string) string {
	const limit = 400
	if len(s) > limit {
		return s[:limit] + "…"
	}
	return s
}
