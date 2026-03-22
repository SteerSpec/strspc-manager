// Package rulediff provides stateful validation of entity file changes.
// It compares before/after versions of entity files to enforce lifecycle
// rules, versioning, and immutability constraints (Rule Manager Spec §7.2).
package rulediff

import (
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Config holds options for the Differ.
type Config struct {
	Strict bool // treat warnings as errors
}

// Option configures the Differ.
type Option func(*Config)

// WithStrict causes warnings to be reported as errors.
func WithStrict(b bool) Option {
	return func(c *Config) { c.Strict = b }
}

// Differ validates lifecycle transitions between entity file versions.
type Differ struct {
	cfg Config
}

// New creates a Differ with the given options.
func New(opts ...Option) *Differ {
	cfg := Config{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Differ{cfg: cfg}
}

// Diff compares before and after entity files and validates transitions.
func (d *Differ) Diff(_, _ *entity.File) *result.Result {
	// TODO: implement 12 checks from §7.2
	return &result.Result{}
}

// DiffNew validates a newly added entity file (no previous version).
func (d *Differ) DiffNew(_ *entity.File) *result.Result {
	// TODO: validate new file constraints (revision 0, state D, etc.)
	return &result.Result{}
}
