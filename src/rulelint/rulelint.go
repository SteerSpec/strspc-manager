// Package rulelint provides stateless validation of SteerSpec entity files.
// It checks a single entity file against the JSON schema and the business
// rules defined in the Rule Manager Spec §7.1 (RL001–RL015).
package rulelint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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

	// crossRefRe finds rule references inside note content, optionally
	// qualified with a realm ID (`dev.steerspec.core:ENT-001`, or a
	// single-component ID such as `core:ENT-001`, per RLM-008). Group 1 is
	// the EUID, group 2 the three-digit sequence number.
	crossRefRe = regexp.MustCompile(`\b(?:[a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*:)?([A-Za-z0-9]{3,18})-(\d{3})\b`)
)

// relationNoteTypes are the note types whose content is expected to
// reference other rules (checked by RL014).
var relationNoteTypes = map[string]bool{
	"supersedes": true,
	"extends":    true,
	"related":    true,
}

// Config holds options for the Linter.
type Config struct {
	// SchemaVersion is the schema version to validate against (default: "v1").
	SchemaVersion string
	// Strict treats warnings as errors.
	Strict bool
	// SchemaFetcher is the fetcher for JSON schemas (nil = skip schema check).
	SchemaFetcher *schema.Fetcher
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

	schemaMu sync.Mutex
	compiled *jsonschema.Schema
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
	if ef == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL000",
			Severity: result.Error,
			Message:  "entity file is nil",
		})
		return res
	}
	l.lintEntity(ef, res, "")
	if l.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// LintBytes parses raw JSON and validates the entity file. ctx bounds the
// JSON Schema fetch (RL002); a cancelled context reports RL002 as an error.
func (l *Linter) LintBytes(ctx context.Context, data []byte) *result.Result {
	f, res := l.parseAndValidate(data)
	if f == nil {
		return res
	}
	return l.lintParsed(ctx, f, data)
}

// parseAndValidate parses raw JSON (RL001). On failure it returns a nil
// file and a result holding the RL001 diagnostic.
func (l *Linter) parseAndValidate(data []byte) (*entity.File, *result.Result) {
	res := &result.Result{}
	f, err := entity.Parse(data)
	if err != nil {
		res.Add(parseDiagnostic(err))
		return nil, res
	}
	return f, res
}

// parseDiagnostic builds the RL001 diagnostic for a JSON parse failure.
func parseDiagnostic(err error) result.Diagnostic {
	return result.Diagnostic{
		Module:   module,
		Code:     "RL001",
		Severity: result.Error,
		Message:  err.Error(),
	}
}

// lintParsed runs every per-file check on an already-parsed entity file.
// data must be the raw bytes f was parsed from: RL002 (schema) and RL011
// (hash) need them. Callers that already hold a parsed file, such as the
// directory walker, use this to avoid parsing twice.
func (l *Linter) lintParsed(ctx context.Context, f *entity.File, data []byte) *result.Result {
	res := &result.Result{}

	// RL002: JSON Schema validation (requires raw bytes).
	l.checkSchema(ctx, data, res)

	// RL011: Blake3 hash verification (requires raw bytes).
	l.checkHash(f, data, res)

	// Remaining checks on parsed structure.
	l.lintEntity(f, res, "")

	if l.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// dirEntry holds a parsed entity file from a directory scan.
type dirEntry struct {
	path string
	file *entity.File
}

// LintDir validates all entity JSON files in a directory and runs cross-file
// reference checks (RL012). Only immediate children are scanned (shallow).
// ctx bounds the JSON Schema fetch (RL002).
func (l *Linter) LintDir(ctx context.Context, dir string) *result.Result {
	return l.lintDir(ctx, dir, entity.WithRecursive(false))
}

// LintRealm validates all entity JSON files in a realm directory tree recursively
// and runs cross-entity reference checks (RL012, RL014). ctx bounds the JSON
// Schema fetch (RL002).
// When the realm declares sub_realms, each sub-realm directory is linted
// separately, and cross-references are validated globally across the
// parent realm and all sub-realms.
func (l *Linter) LintRealm(ctx context.Context, dir string) *result.Result {
	// Try to load realm.json to check for sub_realms.
	rf, err := entity.LoadRealm(filepath.Join(dir, "realm.json"))
	if err != nil || len(rf.SubRealms) == 0 {
		// No realm.json or no sub-realms: fall back to recursive walk.
		return l.lintDir(ctx, dir)
	}

	// Sub-realm-aware linting: lint parent excluding sub-realm dirs,
	// then lint each sub-realm dir, and validate cross-refs globally.
	return l.lintRealmWithSubs(ctx, dir, rf.SubRealms)
}

// lintRealmWithSubs lints the parent realm and each sub-realm separately,
// then validates RL012/RL014 cross-references across the combined rule ID set.
func (l *Linter) lintRealmWithSubs(ctx context.Context, dir string, subRealms []string) *result.Result {
	res := &result.Result{}

	// Shared state for global RL012 cross-ref validation.
	allRuleIDs := make(map[string]string)
	var allParsed []dirEntry

	// Lint parent dir, excluding sub-realm directories.
	l.lintDirCollect(ctx, dir, res, allRuleIDs, &allParsed, entity.WithExcludeDirs(subRealms))

	// Lint each sub-realm directory.
	for _, sub := range subRealms {
		subDir := filepath.Join(dir, sub)
		l.lintDirCollect(ctx, subDir, res, allRuleIDs, &allParsed)
	}

	// Cross-file supersedes/relation references (RL012, RL014) across entire realm tree.
	for _, de := range allParsed {
		checkCrossRefs(de.file, de.path, allRuleIDs, res, l.cfg.Strict)
	}

	return res
}

// lintDirCollect lints entity files in dir and appends results to shared
// collections (allRuleIDs, allParsed) for later cross-ref validation.
func (l *Linter) lintDirCollect(ctx context.Context, dir string, res *result.Result, allRuleIDs map[string]string, allParsed *[]dirEntry, walkOpts ...entity.WalkOption) {
	walkErr := entity.WalkEntityFiles(dir, func(fpath string, data []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			if data == nil {
				res.Add(result.Diagnostic{
					Module: module, Code: "RL000", Severity: result.Error,
					Message: fmt.Sprintf("reading file: %s", parseErr), Path: fpath,
				})
			} else {
				// RL001: the walker already tried entity.Parse; report its error.
				d := parseDiagnostic(parseErr)
				d.Path = fpath
				res.Add(d)
			}
			return nil
		}

		// Reuse the walker's parsed file instead of parsing a second time.
		for _, d := range l.lintParsed(ctx, ef, data).Diagnostics {
			if d.Path == "" {
				d.Path = fpath
			} else {
				d.Path = fpath + ": " + d.Path
			}
			res.Add(d)
		}

		collectRuleIDs(ef, fpath, allRuleIDs)
		*allParsed = append(*allParsed, dirEntry{path: fpath, file: ef})
		return nil
	}, walkOpts...)
	if walkErr != nil {
		res.Add(result.Diagnostic{
			Module: module, Code: "RL000", Severity: result.Error,
			Message: fmt.Sprintf("reading directory: %s", walkErr), Path: dir,
		})
	}
}

// lintDir is the internal implementation used by LintDir and LintRealm; recursion
// behavior is controlled via the provided entity.WalkOption values.
func (l *Linter) lintDir(ctx context.Context, dir string, walkOpts ...entity.WalkOption) *result.Result {
	res := &result.Result{}
	allRuleIDs := make(map[string]string)
	var parsed []dirEntry

	l.lintDirCollect(ctx, dir, res, allRuleIDs, &parsed, walkOpts...)

	// Second pass: cross-file supersedes/relation references (RL012, RL014).
	for _, de := range parsed {
		checkCrossRefs(de.file, de.path, allRuleIDs, res, l.cfg.Strict)
	}

	return res
}

// lintEntity runs checks RL003–RL010, RL013, RL015 on a single entity and recurses
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
	checkNoteSequence(ef, res, path)
	checkNoteTypes(ef, res, path)
	checkSemver(ef, res, path)

	for i := range ef.SubEntities {
		sub := &ef.SubEntities[i]
		subPath := path + " > " + sub.Entity.ID
		checkSubEntityParent(sub, ef.Entity.ID, res, subPath)
		l.lintEntity(sub, res, path)
	}
}

// getCompiledSchema returns the cached compiled JSON Schema. On first
// successful call the result is cached. Transient failures are retried
// on subsequent calls so a temporary network issue doesn't permanently
// disable RL002. All access is synchronized via mutex.
func (l *Linter) getCompiledSchema(ctx context.Context) (*jsonschema.Schema, error) {
	l.schemaMu.Lock()
	defer l.schemaMu.Unlock()

	if l.compiled != nil {
		return l.compiled, nil
	}

	compiled, err := l.compileSchema(ctx)
	if err != nil {
		return nil, err
	}
	l.compiled = compiled
	return l.compiled, nil
}

// compileSchema fetches and compiles the entity JSON Schema.
func (l *Linter) compileSchema(ctx context.Context) (*jsonschema.Schema, error) {
	schemaPath := schema.EntityV1Path
	if l.cfg.SchemaVersion != "" && l.cfg.SchemaVersion != "v1" {
		schemaPath = "entity/" + l.cfg.SchemaVersion + ".json"
	}

	schemaData, err := l.cfg.SchemaFetcher.Fetch(ctx, schemaPath)
	if err != nil {
		return nil, fmt.Errorf("fetching schema: %w", err)
	}

	sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaData))
	if err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("entity.schema.json", sch); err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}

	return c.Compile("entity.schema.json")
}

// RL002: JSON Schema validation.
func (l *Linter) checkSchema(ctx context.Context, data []byte, res *result.Result) {
	if l.cfg.SchemaFetcher == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL002",
			Severity: result.Info,
			Message:  "schema validation skipped: no schema fetcher configured",
		})
		return
	}

	compiled, err := l.getCompiledSchema(ctx)
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
	maxNum := 0

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
		if n > maxNum {
			maxNum = n
		}
	}

	if maxNum == 0 {
		return
	}

	// Check for sequential numbering from 1 to max.
	for i := 1; i <= maxNum; i++ {
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
		if !entity.IsValidState(r.State) {
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

// RL015: Note numbers under each rule are sequential from 01, with no gaps
// and no duplicates (NTE-011). IDs that fail RL009 are skipped.
func checkNoteSequence(ef *entity.File, res *result.Result, path string) {
	seen := make(map[string]map[int]bool)
	maxNum := make(map[string]int)
	var ruleOrder []string

	for _, n := range ef.Notes {
		m := noteIDRe.FindStringSubmatch(n.ID)
		if m == nil {
			continue // RL009 already reported
		}
		num, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if num == 0 {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL015",
				Severity: result.Error,
				Message:  fmt.Sprintf("invalid note number in %s (expected sequential from 01)", n.ID),
				Path:     path,
			})
			continue
		}
		ruleID := m[1]
		if seen[ruleID] == nil {
			seen[ruleID] = make(map[int]bool)
			ruleOrder = append(ruleOrder, ruleID)
		}
		if seen[ruleID][num] {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL015",
				Severity: result.Error,
				Message:  fmt.Sprintf("duplicate note ID %s", n.ID),
				Path:     path,
			})
		}
		seen[ruleID][num] = true
		if num > maxNum[ruleID] {
			maxNum[ruleID] = num
		}
	}

	for _, ruleID := range ruleOrder {
		for i := 1; i <= maxNum[ruleID]; i++ {
			if !seen[ruleID][i] {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RL015",
					Severity: result.Error,
					Message:  fmt.Sprintf("missing note number %s/%02d (expected sequential from 01)", ruleID, i),
					Path:     path,
				})
			}
		}
	}
}

// RL010: Note types in closed enum.
func checkNoteTypes(ef *entity.File, res *result.Result, path string) {
	for _, n := range ef.Notes {
		if !entity.IsValidNoteType(n.Type) {
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

	hashPath := ef.Entity.ID + "/rule_set.hash"

	expected := *ef.RuleSet.Hash
	if !hashRe.MatchString(expected) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("hash format invalid: %q (expected blake3:<64 hex>)", expected),
			Path:     hashPath,
		})
		return
	}

	computed, err := entity.ComputeHash(data)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("computing hash: %s", err),
			Path:     hashPath,
		})
		return
	}

	if computed != expected {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL011",
			Severity: result.Error,
			Message:  fmt.Sprintf("hash mismatch: computed %s, expected %s", computed, expected),
			Path:     hashPath,
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

// RL012: Cross-file supersedes references.
// RL014: Rule references in relation notes (supersedes, extends, related).
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
				Message:  fmt.Sprintf("rule %s supersedes %q which was not found in scanned files", r.ID, ref),
				Path:     fpath,
			})
		}
	}

	checkNoteCrossRefs(ef, fpath, allRuleIDs, res, sev)

	for i := range ef.SubEntities {
		checkCrossRefs(&ef.SubEntities[i], fpath, allRuleIDs, res, strict)
	}
}

// checkNoteCrossRefs implements RL014: every unqualified rule reference
// (<EUID>-<NNN>) found in the content of a supersedes/extends/related note
// must resolve to a rule in the scanned set. Qualified references
// (<realm-id>:<EUID>-<NNN>) point outside this realm and are not checked.
func checkNoteCrossRefs(ef *entity.File, fpath string, allRuleIDs map[string]string, res *result.Result, sev result.Severity) {
	for _, n := range ef.Notes {
		if !relationNoteTypes[n.Type] {
			continue
		}
		reported := make(map[string]bool)
		for _, m := range crossRefRe.FindAllStringSubmatch(n.Content, -1) {
			if strings.Contains(m[0], ":") {
				continue // cross-realm reference; resolved by realm-resolve
			}
			ref := m[1] + "-" + m[2]
			if reported[ref] {
				continue
			}
			if _, ok := allRuleIDs[ref]; !ok {
				reported[ref] = true
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RL014",
					Severity: sev,
					Message:  fmt.Sprintf("note %s (%s) references %q which was not found in scanned files", n.ID, n.Type, ref),
					Path:     fpath,
				})
			}
		}
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

// promoteWarnings promotes all Warning-severity diagnostics to Error.
func promoteWarnings(res *result.Result) {
	for i := range res.Diagnostics {
		if res.Diagnostics[i].Severity == result.Warning {
			res.Diagnostics[i].Severity = result.Error
		}
	}
}
