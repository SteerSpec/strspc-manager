// Package realmkind finds the kind of the Realm holding an entity file, for
// the callers that act on it. Only a Standard Realm's rules escape evaluation
// (STDRLM-011), so whatever is in doubt is a code Realm's.
package realmkind

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Finder finds the kind of the Realm holding each file below a root directory,
// reading each manifest once. It is not safe for concurrent use.
type Finder struct {
	root      string
	manifests map[string]manifest
}

// manifest is what a directory holds of a realm.json.
type manifest struct {
	// found reports whether the directory has one, readable or not.
	found bool
	// file is the manifest; nil when there is none or it can't be loaded.
	file *entity.RealmFile
}

// New returns a Finder for the directory root.
func New(root string) *Finder {
	return &Finder{root: root, manifests: make(map[string]manifest)}
}

// KindOf returns entity.KindStandard for a file in one of the two layouts
// that make it a Standard Realm's, and the empty kind of a code Realm for any
// other. It is a whitelist, so a layout nobody thought of is evaluated. Of the
// manifests on the way from the root down to the file there is either
//
//   - one, a standard one, with no sub-realm it declares holding the file; or
//   - two, a Realm's and, directly below it, that of the sub-realm it declares
//     by that exact name, which is standard, has the ID <parent>.<name>
//     (RR009) and declares no sub-realm of its own (RR010).
//
// Each manifest is a regular file that loads, has an ID and declares a valid
// kind or none (RLMMNFST-008), and each sub_realms entry is a directory name,
// listed once in any case. The root is
// the top of the tree being resolved, so every caller sees the same manifests.
func (f *Finder) KindOf(path string) string {
	if f.standard(path) {
		return entity.KindStandard
	}
	return ""
}

// standard reports whether the file at path is in one of KindOf's two layouts.
func (f *Finder) standard(path string) bool {
	root, err := filepath.Abs(f.root)
	if err != nil {
		return false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, filepath.Dir(abs))
	if err != nil || !filepath.IsLocal(rel) {
		return false
	}
	var names []string
	if rel != "." {
		names = strings.Split(rel, string(filepath.Separator))
	}

	// at holds how far below the root each manifest on the way is.
	var at []int
	var realms []*entity.RealmFile
	dir := root
	for depth := 0; ; depth++ {
		if m := f.load(dir); m.found {
			if !validManifest(m.file) {
				return false
			}
			at = append(at, depth)
			realms = append(realms, m.file)
		}
		if depth == len(names) {
			break
		}
		dir = filepath.Join(dir, names[depth])
	}

	switch len(realms) {
	case 1:
		realm, below := realms[0], names[at[0]:]
		return realm.Realm.Kind == entity.KindStandard &&
			(len(below) == 0 || !slices.ContainsFunc(realm.SubRealms, func(s string) bool { return strings.EqualFold(s, below[0]) }))
	case 2:
		parent, child, name := realms[0], realms[1], names[at[0]]
		return child.Realm.Kind == entity.KindStandard &&
			at[1] == at[0]+1 &&
			slices.Contains(parent.SubRealms, name) &&
			child.Realm.ID == parent.Realm.ID+"."+name &&
			len(child.SubRealms) == 0
	}
	return false
}

// validManifest reports whether a manifest loaded, has an ID, declares the
// standard kind or none, and lists its sub-realms as directory names, each
// once in any case.
func validManifest(rf *entity.RealmFile) bool {
	if rf == nil || rf.Realm.ID == "" {
		return false
	}
	if k := rf.Realm.Kind; k != "" && k != entity.KindStandard {
		return false
	}
	for i, s := range rf.SubRealms {
		if s == "." || !filepath.IsLocal(s) || strings.ContainsAny(s, `/\`) {
			return false
		}
		if slices.ContainsFunc(rf.SubRealms[:i], func(prev string) bool { return strings.EqualFold(prev, s) }) {
			return false
		}
	}
	return true
}

// load returns the manifest of dir.
func (f *Finder) load(dir string) manifest {
	if m, ok := f.manifests[dir]; ok {
		return m
	}
	var m manifest
	path := filepath.Join(dir, "realm.json")
	// Lstat, so that a link, dangling or not, is a manifest in doubt.
	info, err := os.Lstat(path)
	m.found = !errors.Is(err, fs.ErrNotExist)
	if err == nil && info.Mode().IsRegular() {
		if rf, err := entity.LoadRealm(path); err == nil {
			m.file = rf
		}
	}
	f.manifests[dir] = m
	return m
}
