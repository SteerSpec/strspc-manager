// Package rulelint provides stateless validation of SteerSpec entity files.
// It checks a single entity file against the JSON schema and 13 business
// rules defined in the Rule Manager Spec §7.1.
package rulelint

import (
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// Config holds options for the Linter.
type Config struct {
	SchemaVersion string // schema version to validate against (default: "v1")
	Strict        bool   // treat warnings as errors
}

// Option configures the Linter.
type Option func(*Config)

// WithStrict causes warnings to be reported as errors.
func WithStrict(b bool) Option {
	return func(c *Config) { c.Strict = b }
}

// WithSchemaVersion sets the schema version for validation.
func WithSchemaVersion(v string) Option {
	return func(c *Config) { c.SchemaVersion = v }
}

// Linter validates entity files against schema and business rules.
type Linter struct {
	cfg Config
}

// New creates a Linter with the given options.
func New(opts ...Option) *Linter {
	cfg := Config{SchemaVersion: "v1"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Linter{cfg: cfg}
}

// LintFile validates a parsed entity file.
func (l *Linter) LintFile(_ *entity.File) *result.Result {
	// TODO: implement 13 checks from §7.1
	return &result.Result{}
}

// LintBytes parses raw JSON and validates the entity file.
func (l *Linter) LintBytes(data []byte) *result.Result {
	f, err := entity.Parse(data)
	if err != nil {
		r := &result.Result{}
		r.Add(result.Diagnostic{
			Module:   "rule-lint",
			Code:     "RL001",
			Severity: result.Error,
			Message:  err.Error(),
		})
		return r
	}
	return l.LintFile(f)
}
