// Package realmfs reads Realm trees without following links. Below a tree's
// root no link is ever followed: a link or special file whose name ends in
// .json, in any case, is refused, and any other is left alone like any
// non-JSON file, since no reader descends into it. Links in the path naming
// the root are resolved, except on Windows, where one there could lead to a
// network share and is refused instead.
package realmfs

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/SteerSpec/strspc-manager/src/internal/localpath"
)

// isLocal reports whether a path is on a local drive, and followRootLinks
// whether links in the path naming a root are followed. Tests replace them to
// exercise the Windows behaviour.
var (
	isLocal         = localpath.IsLocal
	followRootLinks = runtime.GOOS != "windows"
)

// ErrNotRegular reports an entry in a Realm tree that is a link or another
// special file.
var ErrNotRegular = errors.New("links and special files are not allowed in a Realm tree")

// ErrNotDirOnPath reports that a directory on the way to a file is not a real
// one: a link, or a file. It wraps ErrNotRegular.
var ErrNotDirOnPath = fmt.Errorf("not a directory on the way: %w", ErrNotRegular)

// Root resolves dir, the root of a Realm tree, to the physical directory it
// names. A relative dir is taken against the physical working directory, and
// links are resolved before any ".." is applied. The result must be a real
// directory on a local drive, the one the OS opens for dir. On Windows a link
// anywhere in the path is refused without being followed.
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
		if followRootLinks {
			if wd, err = filepath.EvalSymlinks(wd); err != nil {
				return "", err
			}
		}
		p = wd + string(filepath.Separator) + dir
	}
	if err := CheckLocal(p); err != nil {
		return "", err
	}
	resolved := filepath.Clean(p)
	if followRootLinks {
		var err error
		if resolved, err = filepath.EvalSymlinks(p); err != nil {
			return "", err
		}
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

// CheckLocal reports an error when the absolute path p names a network share
// or device path, or, on Windows, when a component of p is a link: following
// one could reach a network share before any check of where it leads. Each
// component is looked at only once those above it are known to be real
// directories, so the check itself never follows a link.
func CheckLocal(p string) error {
	if !isLocal(p) {
		return fmt.Errorf("%s is not a local path", p)
	}
	if followRootLinks {
		return nil
	}
	// Walk the path as written: each component is checked before a later ".."
	// can take it back, as the OS follows a link before applying "..".
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	for _, part := range strings.FieldsFunc(p[len(vol):], func(r rune) bool { return r == '/' || r == '\\' }) {
		switch part {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return nil // Nothing more to follow; the caller reports it.
		}
		if info.Mode()&fs.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return fmt.Errorf("%s is a link: links in the path to a Realm tree are not followed on Windows", cur)
		}
	}
	return nil
}

// CheckRoot reports an error when dir, made absolute, may not name a Realm
// tree's root: a network share or device path, or, on Windows, a path holding
// a link (CheckLocal). Entry points call it before reading anything below dir.
func CheckRoot(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	return CheckLocal(abs)
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
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// The file opened must be the one checked: a link swapped in between
	// would otherwise be followed.
	if opened, err := f.Stat(); err != nil || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("%s changed while being read: %w", path, ErrNotRegular)
	}
	return io.ReadAll(f)
}

// ReadUnder reads the regular file at the slash path rel below root, through
// real directories only: a link among them, or at rel, is ErrNotRegular and
// is not followed.
func ReadUnder(root, rel string) ([]byte, error) {
	cur := root
	parts := strings.Split(rel, "/")
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s: %w", cur, ErrNotDirOnPath)
		}
	}
	return ReadFile(filepath.Join(cur, parts[len(parts)-1]))
}

// DirFrom checks that the relative path rel, walked from the directory dir as
// written, passes through real directories only and ends at one: a link
// anywhere on the way is ErrNotRegular and is not followed. ".." goes up
// lexically, as the OS does once no link is followed. It returns the path.
func DirFrom(dir, rel string) (string, error) {
	cur, err := filepath.Abs(dir) // So that ".." goes up as the OS goes.
	if err != nil {
		return "", err
	}
	if err := CheckLocal(cur); err != nil {
		return "", err
	}
	for _, part := range strings.FieldsFunc(rel, func(r rune) bool { return r == '/' || r == filepath.Separator }) {
		switch part {
		case ".":
			continue
		case "..":
			cur = filepath.Dir(cur)
			continue
		}
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return "", err
		}
		if !info.IsDir() {
			return "", fmt.Errorf("%s: %w", cur, ErrNotRegular)
		}
	}
	return cur, nil
}

// RegularUnder reports whether the slash path rel below root names a regular
// file reached through real directories only.
func RegularUnder(root, rel string) (bool, error) {
	parts := strings.Split(rel, "/")
	cur := root
	for _, part := range parts[:len(parts)-1] {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil || !info.IsDir() {
			return false, nil
		}
	}
	info, err := os.Lstat(filepath.Join(cur, parts[len(parts)-1]))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
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

// CheckWalked judges a walked entry at path without following it. A link or
// special file whose name ends in .json, in any case, is ErrNotRegular: it
// stands where an entity file or manifest would be read. Skip reports any
// other entry that is neither a regular file nor a directory: no reader
// follows it, so it holds nothing and is left alone like a non-JSON file.
func CheckWalked(path string, d fs.DirEntry) (skip bool, err error) {
	if CheckEntry(d) == nil {
		return false, nil
	}
	if IsJSONName(d.Name()) {
		return true, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return true, nil
}

// IsJSONName reports whether name ends in .json in any case, as a
// case-insensitive file system would open it.
func IsJSONName(name string) bool {
	return strings.EqualFold(filepath.Ext(name), ".json")
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
