package entity

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WalkFunc is the callback invoked by WalkEntityFiles.
//
// Four cases:
//   - Parse success: ef is non-nil, parseErr is nil, data contains the raw bytes.
//   - Parse failure: ef is nil, parseErr describes the failure, data contains the raw bytes.
//   - Read error: ef is nil, data is nil, parseErr describes the I/O error.
//   - Traversal error: ef is nil, data is nil, parseErr describes the access error
//     (e.g. permission denied on a subdirectory). Path may be a directory.
//
// Return fs.SkipAll to stop the walk early (treated as clean termination).
// Return any other non-nil error to abort the walk with that error.
type WalkFunc func(path string, data []byte, ef *File, parseErr error) error

// WalkOption configures WalkEntityFiles behavior.
type WalkOption func(*walkConfig)

type walkConfig struct {
	recursive   bool
	excludeDirs map[string]bool
}

// WithRecursive controls whether subdirectories are walked.
// The default is true (recursive).
func WithRecursive(b bool) WalkOption {
	return func(c *walkConfig) { c.recursive = b }
}

// WithExcludeDirs causes the walker to skip directories whose names
// match any entry in dirs. The comparison uses the directory base name,
// not the full path.
func WithExcludeDirs(dirs []string) WalkOption {
	return func(c *walkConfig) {
		if len(dirs) == 0 {
			return
		}
		c.excludeDirs = make(map[string]bool, len(dirs))
		for _, d := range dirs {
			c.excludeDirs[d] = true
		}
	}
}

// WalkEntityFiles walks dir for .json entity files, skipping _schema/
// directories and realm manifests. For each entity JSON file found, it
// reads and parses the file, then calls fn with the results.
//
// By default the walk is recursive. Use WithRecursive(false) for
// single-directory scanning.
func WalkEntityFiles(dir string, fn WalkFunc, opts ...WalkOption) error {
	cfg := walkConfig{recursive: true}
	for _, opt := range opts {
		opt(&cfg)
	}

	if cfg.recursive {
		return walkRecursive(dir, fn, &cfg)
	}
	return walkShallow(dir, fn, &cfg)
}

// walkRecursive walks the full directory tree.
func walkRecursive(dir string, fn WalkFunc, cfg *walkConfig) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == dir {
				// Root directory error (e.g. not found) — abort the walk.
				return err
			}
			// Forward subdirectory traversal errors to the callback so
			// callers can record diagnostics and continue.
			return fn(path, nil, nil, err)
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				return fs.SkipDir
			}
			if cfg.excludeDirs[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		return processEntry(path, d.Name(), fn)
	})
}

// walkShallow processes only the immediate children of dir.
func walkShallow(dir string, fn WalkFunc, cfg *walkConfig) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := processEntry(filepath.Join(dir, entry.Name()), entry.Name(), fn); err != nil {
			if err == fs.SkipAll {
				return nil
			}
			return err
		}
	}
	return nil
}

// processEntry handles a single file: filters, reads, parses, and calls fn.
func processEntry(path, name string, fn WalkFunc) error {
	if !strings.HasSuffix(name, ".json") || name == "realm.json" {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fn(path, nil, nil, err)
	}

	if IsRealmJSON(data) {
		return nil
	}

	ef, parseErr := Parse(data)
	return fn(path, data, ef, parseErr)
}
