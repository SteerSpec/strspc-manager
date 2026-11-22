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

// KindOf returns entity.KindStandard for a file that is, without doubt, a
// Standard Realm's, and the empty kind of a code Realm for any other.
//
// The Realm is that of the first manifest from the root down to the file, or
// of the sub-realm it declares that holds the file, and has to declare the
// standard kind. Each of these is a doubt:
//   - the file is outside the root or in no Realm;
//   - a manifest on the way can't be loaded, or a declared sub-realm has none;
//   - any other manifest lies below the Realm's, whatever its kind, so a
//     manifest above a Realm can't relabel it;
//   - the layout is invalid: a sub_realms entry that is no directory name or
//     differs only in case from the file's, or a sub-realm declaring its own.
func (f *Finder) KindOf(path string) string {
	root, err := filepath.Abs(f.root)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, filepath.Dir(abs))
	if err != nil || !filepath.IsLocal(rel) {
		return ""
	}

	dirs := []string{root}
	var names []string
	if rel != "." {
		names = strings.Split(rel, string(filepath.Separator))
		for _, name := range names {
			dirs = append(dirs, filepath.Join(dirs[len(dirs)-1], name))
		}
	}
	first := slices.IndexFunc(dirs, func(dir string) bool { return f.load(dir).found })
	if first < 0 {
		return ""
	}
	realm := f.load(dirs[first]).file
	if realm == nil || slices.ContainsFunc(realm.SubRealms, notDirName) {
		return ""
	}
	if first < len(names) {
		name := names[first]
		switch {
		case slices.Contains(realm.SubRealms, name):
			first++
			realm = f.load(dirs[first]).file
			// Nesting is one level deep (RR010).
			if realm == nil || len(realm.SubRealms) > 0 {
				return ""
			}
		case slices.ContainsFunc(realm.SubRealms, func(s string) bool { return strings.EqualFold(s, name) }):
			return ""
		}
	}
	if realm.Realm.Kind != entity.KindStandard {
		return ""
	}
	if slices.ContainsFunc(dirs[first+1:], func(dir string) bool { return f.load(dir).found }) {
		return ""
	}
	return entity.KindStandard
}

// notDirName reports whether a sub_realms entry is not the name of a directory
// directly inside its Realm.
func notDirName(s string) bool {
	return s == "." || !filepath.IsLocal(s) || strings.ContainsAny(s, `/\`)
}

// load returns the manifest of dir.
func (f *Finder) load(dir string) manifest {
	if m, ok := f.manifests[dir]; ok {
		return m
	}
	var m manifest
	path := filepath.Join(dir, "realm.json")
	// Lstat, so that a dangling link is a manifest that can't be loaded.
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		m.found = true
		if rf, err := entity.LoadRealm(path); err == nil {
			m.file = rf
		}
	}
	f.manifests[dir] = m
	return m
}
