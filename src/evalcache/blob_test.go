package evalcache_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/SteerSpec/strspc-manager/src/evalcache"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

func TestStore_ImplementsBlobStore(t *testing.T) {
	var _ ruleeval.BlobStore = (*evalcache.Store)(nil)
}

// TestBlob_RoundTripsApartFromVerdicts checks a value survives reopening,
// and that a value and a verdict under the same key do not collide.
func TestBlob_RoundTripsApartFromVerdicts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	s, err := evalcache.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.GetBlob("k"); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.PutBlobs(map[string][]byte{"k": []byte(`{"findings":[]}`)}); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("k", sample()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = evalcache.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	v, ok, err := s.GetBlob("k")
	if err != nil || !ok || string(v) != `{"findings":[]}` {
		t.Fatalf("GetBlob = %q, %v, %v", v, ok, err)
	}
	if r, ok, err := s.Get("k"); err != nil || !ok || r.Verdict != ruleeval.Violated {
		t.Fatalf("the verdict under the same key = %+v, %v, %v", r, ok, err)
	}
}

// TestBlob_ExpiresAndIsPruned checks a value follows the TTL, and that
// Prune removes expired values along with expired verdicts.
func TestBlob_ExpiresAndIsPruned(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	s, err := evalcache.Open(context.Background(), filepath.Join(t.TempDir(), "cache.db"),
		evalcache.WithTTL(10*time.Minute), evalcache.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	_ = s.PutBlobs(map[string][]byte{"old": []byte("a")})
	_ = s.Put("old", sample())
	clock.now = clock.now.Add(8 * time.Minute)
	_ = s.PutBlobs(map[string][]byte{"fresh": []byte("b")})
	clock.now = clock.now.Add(3 * time.Minute)

	if _, ok, err := s.GetBlob("old"); ok || err != nil {
		t.Fatalf("an expired value must miss: ok=%v err=%v", ok, err)
	}
	if n, err := s.Prune(); err != nil || n != 2 {
		t.Fatalf("Prune = %d, %v; want the old value and the old verdict", n, err)
	}
	if v, ok, _ := s.GetBlob("fresh"); !ok || string(v) != "b" {
		t.Fatal("a fresh value must survive prune")
	}
}

// TestBlob_UndecodableValue checks a value that does not decode is an
// error on read and is removed by Prune, as is a verdict whose response
// does not decode, though its creation time does.
func TestBlob_UndecodableValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	s, err := evalcache.Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		if err := tx.Bucket([]byte("eval/v1")).Put([]byte("bad"), []byte(`{"created_at":"2026-11-08T00:00:00Z","response":"oops"}`)); err != nil {
			return err
		}
		return tx.Bucket([]byte("blob/v1")).Put([]byte("bad"), []byte("not json"))
	}); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	s, err = evalcache.Open(context.Background(), path, evalcache.WithTTL(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if _, _, err := s.GetBlob("bad"); err == nil {
		t.Error("an undecodable value should be an error")
	}
	if _, _, err := s.Get("bad"); err == nil {
		t.Error("an undecodable verdict should be an error")
	}
	if n, err := s.Prune(); err != nil || n != 2 {
		t.Errorf("Prune = %d, %v; want the undecodable value and verdict removed", n, err)
	}
}

// TestBlob_PutsAtOnce checks every value of one call is stored.
func TestBlob_PutsAtOnce(t *testing.T) {
	s, err := evalcache.Open(context.Background(), filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.PutBlobs(map[string][]byte{"a": []byte("1"), "b": []byte("2")}); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]string{"a": "1", "b": "2"} {
		if v, ok, err := s.GetBlob(k); err != nil || !ok || string(v) != want {
			t.Errorf("GetBlob(%q) = %q, %v, %v", k, v, ok, err)
		}
	}
}

func TestBlob_AfterClose(t *testing.T) {
	s, err := evalcache.Open(context.Background(), filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, _, err := s.GetBlob("k"); err == nil {
		t.Error("GetBlob after Close should fail")
	}
	if err := s.PutBlobs(map[string][]byte{"k": []byte("v")}); err == nil {
		t.Error("PutBlob after Close should fail")
	}
}
