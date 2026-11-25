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
)

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
