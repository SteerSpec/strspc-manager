// Package evalcache persists rule-eval verdicts in an embedded key-value
// store (bbolt) at .strspc/cache.db, keyed by ruleeval.CacheKey. Entries
// expire after a configurable TTL. The store implements ruleeval.Cache.
//
// Two failure modes are made explicit so that callers can degrade
// gracefully: ErrLocked when another process holds the database, and an
// Info diagnostic (CodeRecreated) when a corrupt file had to be replaced.
package evalcache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
	bolterrors "go.etcd.io/bbolt/errors"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

const module = "eval-cache"

// CodeRecreated is the Info diagnostic emitted when a corrupt cache file was
// discarded and recreated.
const CodeRecreated = "EC001"

// ErrLocked is returned (wrapped) by Open when another process holds the
// database and the lock timeout elapsed. Callers should continue without a
// cache rather than fail.
var ErrLocked = errors.New("evalcache: database is locked by another process")

var bucketName = []byte("eval/v1")

const (
	defaultLockTimeout = 2 * time.Second
	fileMode           = 0o600
)

type config struct {
	ttl         time.Duration
	now         func() time.Time
	lockTimeout time.Duration
}

// Option configures Open.
type Option func(*config)

// WithTTL sets how long an entry stays valid after it was written.
// Zero (the default) means entries never expire.
func WithTTL(d time.Duration) Option {
	return func(c *config) { c.ttl = d }
}

// WithClock replaces the time source used for expiry (tests).
func WithClock(now func() time.Time) Option {
	return func(c *config) {
		if now != nil {
			c.now = now
		}
	}
}

// WithLockTimeout sets how long Open waits for the file lock before
// returning ErrLocked. Defaults to two seconds.
func WithLockTimeout(d time.Duration) Option {
	return func(c *config) { c.lockTimeout = d }
}

// Store is a bbolt-backed verdict cache.
type Store struct {
	db    *bolt.DB
	cfg   config
	diags *result.Result
}

// entry is the stored value.
type entry struct {
	CreatedAt time.Time              `json:"created_at"`
	Response  *ruleeval.EvalResponse `json:"response"`
}

// Open opens or creates the cache at path, creating parent directories as
// needed. A corrupt file is removed and recreated; Diagnostics then
// reports an Info diagnostic so the caller can tell the user.
func Open(path string, opts ...Option) (*Store, error) {
	cfg := config{now: time.Now, lockTimeout: defaultLockTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("evalcache: create cache dir: %w", err)
	}

	s := &Store{cfg: cfg, diags: &result.Result{}}

	db, err := open(path, cfg.lockTimeout)
	if err == nil {
		s.db = db
		return s, nil
	}
	if errors.Is(err, bolterrors.ErrTimeout) {
		return nil, fmt.Errorf("%w: %s", ErrLocked, path)
	}
	if !isCorrupt(err) {
		return nil, fmt.Errorf("evalcache: open %s: %w", path, err)
	}

	// Corrupt: discard and start over.
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		return nil, fmt.Errorf("evalcache: remove corrupt cache %s: %w", path, rmErr)
	}
	db, err2 := open(path, cfg.lockTimeout)
	if err2 != nil {
		return nil, fmt.Errorf("evalcache: recreate %s after corruption (%v): %w", path, err, err2)
	}
	s.db = db
	s.diags.Add(result.Diagnostic{
		Module:   module,
		Code:     CodeRecreated,
		Severity: result.Info,
		Message:  fmt.Sprintf("evaluation cache was corrupt and has been recreated: %v", err),
		Path:     path,
	})
	return s, nil
}

// open opens the database and ensures the bucket exists. Any failure after
// the file was opened is reported as-is so that Open can classify it.
func open(path string, lockTimeout time.Duration) (*bolt.DB, error) {
	db, err := bolt.Open(path, fileMode, &bolt.Options{Timeout: lockTimeout})
	if err != nil {
		return nil, err
	}
	err = db.Update(func(tx *bolt.Tx) error {
		_, err := tx.CreateBucketIfNotExists(bucketName)
		return err
	})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// isCorrupt reports whether err indicates an unreadable database file (as
// opposed to a permission or lock problem).
func isCorrupt(err error) bool {
	for _, target := range []error{
		bolterrors.ErrInvalid,
		bolterrors.ErrChecksum,
		bolterrors.ErrVersionMismatch,
		bolterrors.ErrInvalidMapping,
	} {
		if errors.Is(err, target) {
			return true
		}
	}
	// bbolt reports some structural problems with plain errors; permission
	// and lock errors never take these shapes.
	var pathErr *os.PathError
	if errors.As(err, &pathErr) || errors.Is(err, bolterrors.ErrTimeout) {
		return false
	}
	return true
}

// Diagnostics returns notices produced while opening the store, such as a
// recreated corrupt file. It is empty on a clean open.
func (s *Store) Diagnostics() *result.Result { return s.diags }

// Get returns the cached response for key. ok is false on a miss or when
// the entry has expired.
func (s *Store) Get(key string) (*ruleeval.EvalResponse, bool, error) {
	if s.db == nil {
		return nil, false, errors.New("evalcache: store is closed")
	}
	var e entry
	found := false
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		raw := b.Get([]byte(key))
		if raw == nil {
			return nil
		}
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("evalcache: decode entry %q: %w", key, err)
		}
		found = true
		return nil
	})
	if err != nil {
		return nil, false, err
	}
	if !found || e.Response == nil || s.expired(e.CreatedAt) {
		return nil, false, nil
	}
	return e.Response, true, nil
}

// Put stores resp under key with the current time as its creation time.
func (s *Store) Put(key string, resp *ruleeval.EvalResponse) error {
	if s.db == nil {
		return errors.New("evalcache: store is closed")
	}
	if resp == nil {
		return errors.New("evalcache: nil response")
	}
	raw, err := json.Marshal(entry{CreatedAt: s.cfg.now().UTC(), Response: resp})
	if err != nil {
		return fmt.Errorf("evalcache: encode entry: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists(bucketName)
		if err != nil {
			return err
		}
		return b.Put([]byte(key), raw)
	})
}

// Prune deletes expired entries and returns how many were removed.
// Undecodable entries are removed as well.
func (s *Store) Prune() (int, error) {
	if s.db == nil {
		return 0, errors.New("evalcache: store is closed")
	}
	if s.cfg.ttl <= 0 {
		return 0, nil
	}
	removed := 0
	err := s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket(bucketName)
		if b == nil {
			return nil
		}
		var stale [][]byte
		err := b.ForEach(func(k, v []byte) error {
			var e entry
			if err := json.Unmarshal(v, &e); err != nil || s.expired(e.CreatedAt) {
				stale = append(stale, append([]byte(nil), k...))
			}
			return nil
		})
		if err != nil {
			return err
		}
		for _, k := range stale {
			if err := b.Delete(k); err != nil {
				return err
			}
			removed++
		}
		return nil
	})
	return removed, err
}

// Close releases the file lock. It is safe to call more than once.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

func (s *Store) expired(createdAt time.Time) bool {
	if s.cfg.ttl <= 0 {
		return false
	}
	return !s.cfg.now().Before(createdAt.Add(s.cfg.ttl))
}
