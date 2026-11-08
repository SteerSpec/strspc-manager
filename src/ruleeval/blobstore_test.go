package ruleeval_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// blobCache is a memCache that also keeps provider values, or fails to
// when err is set.
type blobCache struct {
	*memCache
	mu    sync.Mutex
	blobs map[string][]byte
	err   error
}

func newBlobCache(err error) *blobCache {
	return &blobCache{memCache: newMemCache(), blobs: map[string][]byte{}, err: err}
}

func (c *blobCache) GetBlob(key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return nil, false, c.err
	}
	v, ok := c.blobs[key]
	return v, ok, nil
}

func (c *blobCache) PutBlobs(values map[string][]byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	for k, v := range values {
		c.blobs[k] = v
	}
	return nil
}

// storeProvider is an auditProvider that writes a value through the
// request's Store and reads it back, recording what it saw.
type storeProvider struct {
	auditProvider
	mu      sync.Mutex
	stores  []bool // whether each request carried a Store
	got     []string
	putErrs []error
}

func (s *storeProvider) EvaluateBatch(ctx context.Context, req ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	s.mu.Lock()
	s.stores = append(s.stores, req.Store != nil)
	if req.Store != nil {
		s.putErrs = append(s.putErrs, req.Store.PutBlobs(map[string][]byte{"k": []byte("v")}))
		v, ok, err := req.Store.GetBlob("k")
		if err != nil {
			s.got = append(s.got, "error")
		} else if ok {
			s.got = append(s.got, string(v))
		} else {
			s.got = append(s.got, "miss")
		}
	}
	s.mu.Unlock()
	return s.auditProvider.EvaluateBatch(ctx, req)
}

func newStoreProvider() *storeProvider {
	return &storeProvider{auditProvider: auditProvider{
		batchProvider: &batchProvider{responses: map[string]*ruleeval.EvalResponse{"CMD-001": {Verdict: ruleeval.Compliant}}},
		version:       "audit-test",
	}}
}

// TestAudit_ForwardsBlobStore checks an audit hands the provider the
// cache's BlobStore, and a diff evaluation does not.
func TestAudit_ForwardsBlobStore(t *testing.T) {
	p := newStoreProvider()
	cache := newBlobCache(nil)
	ev, err := ruleeval.New(p, ruleeval.WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Audit(context.Background(), auditInputs(), auditFiles); err != nil {
		t.Fatal(err)
	}
	ev.Evaluate(context.Background(), auditInputs(), "diff --git a/x b/x\n")
	if len(p.stores) != 2 || !p.stores[0] || p.stores[1] {
		t.Fatalf("stores = %v, want one for the audit only", p.stores)
	}
	if len(p.got) != 1 || p.got[0] != "v" || string(cache.blobs["k"]) != "v" {
		t.Errorf("the audit's Store should reach the cache: got %v, cache %q", p.got, cache.blobs["k"])
	}

	// A cache that cannot keep provider values forwards none.
	p = newStoreProvider()
	ev, err = ruleeval.New(p, ruleeval.WithCache(newMemCache()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Audit(context.Background(), auditInputs(), auditFiles); err != nil {
		t.Fatal(err)
	}
	if len(p.stores) != 1 || p.stores[0] {
		t.Errorf("stores = %v, want none without a BlobStore", p.stores)
	}
}

// TestAudit_BlobStoreFailureIsObserved checks a failing store reaches the
// provider as an empty one, is reported to the Observer, and does not keep
// the verdicts out of the cache.
func TestAudit_BlobStoreFailureIsObserved(t *testing.T) {
	p := newStoreProvider()
	cache := newBlobCache(errors.New("disk full"))
	var mu sync.Mutex
	var cacheErrs []ruleeval.Event
	ev, err := ruleeval.New(p, ruleeval.WithCache(cache), ruleeval.WithObserver(ruleeval.ObserverFunc(func(e ruleeval.Event) {
		if e.Kind == ruleeval.EventCacheError {
			mu.Lock()
			cacheErrs = append(cacheErrs, e)
			mu.Unlock()
		}
	})))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ev.Audit(context.Background(), auditInputs(), auditFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.got) != 1 || p.got[0] != "miss" || p.putErrs[0] != nil {
		t.Errorf("the provider should see a miss and no error: got %v, put %v", p.got, p.putErrs)
	}
	if len(cacheErrs) != 2 || cacheErrs[0].EntityID != "CMD" || cacheErrs[0].Err == nil || cacheErrs[0].RuleID != "" {
		t.Errorf("cache errors = %+v, want the put and the get, for entity CMD", cacheErrs)
	}
	if cache.puts != 1 {
		t.Errorf("verdict puts = %d, want the verdict cached all the same", cache.puts)
	}
	if len(res.Diagnostics) != 0 {
		t.Errorf("diagnostics = %+v, want none for a compliant rule", res.Diagnostics)
	}
}
