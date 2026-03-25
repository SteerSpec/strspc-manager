package entity

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// WalkFunc is the callback invoked for each JSON file found by WalkEntityFiles.
// If the file parsed successfully as an entity, ef is non-nil and parseErr is nil.
// If parsing failed, ef is nil and parseErr describes the failure.
// Return filepath.SkipDir or filepath.SkipAll to control the walk, or any
// other non-nil error to abort.
type WalkFunc func(path string, data []byte, ef *File, parseErr error) error

// WalkOption configures WalkEntityFiles behavior.
type WalkOption func(*walkConfig)

type walkConfig struct {
	recursive bool
}

// WithRecursive controls whether subdirectories are walked.
// The default is true (recursive).
func WithRecursive(b bool) WalkOption {
	return func(c *walkConfig) { c.recursive = b }
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
		return walkRecursive(dir, fn)
	}
	return walkShallow(dir, fn)
}

// walkRecursive walks the full directory tree.
func walkRecursive(dir string, fn WalkFunc) error {
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				return fs.SkipDir
			}
			return nil
		}
		return processEntry(path, d.Name(), fn)
	})
}

// walkShallow processes only the immediate children of dir.
func walkShallow(dir string, fn WalkFunc) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := processEntry(filepath.Join(dir, entry.Name()), entry.Name(), fn); err != nil {
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
