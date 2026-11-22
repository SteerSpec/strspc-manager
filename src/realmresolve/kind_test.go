package realmresolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

// Each hop and each rule of an effective set has the kind its own Realm's
// manifest declares, also in a dependency's sub-realm (STDRLM-011).
func TestResolveInheritance_RealmKind(t *testing.T) {
	const (
		std         = entity.KindStandard
		childEntity = `{"entity":{"id":"CHD","title":"Child","inherits":"dev.dep:PAR"},"rule_set":{"version":"0.1.0","hash":null},"rules":[{"id":"CHD-001","state":"P","body":"Child."}],"notes":[]}`
		depEntity   = `{"entity":{"id":"PAR","title":"Parent","visibility":"public"},"rule_set":{"version":"0.1.0","hash":null},"rules":[{"id":"PAR-001","state":"P","body":"Parent."}],"notes":[]}`
	)
	manifest := func(id, kind, rest string) string {
		if kind != "" {
			kind = `,"kind":"` + kind + `"`
		}
		return `{"realm":{"id":"` + id + `","title":"R","version":"1.0.0"` + kind + `}` + rest + `}`
	}
	deps := `,"dependencies":[{"realm_id":"dev.dep","version":"1.0.0","source":"../dep"}]`
	tests := []struct {
		name          string
		files         map[string]string
		child, parent string
	}{
		{"Standard Realm dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json": manifest("dev.dep", std, ""), "dep/PAR.json": depEntity,
		}, "", std},
		{"standard sub-realm of a dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json":     manifest("dev.dep", "", `,"sub_realms":["sub"]`),
			"dep/sub/realm.json": manifest("dev.dep.sub", std, ""), "dep/sub/PAR.json": depEntity,
		}, "", std},
		{"code sub-realm of a Standard Realm dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json":     manifest("dev.dep", std, `,"sub_realms":["sub"]`),
			"dep/sub/realm.json": manifest("dev.dep.sub", "", ""), "dep/sub/PAR.json": depEntity,
		}, "", ""},
		{"sub-realm without a manifest in a Standard Realm dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json": manifest("dev.dep", std, `,"sub_realms":["sub"]`), "dep/sub/PAR.json": depEntity,
		}, "", ""},
		{"unreadable sub-realm manifest in a Standard Realm dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json":     manifest("dev.dep", std, `,"sub_realms":["sub"]`),
			"dep/sub/realm.json": "{", "dep/sub/PAR.json": depEntity,
		}, "", ""},
		{"undeclared code manifest in a Standard Realm dependency", map[string]string{
			"app/realm.json": manifest("dev.app", "", deps), "app/CHD.json": childEntity,
			"dep/realm.json":     manifest("dev.dep", std, ""),
			"dep/sub/realm.json": manifest("dev.dep.sub", "", ""), "dep/sub/PAR.json": depEntity,
		}, "", ""},
		{"undeclared code manifest in the Standard Realm resolved", map[string]string{
			"app/realm.json":     manifest("dev.app", std, deps),
			"app/sub/realm.json": manifest("dev.app.sub", "", ""), "app/sub/CHD.json": childEntity,
			"dep/realm.json": manifest("dev.dep", "", ""), "dep/PAR.json": depEntity,
		}, "", ""},
		{"standard sub-realm inherits from code", map[string]string{
			"app/realm.json":     manifest("dev.app", "", deps+`,"sub_realms":["sub"]`),
			"app/sub/realm.json": manifest("dev.app.sub", std, ""), "app/sub/CHD.json": childEntity,
			"dep/realm.json": manifest("dev.dep", "", ""), "dep/PAR.json": depEntity,
		}, std, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			app := filepath.Join(root, "app")
			rf, err := entity.LoadRealm(filepath.Join(app, "realm.json"))
			if err != nil {
				t.Fatal(err)
			}
			got, res := New().Resolve(t.Context(), rf, app)
			if len(got.Inheritance) != 1 {
				t.Fatalf("got %d chains, want 1: %v", len(got.Inheritance), res.Diagnostics)
			}
			c := got.Inheritance[0]
			if c.Entity.RealmKind != tc.child || c.Ancestors[0].RealmKind != tc.parent {
				t.Errorf("hop kinds = %q and %q, want %q and %q", c.Entity.RealmKind, c.Ancestors[0].RealmKind, tc.child, tc.parent)
			}
			kinds := make(map[string]string)
			for _, r := range inherit.Effective(c.Entity, c.Ancestors, nil) {
				kinds[r.Rule.ID] = r.RealmKind
			}
			if kinds["CHD-001"] != tc.child || kinds["PAR-001"] != tc.parent {
				t.Errorf("rule kinds = %q, want CHD-001 %q and PAR-001 %q", kinds, tc.child, tc.parent)
			}
		})
	}
}

// The kind is found too when the Realm is resolved from a relative directory.
func TestResolveInheritance_RealmKindRelativeDir(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"std/realm.json": `{"realm":{"id":"dev.std","title":"S","version":"1.0.0","kind":"standard"}}`,
		"std/A.json":     `{"entity":{"id":"AAA","title":"A","inherits":"BBB"},"rule_set":{"version":"0.1.0","hash":null},"rules":[],"notes":[]}`,
		"std/B.json":     `{"entity":{"id":"BBB","title":"B"},"rule_set":{"version":"0.1.0","hash":null},"rules":[],"notes":[]}`,
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	rf, err := entity.LoadRealm(filepath.Join("std", "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	got, res := New().Resolve(t.Context(), rf, "std")
	if len(got.Inheritance) != 1 {
		t.Fatalf("got %d chains, want 1: %v", len(got.Inheritance), res.Diagnostics)
	}
	if c := got.Inheritance[0]; c.Entity.RealmKind != entity.KindStandard || c.Ancestors[0].RealmKind != entity.KindStandard {
		t.Errorf("hop kinds = %q and %q, want standard", c.Entity.RealmKind, c.Ancestors[0].RealmKind)
	}
}
