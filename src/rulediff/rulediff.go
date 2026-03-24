// Package rulediff provides stateful validation of entity file changes.
// It compares before/after versions of entity files to enforce lifecycle
// rules, versioning, and immutability constraints (Rule Manager Spec §7.2).
package rulediff

import (
	"os"
	"path/filepath"
	"strings"

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

	checkVersion(baseEnt, headEnt, res, headEnt.Entity.ID)
	checkTimestamp(baseEnt, headEnt, res, headEnt.Entity.ID)
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
	checkVersion(base, head, res, head.Entity.ID)
	checkTimestamp(base, head, res, head.Entity.ID)
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

// diffEntityTree runs rule/note diff checks for one entity and recurses into sub-entities.
// RD007 (version) and RD009 (timestamp) are intentionally NOT run here — they apply only
// to the top-level entity file and are called directly by Diff/DiffBytes.
func (d *Differ) diffEntityTree(base, head *entity.File, res *result.Result, path string) {
	checkRules(base, head, res, path)

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
			delete(baseSubMap, sub.Entity.ID)
		} else {
			checkNewEntityTree(sub, res, subPath)
		}
	}
	// Any sub-entities remaining in baseSubMap were deleted in head — reject.
	for id := range baseSubMap {
		subPath := path + " > " + id
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD005",
			Severity: result.Error,
			Message:  "deletion of sub-entity is not allowed: " + subPath,
			Path:     subPath,
		})
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

// Compare validates the lifecycle transition between two raw entity JSON files.
// It is a package-level convenience wrapper over New(opts...).DiffBytes(base, head).
func Compare(base, head []byte, opts ...Option) *result.Result {
	return New(opts...).DiffBytes(base, head)
}

// CompareNew validates a brand-new entity JSON file (no previous version).
// It parses head and delegates to New(opts...).DiffNew.
func CompareNew(head []byte, opts ...Option) *result.Result {
	res := &result.Result{}
	headEnt, err := entity.Parse(head)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "failed to parse head entity file: " + err.Error(),
		})
		return res
	}
	return New(opts...).DiffNew(headEnt)
}

// CompareDir validates lifecycle transitions across all entity JSON files in
// two directories (baseDir = old version, headDir = new version).
// Files present in both are compared with Compare; files only in headDir are
// validated as new with CompareNew; files deleted from baseDir emit RD005.
// File I/O errors are reported as RD000 diagnostics and processing continues.
func CompareDir(baseDir, headDir string, opts ...Option) *result.Result {
	res := &result.Result{}
	baseFiles := scanJSONFiles(baseDir)
	headFiles := scanJSONFiles(headDir)

	for name, headPath := range headFiles {
		headData, err := os.ReadFile(headPath)
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD000",
				Severity: result.Error,
				Message:  "failed to read " + headPath + ": " + err.Error(),
				Path:     headPath,
			})
			continue
		}
		if basePath, ok := baseFiles[name]; ok {
			baseData, err := os.ReadFile(basePath)
			if err != nil {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RD000",
					Severity: result.Error,
					Message:  "failed to read " + basePath + ": " + err.Error(),
					Path:     basePath,
				})
				continue
			}
			for _, d := range Compare(baseData, headData, opts...).Diagnostics {
				res.Add(d)
			}
		} else {
			for _, d := range CompareNew(headData, opts...).Diagnostics {
				res.Add(d)
			}
		}
	}

	for name := range baseFiles {
		if _, ok := headFiles[name]; !ok {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD005",
				Severity: result.Error,
				Message:  "entity file deleted: " + name,
				Path:     filepath.Join(baseDir, name),
			})
		}
	}
	return res
}

// scanJSONFiles returns a map of filename → full path for all *.json files
// directly inside dir. Non-JSON files and I/O errors are silently skipped.
func scanJSONFiles(dir string) map[string]string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	files := make(map[string]string, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			files[e.Name()] = filepath.Join(dir, e.Name())
		}
	}
	return files
}
