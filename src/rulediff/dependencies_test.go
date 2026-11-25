package rulediff

import (
	"encoding/json"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func manifestWithDeps(t *testing.T, id, kind string, deps ...entity.RealmDep) string {
	t.Helper()
	realm := map[string]string{"id": id}
	if kind != "" {
		realm["kind"] = kind
	}
	data, err := json.Marshal(map[string]any{"realm": realm, "dependencies": deps})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// Finding 6: a dependency keeps its realm ID, so the source it is fetched
// from may change only its ref.
func TestCompareRealms_DependencySource(t *testing.T) {
	dep := func(source string) entity.RealmDep {
		return entity.RealmDep{RealmID: "com.base", Version: "1.0.0", Source: source}
	}
	for _, tc := range []struct {
		name, from, to string
		rd031          bool
	}{
		{"github ref bump", "github://org/core@v1.0.0/rules", "github://org/core@v1.1.0/rules", false},
		{"github owner case", "github://Org/core@v1.0.0/rules", "github://org/core@v2.0.0/rules", false},
		{"local, same path written differently", "../core", "../core/", false},
		{"github owner", "github://org/core@v1/rules", "github://fork/core@v1/rules", true},
		{"github repo", "github://org/core@v1/rules", "github://org/core2@v1/rules", true},
		{"github path", "github://org/core@v1/rules", "github://org/core@v1/rules2", true},
		{"local path", "../core", "../core2", true},
		{"local to github", "../core", "github://org/core@v1/rules", true},
		{"ref to a commit", "github://org/core@v1.0.0/rules", "github://org/core@" + "0123456789abcdef0123456789abcdef01234567/rules", true},
		{"ref to a branch", "github://org/core@v1.0.0/rules", "github://org/core@main/rules", true},
		{"ref to a short tag", "github://org/core@v1.0.0/rules", "github://org/core@v2/rules", true},
		{"ref to latest", "github://org/core@v1.0.0/rules", "github://org/core@latest/rules", true},
		{"ref downgraded", "github://org/core@v1.2.0/rules", "github://org/core@v1.1.9/rules", true},
		{"down from a short tag", "github://org/core@v1/rules", "github://org/core@v0.0.1/rules", true},
		{"down from a two-part tag", "github://org/core@v2.0/rules", "github://org/core@v1.9.0/rules", true},
		{"down from a release candidate", "github://org/core@v2.0.0-rc1/rules", "github://org/core@v1.0.0/rules", true},
		{"up from a short tag", "github://org/core@v1/rules", "github://org/core@v1.2.0/rules", false},
		{"tag beyond semver", "github://org/core@v2.0.0/rules", "github://org/core@v1.0.99999999999999999999/rules", true},
		{"from a commit to a tag", "github://org/core@0123456789abcdef0123456789abcdef01234567/rules", "github://org/core@v1.0.0/rules", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := tree{"realm.json": manifestWithDeps(t, "a.b", "", dep(tc.from))}
			head := tree{"realm.json": manifestWithDeps(t, "a.b", "", dep(tc.to))}
			if got := hasDiag(compareTrees(t, base, head), "RD031"); got != tc.rd031 {
				t.Errorf("RD031 = %v, want %v", got, tc.rd031)
			}
		})
	}
	t.Run("dependency added or removed", func(t *testing.T) {
		none := tree{"realm.json": manifestWithDeps(t, "a.b", "")}
		one := tree{"realm.json": manifestWithDeps(t, "a.b", "", dep("../core"))}
		if hasDiag(compareTrees(t, none, one), "RD031") || hasDiag(compareTrees(t, one, none), "RD031") {
			t.Error("RD031 for an added or removed dependency")
		}
	})
}

// A Realm keeps its kind wherever its manifest is: a copy of a code Realm
// with its ID, marked standard elsewhere in the tree, is RD028.
func TestCompareRealms_KindByRealmID(t *testing.T) {
	base := tree{"core/realm.json": manifestWithDeps(t, "com.base", "")}
	head := base.with(tree{"core2/realm.json": manifestWithDeps(t, "com.base", "standard")})
	assertHasCode(t, compareTrees(t, base, head), "RD028")
	same := base.with(tree{"core2/realm.json": manifestWithDeps(t, "com.base", "")})
	if hasDiag(compareTrees(t, base, same), "RD028") {
		t.Error("RD028 for a copy of the same kind")
	}
}

// The source of a dependency is kept by its realm ID across the tree: a
// sub-realm overriding it, or a manifest at a new path, can't repoint it.
func TestCompareRealms_DependencySourceAcrossTheTree(t *testing.T) {
	core := entity.RealmDep{RealmID: "com.base", Version: "1.0.0", Source: "github://org/core@v1.0.0/rules"}
	fork := core
	fork.Source = "github://fork/core@v1.0.0/rules"
	bump := core
	bump.Source = "github://org/core@v2.0.0/rules"
	parent := func(deps ...entity.RealmDep) string {
		rf := map[string]any{"realm": map[string]string{"id": "a.b"}, "sub_realms": []string{"app"}, "dependencies": deps}
		data, _ := json.Marshal(rf)
		return string(data)
	}
	base := tree{"realm.json": parent(core), childManifest: manifestWithDeps(t, "a.b.app", "")}
	for _, tc := range []struct {
		name  string
		head  tree
		rd031 bool
	}{
		{"sub-realm overrides with another repository", tree{"realm.json": parent(core), childManifest: manifestWithDeps(t, "a.b.app", "", fork)}, true},
		{"sub-realm overrides with a ref bump", tree{"realm.json": parent(core), childManifest: manifestWithDeps(t, "a.b.app", "", bump)}, false},
		{"manifest moved, source changed", tree{"realm.json": parent(), "app2/realm.json": manifestWithDeps(t, "a.b.app", "", fork)}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDiag(compareTrees(t, base, tc.head), "RD031"); got != tc.rd031 {
				t.Errorf("RD031 = %v, want %v", got, tc.rd031)
			}
		})
	}
}

// A changed ref is bounded by the highest release tag base declares for the
// dependency, wherever it declares it: a weaker declaration elsewhere in the
// tree can't lower the bound.
func TestCompareRealms_DependencyRefBoundAcrossTheTree(t *testing.T) {
	dep := func(ref string) entity.RealmDep {
		return entity.RealmDep{RealmID: "com.base", Version: "1.0.0", Source: "github://org/core@" + ref + "/rules"}
	}
	parent := func(deps ...entity.RealmDep) string {
		data, _ := json.Marshal(map[string]any{"realm": map[string]string{"id": "a.b"}, "sub_realms": []string{"app"}, "dependencies": deps})
		return string(data)
	}
	base := tree{"realm.json": parent(dep("v1.25.0")), childManifest: manifestWithDeps(t, "a.b.app", "", dep("v1.0.0"))}
	for _, tc := range []struct {
		name  string
		head  tree
		rd031 bool
	}{
		{"parent lowered to the sub-realm's old tag", tree{"realm.json": parent(dep("v1.1.0")), childManifest: manifestWithDeps(t, "a.b.app", "", dep("v1.0.0"))}, true},
		{"parent set to exactly the sub-realm's old source", tree{"realm.json": parent(dep("v1.0.0")), childManifest: manifestWithDeps(t, "a.b.app", "", dep("v1.0.0"))}, true},
		{"sub-realm unchanged", base, false},
		{"sub-realm raised, still below the parent", tree{"realm.json": parent(dep("v1.25.0")), childManifest: manifestWithDeps(t, "a.b.app", "", dep("v1.5.0"))}, false},
		{"sub-realm raised to the parent's tag", tree{"realm.json": parent(dep("v1.25.0")), childManifest: manifestWithDeps(t, "a.b.app", "", dep("v1.25.0"))}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasDiag(compareTrees(t, base, tc.head), "RD031"); got != tc.rd031 {
				t.Errorf("RD031 = %v, want %v", got, tc.rd031)
			}
		})
	}
}
