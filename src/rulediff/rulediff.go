// Package rulediff provides stateful validation of entity file changes.
// It compares before/after versions of entity files to enforce lifecycle
// rules, versioning, and immutability constraints (Rule Manager Spec §7.2).
package rulediff

import (
	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "rule-diff"

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

// DiffBytes parses raw JSON for both versions and runs all 12 checks,
// including RD008 (hash verification). Use Diff if you already have parsed
// structs and do not need hash verification.
func (d *Differ) DiffBytes(baseData, headData []byte) *result.Result {
	res := &result.Result{}

	baseEnt, err := entity.Parse(baseData)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "failed to parse base entity file: " + err.Error(),
		})
		return res
	}
	headEnt, err := entity.Parse(headData)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "failed to parse head entity file: " + err.Error(),
		})
		return res
	}

	d.diffEntityTree(baseEnt, headEnt, res, headEnt.Entity.ID)
	checkHashBytes(headEnt, headData, res, headEnt.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// Diff compares parsed entity files and validates the lifecycle transition.
// Runs checks RD001–RD007, RD009–RD012. For RD008 (hash verification), use DiffBytes.
func (d *Differ) Diff(base, head *entity.File) *result.Result {
	res := &result.Result{}
	if base == nil || head == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "nil entity file passed to Diff",
		})
		return res
	}
	d.diffEntityTree(base, head, res, head.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// DiffNew validates a brand-new entity file (no previous version).
// Checks RD004 (revision=0, state=D) and RD011 (added_by present) for all rules and notes.
func (d *Differ) DiffNew(head *entity.File) *result.Result {
	res := &result.Result{}
	if head == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "nil entity file passed to DiffNew",
		})
		return res
	}
	checkNewEntityTree(head, res, head.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// diffEntityTree runs all diff checks for one entity and recurses into sub-entities.
func (d *Differ) diffEntityTree(base, head *entity.File, res *result.Result, path string) {
	checkRules(base, head, res, path)
	checkVersion(base, head, res, path)
	checkTimestamp(base, head, res, path)

	// Index base sub-entities for O(1) lookup.
	baseSubMap := make(map[string]*entity.File, len(base.SubEntities))
	for i := range base.SubEntities {
		baseSubMap[base.SubEntities[i].Entity.ID] = &base.SubEntities[i]
	}
	for i := range head.SubEntities {
		sub := &head.SubEntities[i]
		subPath := path + " > " + sub.Entity.ID
		if baseSub, ok := baseSubMap[sub.Entity.ID]; ok {
			d.diffEntityTree(baseSub, sub, res, subPath)
		} else {
			checkNewEntityTree(sub, res, subPath)
		}
	}
}

// promoteWarnings converts all Warning diagnostics to Error (strict mode).
func promoteWarnings(res *result.Result) {
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Severity == result.Warning {
			res.Diagnostics[i].Severity = result.Error
		}
	}
}
