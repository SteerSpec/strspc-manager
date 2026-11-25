// Package realmfs reads Realm trees without following links. Links above a
// tree, in the path naming its root, are resolved; below the root, an entry
// that is neither a regular file nor a directory is refused, so that every
// reader sees the same files the comparison does.
package realmfs

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/SteerSpec/strspc-manager/src/internal/localpath"
)

// isLocal reports whether a resolved root is on a local drive. Tests replace
// it to exercise the Windows-only refusal.
var isLocal = localpath.IsLocal

// ErrNotRegular reports an entry in a Realm tree that is a link or another
// special file.
var ErrNotRegular = errors.New("links and special files are not allowed in a Realm tree")

// Root resolves dir, the root of a Realm tree, to the physical directory it
// names. A relative dir is taken against the physical working directory, and
// links are resolved before any ".." is applied. The result must be a real
// directory, the one the OS opens for dir.
func Root(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("no directory")
	}
	p := dir
	if !filepath.IsAbs(p) {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		if wd, err = filepath.EvalSymlinks(wd); err != nil {
			return "", err
		}
		p = wd + string(filepath.Separator) + dir
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", err
	}
	if !isLocal(resolved) {
		return "", fmt.Errorf("%s resolves to %s, which is not a local directory", dir, resolved)
	}
	info, err := os.Lstat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	given, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if !os.SameFile(given, info) {
		return "", fmt.Errorf("%s does not resolve to the directory it opens", dir)
	}
	return resolved, nil
}

// ReadFile reads the regular file at path. A link, a directory or a special
// file there is ErrNotRegular.
func ReadFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return os.ReadFile(path)
}

// Dir checks that path is a real directory, not a link to one.
func Dir(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return nil
}

// CheckEntry reports ErrNotRegular for a walked entry that is neither a
// regular file nor a directory.
func CheckEntry(d fs.DirEntry) error {
	if t := d.Type(); t.IsDir() || t.IsRegular() {
		return nil
	}
	return fmt.Errorf("%s: %w", d.Name(), ErrNotRegular)
}

// ReadInTree reads the file at path, below the tree rooted at root, following
// a link only to a regular file inside that tree. It is for a base version,
// whose links a change may be replacing: what they held must still be
// compared. A link out of the tree, or to anything but a regular file, is
// ErrNotRegular.
func ReadInTree(root, path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().IsRegular() {
		return os.ReadFile(path)
	}
	if info.Mode()&fs.ModeSymlink == 0 {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	physRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	rel, err := filepath.Rel(physRoot, target)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, fmt.Errorf("%s: links out of the tree: %w", path, ErrNotRegular)
	}
	if t, err := os.Lstat(target); err != nil || !t.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return os.ReadFile(target)
}

// CheckWalked reports ErrNotRegular for a walked entry at path that could
// hold or hide an entity file without being one: a link or special file named
// *.json, or a link to a directory. Skip reports another entry that is neither
// a regular file nor a directory, which holds no entity file and is left alone
// like any non-JSON file.
func CheckWalked(path string, d fs.DirEntry) (skip bool, err error) {
	if CheckEntry(d) == nil {
		return false, nil
	}
	if strings.HasSuffix(d.Name(), ".json") {
		return true, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	if info, statErr := os.Stat(path); statErr == nil && info.IsDir() {
		return true, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return true, nil
}

// FoldKey returns a key equal for two names that differ only by case, as a
// case-insensitive file system would match them: each rune is mapped to the
// smallest rune of its simple case folding orbit.
func FoldKey(name string) string {
	return strings.Map(func(r rune) rune {
		lowest := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			lowest = min(lowest, f)
		}
		return lowest
	}, name)
}
