package replay_test

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
	"sync/atomic"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/internal/replay"
)

func post(t *testing.T, client *http.Client, url, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("x-api-key", "sk-ant-secret")
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readAll(t *testing.T, r io.ReadCloser) string {
	t.Helper()
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestKey_CanonicalisesJSON(t *testing.T) {
	a := replay.Key("POST", "/v1/messages", []byte(`{"b": 1, "a": [1, 2]}`))
	b := replay.Key("POST", "/v1/messages", []byte(`{"a":[1,2],"b":1}`))
	if a != b {
		t.Error("key must not depend on JSON key order or whitespace")
	}
	if len(a) != 64 {
		t.Errorf("expected sha256 hex, got %q", a)
	}
	if replay.Key("POST", "/v1/messages", []byte(`{"a":1}`)) == a {
		t.Error("different bodies must have different keys")
	}
	if replay.Key("GET", "/v1/messages", []byte(`{"a":[1,2],"b":1}`)) == a {
		t.Error("method must affect key")
	}
	if replay.Key("POST", "/v1/other", []byte(`{"a":[1,2],"b":1}`)) == a {
		t.Error("path must affect key")
	}
	// Non-JSON bodies are hashed verbatim.
	if replay.Key("POST", "/p", []byte("raw")) == replay.Key("POST", "/p", []byte("raw ")) {
		t.Error("non-JSON bodies must be hashed verbatim")
	}
}

func TestRecordThenReplay(t *testing.T) {
	var hits atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		if r.Header.Get("x-api-key") == "" {
			t.Error("record mode must forward request headers upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("request-id", "req_123")
		w.Header().Set("Set-Cookie", "session=leak")
		if n == 1 {
			w.Header().Set("retry-after-ms", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"rate_limit_error"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true,"n":`+string(rune('0'+n))+`}`)
	}))
	defer upstream.Close()

	cassette := filepath.Join(t.TempDir(), "cassettes", "flow.json")
	rec, err := replay.New(cassette, replay.ModeRecord, http.DefaultTransport)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: rec}
	body := `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

	r1 := post(t, client, upstream.URL+"/v1/messages", body)
	if r1.StatusCode != 429 {
		t.Fatalf("first upstream response = %d", r1.StatusCode)
	}
	_ = readAll(t, r1.Body)
	r2 := post(t, client, upstream.URL+"/v1/messages", body)
	if r2.StatusCode != 200 || readAll(t, r2.Body) != `{"ok":true,"n":2}` {
		t.Fatalf("second upstream response = %d", r2.StatusCode)
	}
	r3 := post(t, client, upstream.URL+"/v1/messages", `{"other":true}`)
	_ = readAll(t, r3.Body)
	if rec.Requests() != 3 {
		t.Errorf("Requests() = %d, want 3", rec.Requests())
	}
	if err := rec.Save(); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(cassette)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"sk-ant-secret", "secret-token", "session=leak"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Errorf("cassette leaks %q", secret)
		}
	}
	if !bytes.Contains(raw, []byte("req_123")) || !bytes.Contains(raw, []byte("retry-after-ms")) {
		t.Error("cassette should keep allow-listed response headers")
	}

	// Replay: no upstream at all. Same two requests must yield 429 then 200
	// in order, and the third request its own response.
	rp, err := replay.New(cassette, replay.ModeReplay, nil)
	if err != nil {
		t.Fatal(err)
	}
	client = &http.Client{Transport: rp}
	p1 := post(t, client, "https://api.anthropic.com/v1/messages", body)
	_ = readAll(t, p1.Body)
	if p1.StatusCode != 429 || p1.Header.Get("retry-after-ms") != "1" {
		t.Fatalf("replay 1 = %d %v", p1.StatusCode, p1.Header)
	}
	p2 := post(t, client, "https://api.anthropic.com/v1/messages", body)
	if p2.StatusCode != 200 || readAll(t, p2.Body) != `{"ok":true,"n":2}` {
		t.Fatalf("replay 2 = %d", p2.StatusCode)
	}
	// Exhausted sequences repeat their last response.
	p2b := post(t, client, "https://api.anthropic.com/v1/messages", body)
	if p2b.StatusCode != 200 {
		t.Fatalf("replay 2b = %d, want last response repeated", p2b.StatusCode)
	}
	_ = readAll(t, p2b.Body)
	p3 := post(t, client, "https://api.anthropic.com/v1/messages", `{ "other" : true }`)
	if p3.StatusCode != 200 || readAll(t, p3.Body) != `{"ok":true,"n":3}` {
		t.Fatalf("replay 3 = %d", p3.StatusCode)
	}
	if rp.Requests() != 4 {
		t.Errorf("Requests() = %d, want 4", rp.Requests())
	}
	if hits.Load() != 3 {
		t.Errorf("replay must not touch upstream; hits = %d", hits.Load())
	}
}

func TestReplay_MissingInteraction(t *testing.T) {
	cassette := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(cassette, []byte(`{"interactions":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	rp, err := replay.New(cassette, replay.ModeReplay, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.anthropic.com/v1/messages", strings.NewReader(`{"x":1}`))
	_, err = rp.RoundTrip(req)
	if !errors.Is(err, replay.ErrMissingInteraction) {
		t.Fatalf("expected ErrMissingInteraction, got %v", err)
	}
	if !strings.Contains(err.Error(), "/v1/messages") || !strings.Contains(err.Error(), `{"x":1}`) {
		t.Errorf("error should name the request, got %q", err)
	}
}

func TestReplay_MissingCassetteFile(t *testing.T) {
	_, err := replay.New(filepath.Join(t.TempDir(), "nope.json"), replay.ModeReplay, nil)
	if err == nil {
		t.Fatal("replay mode with a missing cassette must fail at construction")
	}
}

func TestRecord_RequiresUpstream(t *testing.T) {
	_, err := replay.New(filepath.Join(t.TempDir(), "c.json"), replay.ModeRecord, nil)
	if err == nil {
		t.Fatal("record mode without upstream must fail")
	}
}

func TestModeFromEnv(t *testing.T) {
	t.Setenv("STRSPC_RECORD", "")
	if replay.ModeFromEnv() != replay.ModeReplay {
		t.Error("unset → replay")
	}
	t.Setenv("STRSPC_RECORD", "1")
	if replay.ModeFromEnv() != replay.ModeRecord {
		t.Error("STRSPC_RECORD=1 → record")
	}
	t.Setenv("STRSPC_RECORD", "0")
	if replay.ModeFromEnv() != replay.ModeReplay {
		t.Error("STRSPC_RECORD=0 → replay")
	}
}

func TestRecord_UpstreamError(t *testing.T) {
	cassette := filepath.Join(t.TempDir(), "c.json")
	rec, err := replay.New(cassette, replay.ModeRecord, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	}))
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://x/", nil)
	if _, err := rec.RoundTrip(req); err == nil || !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected upstream error to propagate, got %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
