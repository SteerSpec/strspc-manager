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
	if err := CheckLocal(p); err != nil {
		return "", err
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

// CheckLocal reports an error when the absolute path p, or the text of a link
// met along it, names a network share or device path. Each link's text is
// checked before the link is followed, so that resolving p never reaches such
// a path, as resolving it to check afterwards would.
func CheckLocal(p string) error {
	return checkLocal(p, 0)
}

func checkLocal(p string, depth int) error {
	if !isLocal(p) {
		return fmt.Errorf("%s is not a local path", p)
	}
	if depth > 40 {
		return fmt.Errorf("%s: too many links", p)
	}
	vol := filepath.VolumeName(p)
	cur := vol + string(filepath.Separator)
	parts := strings.Split(strings.Trim(p[len(vol):], string(filepath.Separator)), string(filepath.Separator))
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		next := filepath.Join(cur, part)
		info, err := os.Lstat(next)
		if err != nil {
			return nil // Nothing more to follow; the caller reports it.
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			cur = next
			continue
		}
		text, err := os.Readlink(next)
		if err != nil {
			return err
		}
		if !isLocal(text) {
			return fmt.Errorf("%s links to %s, which is not a local path", next, text)
		}
		if !filepath.IsAbs(text) {
			text = filepath.Join(cur, text)
		}
		return checkLocal(filepath.Join(append([]string{text}, parts[i+1:]...)...), depth+1)
	}
	return nil
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
// a link only to a regular file inside that tree, judged from the link's text
// through real directories, so nothing outside the tree is looked at. It is for a base version,
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
	target, why := inTreeTarget(root, path)
	if why != "" {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	if t, err := os.Lstat(target); err != nil || !t.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: %w", path, ErrNotRegular)
	}
	return os.ReadFile(target)
}

// CheckWalked reports ErrNotRegular for an entry at path, walked below root,
// that could hold or hide an entity file without being one: a link or
// special file whose name ends in .json in any case, or a link that leads to
// a directory, out of the tree, or to another link. Skip reports any other
// entry that is neither a regular file nor a directory: it holds no entity
// file and is left alone like any non-JSON file. A link's target is only
// looked at once its text is known to stay inside the tree, so that a walk
// never reaches out of it, to a network share for one.
func CheckWalked(root, path string, d fs.DirEntry) (skip bool, err error) {
	if CheckEntry(d) == nil {
		return false, nil
	}
	refuse := func(why string) (bool, error) {
		return true, fmt.Errorf("%s%s: %w", path, why, ErrNotRegular)
	}
	if strings.EqualFold(filepath.Ext(d.Name()), ".json") {
		return refuse("")
	}
	if d.Type()&fs.ModeSymlink == 0 {
		return true, nil // A special file, never read.
	}
	target, why := inTreeTarget(root, path)
	if why != "" {
		if why == "dangling" {
			return true, nil // Leads to nothing.
		}
		return refuse(why)
	}
	info, err := os.Lstat(target)
	switch {
	case err != nil:
		return true, nil // Dangling: leads to nothing.
	case info.IsDir():
		return refuse(" links to a directory")
	case info.Mode()&fs.ModeSymlink != 0:
		return refuse(" links to a link")
	}
	return true, nil
}

// inTreeTarget returns the path the link at path names, from its text alone,
// when that stays inside the tree rooted at root through real directories
// only, so it can be looked at without leaving the tree. Otherwise it
// explains why not; "dangling" means a directory on the way is missing.
func inTreeTarget(root, path string) (string, string) {
	text, err := os.Readlink(path)
	if err != nil || filepath.IsAbs(text) || filepath.VolumeName(text) != "" {
		return "", " links out of the tree"
	}
	target := filepath.Join(filepath.Dir(path), text)
	rel, err := filepath.Rel(root, target)
	if err != nil || !filepath.IsLocal(rel) {
		return "", " links out of the tree"
	}
	dir := root
	for _, part := range strings.Split(filepath.Dir(rel), string(filepath.Separator)) {
		if part == "." {
			continue
		}
		dir = filepath.Join(dir, part)
		info, err := os.Lstat(dir)
		if err != nil {
			return "", "dangling"
		}
		if !info.IsDir() {
			return "", " links through a link"
		}
	}
	return target, ""
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

// TargetInTree returns the slash path, below root, of the regular file the
// link at path leads to, when it leads to one inside the tree.
func TargetInTree(root, path string) (string, bool) {
	if info, err := os.Lstat(path); err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return "", false
	}
	target, why := inTreeTarget(root, path)
	if why != "" {
		return "", false
	}
	if t, err := os.Lstat(target); err != nil || !t.Mode().IsRegular() {
		return "", false
	}
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
