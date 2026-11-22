package ruleresolve

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// realmKinds finds the kind of the Realm holding each file of a source
// directory, reading each manifest once.
type realmKinds struct {
	root      string
	manifests map[string]realmManifest
}

// realmManifest is what a directory holds of a manifest.
type realmManifest struct {
	// found reports whether the directory has a manifest, readable or not.
	found bool
	// file is the manifest; nil when there is none or it can't be loaded.
	file *entity.RealmFile
}

// newRealmKinds returns a realmKinds for the source directory root.
func newRealmKinds(root string) *realmKinds {
	return &realmKinds{root: root, manifests: make(map[string]realmManifest)}
}

// kindOf returns the kind of the Realm holding the file at path. The Realm is
// that of the first manifest from the source root down to the file, or of the
// sub-realm it declares that holds the file, as rule-lint reads a tree: a
// manifest it doesn't declare makes no Realm. A file in no Realm, or in one
// whose manifest can't be loaded, is a code Realm's, whose rules are evaluated.
func (k *realmKinds) kindOf(path string) string {
	rel, err := filepath.Rel(k.root, filepath.Dir(path))
	if err != nil {
		return ""
	}
	var below []string
	if rel != "." {
		below = strings.Split(rel, string(filepath.Separator))
	}
	dir := k.root
	for {
		if m := k.manifest(dir); m.found {
			realm := m.file
			if realm != nil && len(below) > 0 && slices.Contains(realm.SubRealms, below[0]) {
				realm = k.manifest(filepath.Join(dir, below[0])).file
			}
			if realm == nil {
				return ""
			}
			return realm.Realm.Kind
		}
		if len(below) == 0 {
			return ""
		}
		dir, below = filepath.Join(dir, below[0]), below[1:]
	}
}

// manifest returns the manifest of dir.
func (k *realmKinds) manifest(dir string) realmManifest {
	if m, ok := k.manifests[dir]; ok {
		return m
	}
	var m realmManifest
	path := filepath.Join(dir, "realm.json")
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		m.found = true
		if rf, err := entity.LoadRealm(path); err == nil {
			m.file = rf
		}
	}
	k.manifests[dir] = m
	return m
}
