package realmresolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// layout is one shape of a Realm tree with an inheriting entity in it.
type layout struct {
	root, subs, sub, deep, at string
}

// standard says whether the entity's file is a Standard Realm's without doubt.
// It is the whole list of the layouts whose rules escape evaluation: any other
// layout is a code Realm's.
func (l layout) standard() bool {
	validRoot := l.root == "code" || l.root == "standard"
	switch {
	case !validRoot:
		return false
	case l.subs != "none" && l.subs != "sub" && l.subs != "case":
		return false // An invalid sub_realms.
	case l.at == ".":
		return l.root == "standard"
	case l.subs == "case":
		return false
	case l.at == "sub/deep" && l.deep != "none":
		return false
	case l.subs == "sub":
		return l.sub == "standard"
	}
	return l.root == "standard" && l.sub == "none"
}

// write writes the tree of l under dir, and beside it a code Realm that depends
// on it and inherits from its entity.
func (l layout) write(t *testing.T, dir string) (tree, app string) {
	t.Helper()
	subs := map[string]string{
		"none": "", "sub": `["sub"]`, "case": `["Sub"]`, "dot": `["sub","."]`,
		"parent": `["sub",".."]`, "path": `["sub","a/b"]`, "twice": `["sub","sub"]`,
	}[l.subs]
	if subs != "" {
		subs = `,"sub_realms":` + subs
	}
	const head, kind = `{"realm":{"title":"R","version":"1.0.0"`, `,"kind":"standard"`
	roots := map[string]string{
		"code":     head + `,"id":"dev.x"}` + subs + `}`,
		"standard": head + `,"id":"dev.x"` + kind + `}` + subs + `}`,
		"no id":    head + kind + `}` + subs + `}`,
		"broken":   "{",
	}
	children := map[string]string{
		"code":     head + `,"id":"dev.x.sub"}}`,
		"standard": head + `,"id":"dev.x.sub"` + kind + `}}`,
		"wrong id": head + `,"id":"dev.other"` + kind + `}}`,
		"nesting":  head + `,"id":"dev.x.sub"` + kind + `},"sub_realms":["deep"]}`,
		"broken":   "{",
	}
	deeps := map[string]string{
		"code":     head + `,"id":"dev.x.sub.deep"}}`,
		"standard": head + `,"id":"dev.x.sub.deep"` + kind + `}}`,
	}
	ent := func(id, extra string) string {
		return `{"entity":{"id":"` + id + `","title":"E","visibility":"public"` + extra + `},"rule_set":{"version":"0.1.0","hash":null},"rules":[],"notes":[]}`
	}
	files := map[string]string{
		"tree/realm.json":                roots[l.root],
		"tree/" + l.at + "/AAA.json":     ent("AAA", ""),
		"tree/" + l.at + "/BBB.json":     ent("BBB", `,"inherits":"AAA"`),
		"app/APP.json":                   ent("APP", `,"inherits":"dev.x:AAA"`),
		"app/realm.json":                 head + `,"id":"dev.app"},"dependencies":[{"realm_id":"dev.x","version":"1.0.0","source":"../tree"}]}`,
		"tree/sub/realm.json":            children[l.sub],
		"tree/sub/deep/realm.json":       deeps[l.deep],
		"tree/sub/deep/README.md":        "keeps the directories there",
		"tree/_schema/entity.schema.txt": "not read",
	}
	for name, content := range files {
		if content == "" {
			continue
		}
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "tree"), filepath.Join(dir, "app")
}

// Every layout, through every way a rule reaches evaluation: rule-resolve on
// the tree, a chain in the tree, and a chain into the tree as a dependency.
// None gives the standard kind outside the whitelist, so the resolvers can't
// drift apart and a malformed tree can't switch evaluation off (STDRLM-011).
func TestRealmKindLayouts(t *testing.T) {
	var layouts []layout
	for _, root := range []string{"code", "standard", "no id", "broken"} {
		for _, subs := range []string{"none", "sub", "case", "dot", "parent", "path", "twice"} {
			for _, sub := range []string{"none", "code", "standard", "wrong id", "nesting", "broken"} {
				for _, deep := range []string{"none", "code", "standard"} {
					for _, at := range []string{".", "sub", "sub/deep"} {
						layouts = append(layouts, layout{root, subs, sub, deep, at})
					}
				}
			}
		}
	}
	var inTree, inDep int
	for _, l := range layouts {
		tree, app := l.write(t, t.TempDir())
		want := ""
		if l.standard() {
			want = entity.KindStandard
		}

		rr, err := ruleresolve.New([]ruleresolve.SourceEntry{{Source: tree}}, ruleresolve.WithBaseDir(tree))
		if err != nil {
			t.Fatal(err)
		}
		files, res := rr.Resolve(t.Context())
		if len(files) != 2 {
			t.Fatalf("%+v: rule-resolve gave %d files, want 2: %v", l, len(files), res.Diagnostics)
		}
		for _, f := range files {
			if f.RealmKind != want {
				t.Errorf("%+v: rule-resolve gives %q, want %q", l, f.RealmKind, want)
			}
		}

		// A tree that realm-resolve rejects may have no chain; any it has must agree.
		if rf, err := entity.LoadRealm(filepath.Join(tree, "realm.json")); err == nil {
			got, _ := New().Resolve(t.Context(), rf, tree)
			for _, c := range got.Inheritance {
				inTree++
				if c.Entity.RealmKind != want || c.Ancestors[0].RealmKind != want {
					t.Errorf("%+v: a chain in the tree gives %q and %q, want %q", l, c.Entity.RealmKind, c.Ancestors[0].RealmKind, want)
				}
			}
		}
		rf, err := entity.LoadRealm(filepath.Join(app, "realm.json"))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := New().Resolve(t.Context(), rf, app)
		for _, c := range got.Inheritance {
			inDep++
			if c.Ancestors[0].RealmKind != want {
				t.Errorf("%+v: a chain into the dependency gives %q, want %q", l, c.Ancestors[0].RealmKind, want)
			}
		}
	}
	if inTree == 0 || inDep == 0 {
		t.Fatalf("compared %d chains in trees and %d into dependencies, want some of each", inTree, inDep)
	}
	t.Logf("%d layouts, %d chains in trees, %d into dependencies", len(layouts), inTree, inDep)
}
