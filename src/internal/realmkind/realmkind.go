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

// KindOf returns the kind of the Realm holding the file at path: that of the
// first manifest from the root down to the file, or of the sub-realm it
// declares that holds the file. The kind is entity.KindStandard only when
// every manifest below that one down to the file says so too, so a manifest
// above a code Realm can't relabel it. A file outside the root, in no Realm, or
// under a manifest that can't be loaded has the empty kind of a code Realm.
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
	if realm != nil && first < len(names) && slices.Contains(realm.SubRealms, names[first]) {
		first++
		realm = f.load(dirs[first]).file
	}
	if realm == nil || realm.Realm.Kind != entity.KindStandard {
		return ""
	}
	for _, dir := range dirs[first+1:] {
		if m := f.load(dir); m.found && (m.file == nil || m.file.Realm.Kind != entity.KindStandard) {
			return ""
		}
	}
	return entity.KindStandard
}

// load returns the manifest of dir.
func (f *Finder) load(dir string) manifest {
	if m, ok := f.manifests[dir]; ok {
		return m
	}
	var m manifest
	path := filepath.Join(dir, "realm.json")
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		m.found = true
		if rf, err := entity.LoadRealm(path); err == nil {
			m.file = rf
		}
	}
	f.manifests[dir] = m
	return m
}
