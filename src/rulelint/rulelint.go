// Package rulelint provides stateless validation of SteerSpec entity files.
// It checks a single entity file against the JSON schema and 13 business
// rules defined in the Rule Manager Spec §7.1.
package rulelint

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

const module = "rule-lint"

var (
	euidRe   = regexp.MustCompile(`^[a-zA-Z0-9]{3,18}$`)
	ruleIDRe = regexp.MustCompile(`^([A-Za-z0-9]+)-(\d{3})$`)
	noteIDRe = regexp.MustCompile(`^([A-Za-z0-9]+-\d{3})/(\d{2})$`)
	semverRe = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
	hashRe   = regexp.MustCompile(`^blake3:[a-f0-9]{64}$`)
)

var validStates = map[string]bool{
	"D": true, "A": true, "P": true,
	"I": true, "R": true, "T": true,
}

var validNoteTypes = map[string]bool{
	"rationale":          true,
	"example":            true,
	"counter_example":    true,
	"reference":          true,
	"applies_to":         true,
	"changelog":          true,
	"clarification":      true,
	"deprecation_notice": true,
	"supersedes":         true,
	"extends":            true,
	"related":            true,
}

// Config holds options for the Linter.
type Config struct {
	SchemaVersion string          // schema version to validate against (default: "v1")
	Strict        bool            // treat warnings as errors
	SchemaFetcher *schema.Fetcher // fetcher for JSON schemas (nil = skip schema check)
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

// WithSchemaFetcher sets the schema fetcher for JSON Schema validation.
func WithSchemaFetcher(f *schema.Fetcher) Option {
	return func(c *Config) { c.SchemaFetcher = f }
}

// Linter validates entity files against schema and business rules.
type Linter struct {
	cfg Config

	schemaOnce sync.Once
	compiled   *jsonschema.Schema
	schemaErr  error
}

// New creates a Linter with the given options.
func New(opts ...Option) *Linter {
	cfg := Config{SchemaVersion: "v1"}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Linter{cfg: cfg}
}

// LintFile validates a parsed entity file. For JSON Schema validation (RL002)
// and hash verification (RL011), use LintBytes which has access to raw JSON.
func (l *Linter) LintFile(ef *entity.File) *result.Result {
	res := &result.Result{}
	l.lintEntity(ef, res, "")
	if l.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// LintBytes parses raw JSON and validates the entity file.
func (l *Linter) LintBytes(data []byte) *result.Result {
	res := &result.Result{}

	// RL001: Valid JSON.
	f, err := entity.Parse(data)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL001",
			Severity: result.Error,
			Message:  err.Error(),
		})
		return res
	}

	// RL002: JSON Schema validation (requires raw bytes).
	l.checkSchema(data, res)

	// RL011: Blake3 hash verification (requires raw bytes).
	l.checkHash(f, data, res)

	// Remaining checks on parsed structure.
	l.lintEntity(f, res, "")

	if l.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// dirEntry holds a parsed entity file and its raw bytes from a directory scan.
type dirEntry struct {
	path string
	data []byte
	file *entity.File
}

// LintDir validates all entity JSON files in a directory and runs cross-file
// reference checks (RL012).
func (l *Linter) LintDir(dir string) *result.Result {
	res := &result.Result{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL000",
			Severity: result.Error,
			Message:  fmt.Sprintf("reading directory: %s", err),
			Path:     dir,
		})
		return res
	}

	// First pass: lint each file and collect parsed entities.
	var parsed []dirEntry
	allRuleIDs := make(map[string]string) // ruleID → file path

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		fpath := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(fpath)
		if readErr != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL000",
				Severity: result.Error,
				Message:  fmt.Sprintf("reading file: %s", readErr),
				Path:     fpath,
			})
			continue
		}

		// Skip non-entity files (e.g. realm.json).
		if !isEntityJSON(data) {
			continue
		}

		fileRes := l.LintBytes(data)
		// Copy diagnostics, adding file path context.
		for _, d := range fileRes.Diagnostics {
			if d.Path == "" {
				d.Path = fpath
			} else {
				d.Path = fpath + ": " + d.Path
			}
			res.Add(d)
		}

		ef, parseErr := entity.Parse(data)
		if parseErr == nil {
			collectRuleIDs(ef, fpath, allRuleIDs)
			parsed = append(parsed, dirEntry{path: fpath, data: data, file: ef})
		}
	}

	// Second pass: cross-file supersedes/relation references (RL012).
	for _, de := range parsed {
		checkCrossRefs(de.file, de.path, allRuleIDs, res, l.cfg.Strict)
	}

	return res
}

// lintEntity runs checks RL003–RL010, RL013 on a single entity and recurses
// into sub-entities. The path prefix is used for sub-entity context.
func (l *Linter) lintEntity(ef *entity.File, res *result.Result, pathPrefix string) {
	path := pathPrefix
	if path != "" {
		path += " > "
	}
	path += ef.Entity.ID

	checkEUID(ef, res, path)
	checkRuleIDs(ef, res, path)
	checkSequential(ef, res, path)
	checkStates(ef, res, path)
	checkNoteRuleRefs(ef, res, path)
	checkNoteIDs(ef, res, path)
	checkNoteTypes(ef, res, path)
	checkSemver(ef, res, path)

	for i := range ef.SubEntities {
		sub := &ef.SubEntities[i]
		subPath := path + " > " + sub.Entity.ID
		checkSubEntityParent(sub, ef.Entity.ID, res, subPath)
		l.lintEntity(sub, res, path)
	}
}

// getCompiledSchema returns the cached compiled JSON Schema, fetching and
// compiling it on first call.
func (l *Linter) getCompiledSchema() (*jsonschema.Schema, error) {
	l.schemaOnce.Do(func() {
		schemaPath := schema.EntityV1Path
		if l.cfg.SchemaVersion != "" && l.cfg.SchemaVersion != "v1" {
			schemaPath = "entity/" + l.cfg.SchemaVersion + ".json"
		}

		schemaData, err := l.cfg.SchemaFetcher.Fetch(context.Background(), schemaPath)
		if err != nil {
			l.schemaErr = fmt.Errorf("fetching schema: %w", err)
			return
		}

		sch, err := jsonschema.UnmarshalJSON(strings.NewReader(string(schemaData)))
		if err != nil {
			l.schemaErr = fmt.Errorf("parsing schema: %w", err)
			return
		}

		c := jsonschema.NewCompiler()
		if err := c.AddResource("entity.schema.json", sch); err != nil {
			l.schemaErr = fmt.Errorf("compiling schema: %w", err)
			return
		}

		l.compiled, l.schemaErr = c.Compile("entity.schema.json")
	})
	return l.compiled, l.schemaErr
}

// RL002: JSON Schema validation.
func (l *Linter) checkSchema(data []byte, res *result.Result) {
	if l.cfg.SchemaFetcher == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL002",
			Severity: result.Info,
			Message:  "schema validation skipped: no schema fetcher configured",
		})
		return
	}

	compiled, err := l.getCompiledSchema()
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL002",
			Severity: result.Error,
			Message:  err.Error(),
		})
		return
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return // RL001 already catches this
	}

	if err := compiled.Validate(doc); err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL002",
			Severity: result.Error,
			Message:  fmt.Sprintf("schema validation failed: %s", err),
		})
	}
}

// RL003: EUID format (3–18 alphanumeric characters).
func checkEUID(ef *entity.File, res *result.Result, path string) {
	if !euidRe.MatchString(ef.Entity.ID) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL003",
			Severity: result.Error,
			Message:  fmt.Sprintf("invalid EUID %q: must be 3-18 alphanumeric characters", ef.Entity.ID),
			Path:     path,
		})
	}
}

// RL004: Rule IDs must be prefixed with entity ID.
func checkRuleIDs(ef *entity.File, res *result.Result, path string) {
	for _, r := range ef.Rules {
		m := ruleIDRe.FindStringSubmatch(r.ID)
		if m == nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL004",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule ID %q does not match format <EUID>-<NNN>", r.ID),
				Path:     path,
			})
			continue
		}
		if m[1] != ef.Entity.ID {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL004",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule ID %q prefix %q does not match entity ID %q", r.ID, m[1], ef.Entity.ID),
				Path:     path,
			})
		}
	}
}

// RL005: Sequential rule numbers, no gaps, no duplicates.
func checkSequential(ef *entity.File, res *result.Result, path string) {
	seen := make(map[int]bool)
	var nums []int

	for _, r := range ef.Rules {
		m := ruleIDRe.FindStringSubmatch(r.ID)
		if m == nil {
			continue // RL004 already reported
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if seen[n] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL005",
				Severity: result.Error,
				Message:  fmt.Sprintf("duplicate rule number %d in %q", n, r.ID),
				Path:     path,
			})
		}
		seen[n] = true
		nums = append(nums, n)
	}

	if len(nums) == 0 {
		return
	}

	// Check for sequential numbering starting at 1.
	for i := 1; i <= len(nums); i++ {
		if !seen[i] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL005",
				Severity: result.Error,
				Message:  fmt.Sprintf("missing rule number %03d (expected sequential from 001)", i),
				Path:     path,
			})
		}
	}
}

// RL006: State values in allowed enum.
func checkStates(ef *entity.File, res *result.Result, path string) {
	for _, r := range ef.Rules {
		if !validStates[r.State] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL006",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s has invalid state %q (allowed: D, A, P, I, R, T)", r.ID, r.State),
				Path:     path,
			})
		}
	}
}

// RL007: Sub-entity parent matches containing entity ID.
func checkSubEntityParent(sub *entity.File, parentID string, res *result.Result, subPath string) {
	if sub.Entity.Parent != parentID {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL007",
			Severity: result.Error,
			Message:  fmt.Sprintf("sub-entity %q parent %q does not match containing entity %q", sub.Entity.ID, sub.Entity.Parent, parentID),
			Path:     subPath,
		})
	}
}

// RL008: Note rule_ref points to existing rule in same entity.
func checkNoteRuleRefs(ef *entity.File, res *result.Result, path string) {
	ruleIDs := make(map[string]bool, len(ef.Rules))
	for _, r := range ef.Rules {
		ruleIDs[r.ID] = true
	}
	for _, n := range ef.Notes {
		if !ruleIDs[n.RuleRef] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL008",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %q references non-existent rule %q", n.ID, n.RuleRef),
				Path:     path,
			})
		}
	}
}

// RL009: Note ID format (<rule_id>/<incremental>).
func checkNoteIDs(ef *entity.File, res *result.Result, path string) {
	for _, n := range ef.Notes {
		m := noteIDRe.FindStringSubmatch(n.ID)
		if m == nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL009",
				Severity: result.Error,
				Message:  fmt.Sprintf("note ID %q does not match format <rule_id>/<NN>", n.ID),
				Path:     path,
			})
			continue
		}
		if m[1] != n.RuleRef {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL009",
				Severity: result.Error,
				Message:  fmt.Sprintf("note ID %q prefix %q does not match rule_ref %q", n.ID, m[1], n.RuleRef),
				Path:     path,
			})
		}
	}
}

// RL010: Note types in closed enum.
func checkNoteTypes(ef *entity.File, res *result.Result, path string) {
	for _, n := range ef.Notes {
		if !validNoteTypes[n.Type] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL010",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %q has invalid type %q", n.ID, n.Type),
				Path:     path,
			})
		}
	}
}

// RL011: Blake3 hash verification.
func (l *Linter) checkHash(ef *entity.File, data []byte, res *result.Result) {
	if ef.RuleSet.Hash == nil {
		return // hash is optional
	}

	expected := *ef.RuleSet.Hash
	if !hashRe.MatchString(expected) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("hash format invalid: %q (expected blake3:<64 hex>)", expected),
		})
		return
	}

	computed, err := computeHash(data)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("computing hash: %s", err),
		})
		return
	}

	if computed != expected {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("hash mismatch: computed %s, expected %s", computed, expected),
		})
	}
}

// RL013: Valid semver version.
func checkSemver(ef *entity.File, res *result.Result, path string) {
	if !semverRe.MatchString(ef.RuleSet.Version) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL013",
			Severity: result.Error,
			Message:  fmt.Sprintf("invalid semver version %q", ef.RuleSet.Version),
			Path:     path,
		})
	}
}

// RL012: Cross-file supersedes/relation references.
func checkCrossRefs(ef *entity.File, fpath string, allRuleIDs map[string]string, res *result.Result, strict bool) {
	sev := result.Warning
	if strict {
		sev = result.Error
	}

	for _, r := range ef.Rules {
		if r.Supersedes == nil {
			continue
		}
		ref := *r.Supersedes
		if _, ok := allRuleIDs[ref]; !ok {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL012",
				Severity: sev,
				Message:  fmt.Sprintf("rule %s supersedes %q which was not found in directory", r.ID, ref),
				Path:     fpath,
			})
		}
	}

	for i := range ef.SubEntities {
		checkCrossRefs(&ef.SubEntities[i], fpath, allRuleIDs, res, strict)
	}
}

// collectRuleIDs gathers all rule IDs from an entity file into the map.
func collectRuleIDs(ef *entity.File, fpath string, out map[string]string) {
	for _, r := range ef.Rules {
		out[r.ID] = fpath
	}
	for i := range ef.SubEntities {
		collectRuleIDs(&ef.SubEntities[i], fpath, out)
	}
}

// isEntityJSON does a quick check if the JSON has an "entity" top-level key.
func isEntityJSON(data []byte) bool {
	var probe struct {
		Entity *json.RawMessage `json:"entity"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Entity != nil
}

// promoteWarnings promotes all Warning-severity diagnostics to Error.
func promoteWarnings(res *result.Result) {
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Severity == result.Warning {
			res.Diagnostics[i].Severity = result.Error
		}
	}
}
