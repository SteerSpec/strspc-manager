// Package realmlint validates Realm directory structure, including the
// realm.json manifest, schema directory, entity file validation, and
// EUID uniqueness across the Realm.
package realmlint

import (
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Config holds options for the RealmLinter.
type Config struct {
	Strict bool // treat warnings as errors
}

// Option configures the RealmLinter.
type Option func(*Config)

// WithStrict causes warnings to be reported as errors.
func WithStrict(b bool) Option {
	return func(c *Config) { c.Strict = b }
}

// RealmLinter validates Realm directories.
type RealmLinter struct {
	cfg Config
}

// New creates a RealmLinter with the given options.
func New(opts ...Option) *RealmLinter {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &RealmLinter{cfg: cfg}
}

// Lint validates a Realm directory at the given path.
func (l *RealmLinter) Lint(_ string) *result.Result {
	// TODO: implement realm validation
	// - Parse and validate realm.json
	// - Check _schema/ directory
	// - Validate all entity files
	// - Check EUID uniqueness
	return &result.Result{}
}
