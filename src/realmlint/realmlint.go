// Package realmlint validates Realm directory structure, including the
// realm.json manifest, schema directory, and EUID uniqueness across
// the Realm. Entity file validation is optionally delegated to a
// rulelint.Linter when one is provided via WithRuleLinter.
package realmlint

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

const module = "realm-lint"

var (
	// realmIDRe enforces RLM-008: reverse domain notation.
	realmIDRe = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9]*)*$`)
	semverRe  = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)
)

// Config holds options for the RealmLinter.
type Config struct {
	Strict        bool             // treat warnings as errors
	SchemaFetcher *schema.Fetcher  // fetcher for the Realm v1 schema (realm/v1.json)
	RuleLinter    *rulelint.Linter // optional: delegate entity file checks
}

// Option configures the RealmLinter.
type Option func(*Config)

// WithStrict causes warnings to be reported as errors.
func WithStrict(b bool) Option {
	return func(c *Config) { c.Strict = b }
}

// WithSchemaFetcher sets the schema fetcher for realm.json validation.
func WithSchemaFetcher(f *schema.Fetcher) Option {
	return func(c *Config) { c.SchemaFetcher = f }
}

// WithRuleLinter sets the rulelint.Linter used to validate entity files.
func WithRuleLinter(l *rulelint.Linter) Option {
	return func(c *Config) { c.RuleLinter = l }
}

// RealmLinter validates Realm directories.
type RealmLinter struct {
	cfg Config

	schemaMu sync.Mutex
	compiled *jsonschema.Schema
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
func (l *RealmLinter) Lint(dir string) *result.Result {
	res := &result.Result{}

	// RM001: Read and parse realm.json.
	realmPath := filepath.Join(dir, "realm.json")
	data, err := os.ReadFile(realmPath)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM001",
			Severity: result.Error,
			Message:  fmt.Sprintf("reading realm.json: %s", err),
			Path:     realmPath,
		})
		return res
	}

	rf, err := entity.ParseRealm(data)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM001",
			Severity: result.Error,
			Message:  fmt.Sprintf("parsing realm.json: %s", err),
			Path:     realmPath,
		})
		return res
	}

	// RM002: JSON Schema validation.
	l.checkRealmSchema(data, realmPath, res)

	// RM007: Realm field validation.
	checkRealmFields(rf, realmPath, res)

	// RM003 + RM004: Directory structure.
	checkSchemaDir(dir, res)

	// RM005 + RM006: Entity file scanning.
	l.scanEntityFiles(dir, res)

	if l.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// getCompiledSchema returns the cached compiled JSON Schema for realm.json.
// Only successful compilations are cached; transient failures are retried
// on subsequent calls. All access is synchronized via mutex.
func (l *RealmLinter) getCompiledSchema() (*jsonschema.Schema, error) {
	l.schemaMu.Lock()
	defer l.schemaMu.Unlock()

	if l.compiled != nil {
		return l.compiled, nil
	}

	compiled, err := l.compileSchema()
	if err != nil {
		return nil, err
	}
	l.compiled = compiled
	return l.compiled, nil
}

// compileSchema fetches and compiles the realm JSON Schema.
func (l *RealmLinter) compileSchema() (*jsonschema.Schema, error) {
	schemaData, err := l.cfg.SchemaFetcher.Fetch(context.Background(), schema.RealmV1Path)
	if err != nil {
		return nil, fmt.Errorf("fetching schema: %w", err)
	}

	sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(schemaData))
	if err != nil {
		return nil, fmt.Errorf("parsing schema: %w", err)
	}

	c := jsonschema.NewCompiler()
	if err := c.AddResource("realm.schema.json", sch); err != nil {
		return nil, fmt.Errorf("compiling schema: %w", err)
	}

	return c.Compile("realm.schema.json")
}

// RM002: JSON Schema validation of realm.json.
func (l *RealmLinter) checkRealmSchema(data []byte, realmPath string, res *result.Result) {
	if l.cfg.SchemaFetcher == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM002",
			Severity: result.Info,
			Message:  "schema validation skipped: no schema fetcher configured",
			Path:     realmPath,
		})
		return
	}

	compiled, err := l.getCompiledSchema()
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM002",
			Severity: result.Error,
			Message:  err.Error(),
			Path:     realmPath,
		})
		return
	}

	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return // RM001 already catches this
	}

	if err := compiled.Validate(doc); err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM002",
			Severity: result.Error,
			Message:  fmt.Sprintf("schema validation failed: %s", err),
			Path:     realmPath,
		})
	}
}

// RM007: Validate realm ID format (RLM-008) and semver version.
func checkRealmFields(rf *entity.RealmFile, realmPath string, res *result.Result) {
	if rf.Realm.ID == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM007",
			Severity: result.Error,
			Message:  "realm ID is required",
			Path:     realmPath,
		})
	} else if !realmIDRe.MatchString(rf.Realm.ID) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM007",
			Severity: result.Error,
			Message:  fmt.Sprintf("invalid realm ID %q: must follow reverse domain notation (e.g., com.example.rules)", rf.Realm.ID),
			Path:     realmPath,
		})
	}

	if rf.Realm.Version == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM007",
			Severity: result.Error,
			Message:  "realm version is required",
			Path:     realmPath,
		})
	} else if !semverRe.MatchString(rf.Realm.Version) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM007",
			Severity: result.Error,
			Message:  fmt.Sprintf("invalid realm version %q: must be valid semver", rf.Realm.Version),
			Path:     realmPath,
		})
	}
}

// RM003 + RM004: Validate _schema/ directory structure.
func checkSchemaDir(dir string, res *result.Result) {
	schemaDir := filepath.Join(dir, "_schema")
	info, err := os.Stat(schemaDir)
	if err != nil {
		msg := fmt.Sprintf("failed to access _schema/ directory: %v", err)
		if os.IsNotExist(err) {
			msg = "_schema/ directory missing"
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM003",
			Severity: result.Error,
			Message:  msg,
			Path:     schemaDir,
		})
		return
	}
	if !info.IsDir() {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM003",
			Severity: result.Error,
			Message:  "_schema/ exists but is not a directory",
			Path:     schemaDir,
		})
		return
	}

	entitySchemaPath := filepath.Join(schemaDir, "entity.v1.schema.json")
	if _, err := os.Stat(entitySchemaPath); err != nil {
		msg := "entity.v1.schema.json missing in _schema/"
		if !os.IsNotExist(err) {
			msg = fmt.Sprintf("unable to access entity.v1.schema.json in _schema/: %v", err)
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM004",
			Severity: result.Error,
			Message:  msg,
			Path:     entitySchemaPath,
		})
	}
}

// scanEntityFiles walks the Realm directory tree for .json entity files,
// checking EUID uniqueness (RM006) and optionally delegating to rulelint (RM005).
// The _schema/ subtree and realm.json files are skipped.
func (l *RealmLinter) scanEntityFiles(dir string, res *result.Result) {
	if l.cfg.RuleLinter == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM005",
			Severity: result.Info,
			Message:  "entity file validation skipped: no rule linter configured",
		})
	}

	// EUID → first file path where it was seen.
	euids := make(map[string]string)

	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM005",
				Severity: result.Error,
				Message:  fmt.Sprintf("accessing path: %s", err),
				Path:     path,
			})
			return nil
		}

		// Skip _schema/ subtree entirely.
		if d.IsDir() && d.Name() == "_schema" {
			return fs.SkipDir
		}

		// Skip directories, non-.json files, and realm.json.
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") || d.Name() == "realm.json" {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RM005",
				Severity: result.Error,
				Message:  fmt.Sprintf("reading file: %s", readErr),
				Path:     path,
			})
			return nil
		}

		// Try to parse as entity file to extract EUID.
		ef, parseErr := entity.Parse(data)
		if parseErr != nil {
			// Not a valid entity file — if rulelint is configured it will
			// report the error; otherwise skip silently.
			if l.cfg.RuleLinter != nil {
				fileRes := l.cfg.RuleLinter.LintBytes(data)
				copyDiagnostics(fileRes, path, res)
			}
			return nil
		}

		// RM006: EUID uniqueness.
		euid := ef.Entity.ID
		if euid != "" {
			if firstPath, exists := euids[euid]; exists {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RM006",
					Severity: result.Error,
					Message:  fmt.Sprintf("duplicate EUID %q (first seen in %s)", euid, filepath.Base(firstPath)),
					Path:     path,
				})
			} else {
				euids[euid] = path
			}
		}

		// Collect EUIDs from sub-entities too.
		collectSubEntityEUIDs(ef, path, euids, res)

		// RM005: Delegate entity file validation to rulelint.
		if l.cfg.RuleLinter != nil {
			fileRes := l.cfg.RuleLinter.LintBytes(data)
			copyDiagnostics(fileRes, path, res)
		}

		return nil
	})
	if walkErr != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RM005",
			Severity: result.Error,
			Message:  fmt.Sprintf("walking directory: %s", walkErr),
			Path:     dir,
		})
	}
}

// collectSubEntityEUIDs collects EUIDs from sub-entities and checks uniqueness.
func collectSubEntityEUIDs(ef *entity.File, fpath string, euids map[string]string, res *result.Result) {
	for i := range ef.SubEntities {
		sub := &ef.SubEntities[i]
		euid := sub.Entity.ID
		if euid != "" {
			if firstPath, exists := euids[euid]; exists {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RM006",
					Severity: result.Error,
					Message:  fmt.Sprintf("duplicate EUID %q (first seen in %s)", euid, filepath.Base(firstPath)),
					Path:     fpath,
				})
			} else {
				euids[euid] = fpath
			}
		}
		collectSubEntityEUIDs(sub, fpath, euids, res)
	}
}

// copyDiagnostics copies diagnostics from a sub-result into the main result,
// adding file path context.
func copyDiagnostics(from *result.Result, fpath string, to *result.Result) {
	for _, d := range from.Diagnostics {
		if d.Path == "" {
			d.Path = fpath
		} else {
			d.Path = fpath + ": " + d.Path
		}
		to.Add(d)
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
