package evalcache_test

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/evalcache"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

func sample() *ruleeval.EvalResponse {
	return &ruleeval.EvalResponse{
		Verdict:     ruleeval.Violated,
		Explanation: "missing Long",
		Confidence:  0.9,
		Evidence:    []ruleeval.Location{{File: "src/cmd/lint.go", Line: 12, Excerpt: "Short: \"Lint\""}},
	}
}

func TestOpen_CreatesParentDirAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".strspc", "cache.db")
	s, err := evalcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if len(s.Diagnostics().Diagnostics) != 0 {
		t.Errorf("fresh store should have no diagnostics, got %v", s.Diagnostics().Diagnostics)
	}

	if _, ok, err := s.Get("missing"); err != nil || ok {
		t.Fatalf("Get(missing) = ok=%v err=%v, want miss", ok, err)
	}
	if err := s.Put("k1", sample()); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Get("k1")
	if err != nil || !ok {
		t.Fatalf("Get(k1) = ok=%v err=%v", ok, err)
	}
	if !reflect.DeepEqual(got, sample()) {
		t.Errorf("round trip mismatch:\n got %+v\nwant %+v", got, sample())
	}

	// Reopen and read back: entries persist across processes.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := evalcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	if _, ok, _ := s2.Get("k1"); !ok {
		t.Error("entry should survive reopen")
	}
}

func TestStore_ImplementsCache(t *testing.T) {
	var _ ruleeval.Cache = (*evalcache.Store)(nil)
}

func TestTTL_ExpiresViaInjectedClock(t *testing.T) {
	clock := &fakeClock{now: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)}
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"),
		evalcache.WithTTL(time.Hour), evalcache.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	if err := s.Put("k", sample()); err != nil {
		t.Fatal(err)
	}
	clock.now = clock.now.Add(59 * time.Minute)
	if _, ok, _ := s.Get("k"); !ok {
		t.Fatal("entry should still be valid before TTL")
	}
	clock.now = clock.now.Add(2 * time.Minute)
	if _, ok, err := s.Get("k"); ok || err != nil {
		t.Fatalf("entry should be expired after TTL: ok=%v err=%v", ok, err)
	}
}

func TestTTL_ZeroNeverExpires(t *testing.T) {
	clock := &fakeClock{now: time.Unix(0, 0)}
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"), evalcache.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	_ = s.Put("k", sample())
	clock.now = clock.now.Add(100 * 365 * 24 * time.Hour)
	if _, ok, _ := s.Get("k"); !ok {
		t.Fatal("TTL 0 must mean entries never expire")
	}
	if n, err := s.Prune(); err != nil || n != 0 {
		t.Fatalf("Prune with TTL 0 = %d, %v; want 0, nil", n, err)
	}
}

func TestPrune_RemovesOnlyExpired(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_000_000, 0)}
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"),
		evalcache.WithTTL(10*time.Minute), evalcache.WithClock(clock.Now))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()

	_ = s.Put("old1", sample())
	_ = s.Put("old2", sample())
	clock.now = clock.now.Add(8 * time.Minute)
	_ = s.Put("fresh", sample())
	clock.now = clock.now.Add(3 * time.Minute) // old* are 11m old, fresh is 3m old

	n, err := s.Prune()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("Prune removed %d, want 2", n)
	}
	if _, ok, _ := s.Get("fresh"); !ok {
		t.Error("fresh entry must survive prune")
	}
	// Advance past fresh's expiry: it was never read back, so prune finds it.
	clock.now = clock.now.Add(10 * time.Minute)
	if n, _ := s.Prune(); n != 1 {
		t.Errorf("second Prune removed %d, want 1", n)
	}
}

func TestOpen_CorruptFileIsRecreated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	writeGarbage(t, path)

	s, err := evalcache.Open(path)
	if err != nil {
		t.Fatalf("Open on corrupt file should recreate, got %v", err)
	}
	defer func() { _ = s.Close() }()

	diags := s.Diagnostics().Diagnostics
	if len(diags) != 1 || diags[0].Code != evalcache.CodeRecreated || diags[0].Severity != result.Info {
		t.Fatalf("expected one Info %s diagnostic, got %v", evalcache.CodeRecreated, diags)
	}
	if err := s.Put("k", sample()); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Get("k"); !ok {
		t.Error("recreated store must be usable")
	}
}

func TestOpen_LockTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	a, err := evalcache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()

	start := time.Now()
	_, err = evalcache.Open(path, evalcache.WithLockTimeout(100*time.Millisecond))
	if !errors.Is(err, evalcache.ErrLocked) {
		t.Fatalf("second Open should fail with ErrLocked, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("lock timeout took %v, expected roughly 100ms", elapsed)
	}

	// Once the first holder closes, opening succeeds.
	_ = a.Close()
	b, err := evalcache.Open(path, evalcache.WithLockTimeout(100*time.Millisecond))
	if err != nil {
		t.Fatalf("Open after release: %v", err)
	}
	_ = b.Close()
}

func TestStore_AfterClose(t *testing.T) {
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, _, err := s.Get("k"); err == nil {
		t.Error("Get after Close should fail")
	}
	if err := s.Put("k", sample()); err == nil {
		t.Error("Put after Close should fail")
	}
	if err := s.Close(); err != nil {
		t.Errorf("double Close should be a no-op, got %v", err)
	}
}

func TestPut_RejectsNil(t *testing.T) {
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	if err := s.Put("k", nil); err == nil {
		t.Error("Put(nil) should fail")
	}
}

func TestClose_ConcurrentWithOperations(t *testing.T) {
	s, err := evalcache.Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Put("seed", sample())

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			for j := 0; j < 50; j++ {
				key := "k" + strconv.Itoa(i*100+j)
				// Every call must either succeed or return ErrClosed; never panic.
				if err := s.Put(key, sample()); err != nil && !errors.Is(err, evalcache.ErrClosed) {
					t.Errorf("Put: %v", err)
				}
				if _, _, err := s.Get("seed"); err != nil && !errors.Is(err, evalcache.ErrClosed) {
					t.Errorf("Get: %v", err)
				}
				if _, err := s.Prune(); err != nil && !errors.Is(err, evalcache.ErrClosed) {
					t.Errorf("Prune: %v", err)
				}
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		time.Sleep(time.Millisecond)
		if err := s.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := s.Close(); err != nil {
			t.Errorf("second Close: %v", err)
		}
	}()
	close(start)
	wg.Wait()

	if _, _, err := s.Get("seed"); !errors.Is(err, evalcache.ErrClosed) {
		t.Errorf("Get after Close = %v, want ErrClosed", err)
	}
}

func writeGarbage(t *testing.T, path string) {
	t.Helper()
	garbage := make([]byte, 8192)
	for i := range garbage {
		garbage[i] = byte(i*7 + 3)
	}
	if err := os.WriteFile(path, garbage, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestOpen_ConcurrentRecoveryKeepsOneDatabase(t *testing.T) {
	for round := 0; round < 5; round++ {
		path := filepath.Join(t.TempDir(), "cache.db")
		writeGarbage(t, path)

		const n = 4
		type outcome struct {
			s   *evalcache.Store
			err error
		}
		results := make([]outcome, n)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start
				s, err := evalcache.Open(path, evalcache.WithLockTimeout(200*time.Millisecond))
				results[i] = outcome{s, err}
			}(i)
		}
		close(start)
		wg.Wait()

		var winner *evalcache.Store
		for i, r := range results {
			switch {
			case r.err == nil:
				if winner != nil {
					t.Fatalf("round %d: two stores opened the same path at once (%d)", round, i)
				}
				winner = r.s
			case errors.Is(r.err, evalcache.ErrLocked):
				// lost the race: must degrade, never recover over a healthy file
			default:
				t.Fatalf("round %d: opener %d: unexpected error %v", round, i, r.err)
			}
		}
		if winner == nil {
			t.Fatalf("round %d: nobody recovered the corrupt file", round)
		}
		// The winner is usually the opener that rebuilt the file and reports
		// the recovery; it may also be an opener that found the healthy
		// replacement first (then the rebuilder lost its reopen race and
		// degraded with ErrLocked). Either way exactly one store is open
		// and it must not report anything but the recovery.
		if diags := winner.Diagnostics().Diagnostics; len(diags) > 1 || (len(diags) == 1 && diags[0].Code != evalcache.CodeRecreated) {
			t.Errorf("round %d: unexpected diagnostics on the winner: %v", round, diags)
		}
		if err := winner.Put("k", sample()); err != nil {
			t.Fatalf("round %d: winner Put: %v", round, err)
		}
		_ = winner.Close()

		// The surviving file is the one the winner wrote to: a fresh open
		// finds the entry and reports no recovery.
		s, err := evalcache.Open(path)
		if err != nil {
			t.Fatalf("round %d: reopen: %v", round, err)
		}
		if _, ok, _ := s.Get("k"); !ok {
			t.Fatalf("round %d: entry written by the winner was lost (file replaced underneath it)", round)
		}
		if len(s.Diagnostics().Diagnostics) != 0 {
			t.Errorf("round %d: healthy file must not be recovered again", round)
		}
		_ = s.Close()
		if _, err := os.Stat(path + ".recover.lock"); !os.IsNotExist(err) {
			t.Errorf("round %d: recovery lock file left behind (%v)", round, err)
		}
	}
}

func TestOpen_StaleRecoveryLockIsReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	writeGarbage(t, path)
	lock := path + ".recover.lock"
	if err := os.WriteFile(lock, []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	s, err := evalcache.Open(path, evalcache.WithLockTimeout(300*time.Millisecond))
	if err != nil {
		t.Fatalf("stale recovery lock must be reclaimed, got %v", err)
	}
	defer func() { _ = s.Close() }()
	if len(s.Diagnostics().Diagnostics) != 1 {
		t.Errorf("expected recovery diagnostic, got %v", s.Diagnostics().Diagnostics)
	}
}

func TestOpen_FreshRecoveryLockHeldElsewhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	writeGarbage(t, path)
	if err := os.WriteFile(path+".recover.lock", []byte("12345\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := evalcache.Open(path, evalcache.WithLockTimeout(150*time.Millisecond))
	if !errors.Is(err, evalcache.ErrLocked) {
		t.Fatalf("a fresh recovery lock held by another process should yield ErrLocked, got %v", err)
	}
}
