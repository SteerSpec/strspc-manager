// Package rulediff provides stateful validation of entity file changes.
// It compares before/after versions of entity files to enforce lifecycle
// rules, versioning, and immutability constraints (Rule Manager Spec §7.2).
package rulediff

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"

	"github.com/SteerSpec/strspc-manager/src/internal/realmfs"
)

const module = "rule-diff"

// Config holds options for the Differ.
type Config struct {
	// Strict treats warnings as errors.
	Strict bool
	// Contributors lists the Realm's contributors; RD023 runs when it is
	// not empty.
	Contributors []entity.Contributor
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

// DiffBytes parses raw JSON for both versions and runs every check Diff runs,
// plus RD008 (hash verification). Use Diff if you already have parsed
// structs and do not need hash verification.
func (d *Differ) DiffBytes(baseData, headData []byte) *result.Result {
	res := &result.Result{}

	baseEnt, err := decodeEntity(baseData)
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
	d.diffEntityTree(baseEnt, headEnt, entity.VisibilityPublic, res, headEnt.Entity.ID)
	checkContributors(baseEnt, headEnt, d.cfg.Contributors, res, headEnt.Entity.ID)
	checkHashBytes(headEnt, headData, res, headEnt.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// Diff compares parsed entity files and validates the lifecycle transition.
// It runs the checks of one entity file, all but RD008 (hash verification),
// which DiffBytes adds, and the tree checks CompareRealms runs.
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
	d.diffEntityTree(base, head, entity.VisibilityPublic, res, head.Entity.ID)
	checkContributors(base, head, d.cfg.Contributors, res, head.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// decodeEntity decodes without entity.Parse's content check. A base needs that,
// so that the change repairing it is compared, and so does CompareRealms, as
// only Compare reports that check.
func decodeEntity(data []byte) (*entity.File, error) {
	var f entity.File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing entity JSON: %w", err)
	}
	return &f, nil
}

// DiffNewBytes parses a brand-new entity file and validates it like DiffNew,
// plus RD008 (hash verification), as DiffBytes does for a changed file.
func (d *Differ) DiffNewBytes(headData []byte) *result.Result {
	headEnt, err := entity.Parse(headData)
	if err != nil {
		res := &result.Result{}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "failed to parse head entity file: " + err.Error(),
		})
		return res
	}
	res := d.DiffNew(headEnt)
	// DiffNew has promoted warnings already; RD008 is an Error either way.
	checkHashBytes(headEnt, headData, res, headEnt.Entity.ID)
	return res
}

// DiffNew validates a brand-new entity file (no previous version).
// Checks RD004 (revision=0, state=D) and RD011 (added_by present) for all rules and notes.
// It has no bytes to hash, so RD008 is left to DiffNewBytes.
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
	checkContributors(nil, head, d.cfg.Contributors, res, head.Entity.ID)
	if d.cfg.Strict {
		promoteWarnings(res)
	}
	return res
}

// diffEntityTree runs rule/note diff checks for one entity and recurses into sub-entities.
// RD007 (version) and RD009 (timestamp) are intentionally NOT run here — they apply only
// to the top-level entity file and are called directly by Diff/DiffBytes.
// baseVisibility is the effective visibility in base of the entity containing
// this one, "" when it cannot be known here (see visibilityOf).
func (d *Differ) diffEntityTree(base, head *entity.File, baseVisibility string, res *result.Result, path string) {
	checkRules(base, head, res, path)
	checkInherits(base, head, res, path)
	checkVisibility(base, head, baseVisibility, res, path)
	baseVisibility = visibilityOf(&base.Entity, baseVisibility)

	// Index base sub-entities for O(1) lookup.
	baseSubMap := make(map[string]*entity.File, len(base.SubEntities))
	for i := range base.SubEntities {
		baseSubMap[base.SubEntities[i].Entity.ID] = &base.SubEntities[i]
	}
	for i := range head.SubEntities {
		sub := &head.SubEntities[i]
		subPath := path + " > " + sub.Entity.ID
		if baseSub, ok := baseSubMap[sub.Entity.ID]; ok {
			d.diffEntityTree(baseSub, sub, baseVisibility, res, subPath)
			delete(baseSubMap, sub.Entity.ID)
		} else {
			checkNewEntityTree(sub, res, subPath)
		}
	}
	// Any sub-entities remaining in baseSubMap were deleted in head — reject.
	for _, id := range slices.Sorted(maps.Keys(baseSubMap)) {
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
// It is a package-level convenience wrapper over New(opts...).DiffNewBytes(head).
func CompareNew(head []byte, opts ...Option) *result.Result {
	return New(opts...).DiffNewBytes(head)
}

// CompareDir validates lifecycle transitions across all entity JSON files in
// two directories (baseDir = old version, headDir = new version).
// Files present in both are compared with Compare, unless their bytes are the
// same: SPEC §7.2 validates each changed entity file. Files only in headDir are
// validated as new with CompareNew; files deleted from baseDir emit RD005.
// realm.json is a manifest, not an entity file: it and the kind of each file
// below the directories are compared as CompareRealms does, except that a
// baseDir that doesn't exist is an empty one, so a mistyped one passes for a
// new tree: CompareRealms refuses it. A headDir that doesn't exist is RD000, as
// is an empty path or a dangling link for either.
// Entity files are compared only directly inside the directories.
// File I/O errors are reported as RD000 diagnostics and processing continues.
func CompareDir(baseDir, headDir string, opts ...Option) *result.Result {
	res := &result.Result{}
	for _, dir := range []string{baseDir, headDir} {
		if dir == "" {
			continue
		}
		if err := realmfs.CheckRoot(dir); err != nil {
			res.Add(result.Diagnostic{Module: module, Code: "RD000", Severity: result.Error, Message: "failed to read " + dir + ": " + err.Error(), Path: dir})
			return res
		}
	}
	// A link is never read. One in head is refused; one in base, deleted or
	// replaced, fails closed in the CompareRealms run below.
	baseFiles, _ := scanJSONFiles(baseDir)
	headFiles, headLinks := scanJSONFiles(headDir)
	// A base index.json that is no published index held rules, so it is
	// compared like any entity file: deleting it is RD005.
	if path := filepath.Join(baseDir, "index.json"); baseDir != "" {
		if data, err := realmfs.ReadFile(path); err == nil && !entity.IsPublishedIndex(data) {
			if baseFiles == nil { // A directory that can't be listed but can be read.
				baseFiles = map[string]string{}
			}
			baseFiles["index.json"] = path
		}
	}
	for _, name := range headLinks {
		path := filepath.Join(headDir, name)
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD000",
			Severity: result.Error,
			Message:  "failed to read " + path + ": " + realmfs.ErrNotRegular.Error(),
			Path:     path,
		})
	}

	for _, name := range slices.Sorted(maps.Keys(headFiles)) {
		headPath := headFiles[name]
		headData, err := realmfs.ReadFile(headPath)
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
			baseData, err := realmfs.ReadFile(basePath)
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
			if bytes.Equal(baseData, headData) {
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

	realms := CompareRealms
	if isAbsent(baseDir, os.Lstat) {
		realms = func(_, head string, opts ...Option) *result.Result {
			return compareRealms("", head, opts...) // Nothing to compare with.
		}
	}
	for _, d := range realms(baseDir, headDir, opts...).Diagnostics {
		res.Add(d)
	}

	for _, name := range slices.Sorted(maps.Keys(baseFiles)) {
		basePath := baseFiles[name]
		if _, ok := headFiles[name]; !ok {
			if data, err := realmfs.ReadFile(basePath); err == nil && DeletionAllowed(data) {
				continue
			}
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

// baseLinkMessage explains RD000 for a link in base at path.
func baseLinkMessage(path string) string {
	return "base " + path + " is a link, which the comparison doesn't read, so the change can't be compared with what it held: replace links with regular files, with the CLI you use now, before upgrading"
}

// DeletionAllowed reports whether deleting an entity file with the base content
// data is not a rule deletion (RD005): data has a top-level "realm" key and no
// entity content, so it holds no rules.
func DeletionAllowed(data []byte) bool {
	return entity.HoldsNoEntity(data)
}

// isAbsent reports whether nothing is at path. A dangling link is something, at
// path or above it, and an empty path names nothing to look for.
func isAbsent(path string, lstat func(string) (fs.FileInfo, error)) bool {
	if path == "" {
		return false
	}
	if _, err := lstat(path); !errors.Is(err, fs.ErrNotExist) {
		return false
	}
	parent := filepath.Dir(path)
	if parent == path {
		return true
	}
	if info, err := os.Stat(parent); err == nil {
		return info.IsDir() // Windows says not found below a file too.
	}
	return isAbsent(parent, lstat)
}

// scanJSONFiles returns a map of filename → full path for all *.json files
// directly inside dir, except the realm.json manifest, and the names of the
// ones that are links or special files, which are never read. Non-JSON files
// and I/O errors are silently skipped.
func scanJSONFiles(dir string) (files map[string]string, links []string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil
	}
	files = make(map[string]string, len(entries))
	for _, e := range entries {
		if realmfs.CheckEntry(e) != nil {
			if realmfs.IsJSONName(e.Name()) {
				links = append(links, e.Name())
			}
			continue
		}
		// realm.json is a manifest and index.json a published index or an
		// error, both judged by CompareRealms: neither is an entity file.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") || e.Name() == "realm.json" || e.Name() == "index.json" {
			continue
		}
		files[e.Name()] = filepath.Join(dir, e.Name())
	}
	return files, links
}
