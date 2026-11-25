package entity

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/internal/realmfs"
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

// WithExcludeDirs causes the walker to skip the directories at the given paths,
// relative to the walked directory, such as a Realm's sub_realms. A directory
// of the same name deeper in the tree is still walked. An entry that is not a
// clean path below the walked directory, such as "", "." or "../x", excludes
// nothing: it must never hide the walked directory itself, and realm-lint and
// realm-resolve report it (RM008, RR007).
func WithExcludeDirs(dirs []string) WalkOption {
	return func(c *walkConfig) {
		if len(dirs) == 0 {
			return
		}
		c.excludeDirs = make(map[string]bool, len(dirs))
		for _, d := range dirs {
			if !isCleanRelDir(d) {
				continue
			}
			c.excludeDirs[filepath.ToSlash(d)] = true
		}
	}
}

// isCleanRelDir reports whether d is a clean relative path strictly below the
// directory it is relative to.
func isCleanRelDir(d string) bool {
	return d != "" && filepath.Clean(d) == d && filepath.IsLocal(d) && d != "."
}

// WalkEntityFiles walks dir for .json entity files, skipping _schema/
// directories, realm.json and a published index.json. For each other JSON file
// found, it reads and parses the file, then calls fn with the results: a
// manifest under another name, or any other index.json, is a parse failure.
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

// walkRoot returns the directory to walk for dir: dir itself when it is a
// file, which is then the only entry, or else the physical directory it names.
func walkRoot(dir string) (string, error) {
	if info, err := os.Lstat(dir); err == nil && info.Mode().IsRegular() {
		return dir, nil
	}
	return realmfs.Root(dir)
}

// under maps path, below root, to the same entry below dir as given.
func under(dir, root, path string) string {
	if dir == root {
		return path
	}
	if path == root {
		return dir
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.Join(dir, rel)
}

// refused reports an entry that is neither a regular file nor a directory.
func refused(path string, fn WalkFunc) error {
	return fn(path, nil, nil, fmt.Errorf("%s: %w", path, realmfs.ErrNotRegular))
}

// walkRecursive walks the full directory tree.
func walkRecursive(dir string, userFn WalkFunc, cfg *walkConfig) error {
	// filepath.WalkDir compares the error with fs.SkipAll using ==, so a
	// callback's wrapped fs.SkipAll would abort the walk with an error. Return
	// the bare value, as walkShallow's errors.Is does.
	fn := func(path string, data []byte, ef *File, parseErr error) error {
		err := userFn(path, data, ef, parseErr)
		if errors.Is(err, fs.SkipAll) {
			return fs.SkipAll
		}
		return err
	}
	root, err := walkRoot(dir)
	if err != nil {
		return err
	}
	return filepath.WalkDir(root, func(physical string, d fs.DirEntry, err error) error {
		path := under(dir, root, physical)
		if err != nil {
			if physical == root {
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
			if cfg.excludeDirs != nil {
				if rel, err := filepath.Rel(root, physical); err == nil && cfg.excludeDirs[filepath.ToSlash(rel)] {
					return fs.SkipDir
				}
			}
			return nil
		}
		if skip, err := realmfs.CheckWalked(physical, d); err != nil {
			return refused(path, fn)
		} else if skip {
			return nil
		}
		return processEntry(path, d.Name(), fn)
	})
}

// walkShallow processes only the immediate children of dir.
func walkShallow(dir string, fn WalkFunc, cfg *walkConfig) error {
	root, err := walkRoot(dir)
	if err != nil {
		return err
	}
	if root == dir {
		if info, err := os.Lstat(dir); err == nil && info.Mode().IsRegular() {
			if err := processEntry(dir, filepath.Base(dir), fn); err != nil && !errors.Is(err, fs.SkipAll) {
				return err
			}
			return nil
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		var err error
		skip, linkErr := realmfs.CheckWalked(filepath.Join(root, entry.Name()), entry)
		switch {
		case linkErr != nil:
			err = refused(path, fn)
		case skip, entry.IsDir():
			continue
		default:
			err = processEntry(path, entry.Name(), fn)
		}
		if err != nil {
			if errors.Is(err, fs.SkipAll) {
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

	data, err := realmfs.ReadFile(path)
	if err != nil {
		return fn(path, nil, nil, err)
	}

	if name == "index.json" {
		if isPublishedIndex(data) {
			return nil
		}
		// Reserved, so that nothing enforced can be swapped for an index.
		return fn(path, data, nil, errReservedIndex)
	}
	if IsRealmJSON(data) {
		return fn(path, data, nil, fmt.Errorf("parsing entity JSON: %w", errRealmKey))
	}

	ef, parseErr := Parse(data)
	return fn(path, data, ef, parseErr)
}
