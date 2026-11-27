package rulediff

import (
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// Removing a duplicated sub_realms entry from a Standard Realm's manifest
// turns evaluation of its files off: with the duplicate, the manifest was
// invalid and the files were a code Realm's. RD028 stays, and says why the
// files were code, so the author sees the repair is what changes the kind.
func TestCompareRealms_RD028NamesTheInvalidManifest(t *testing.T) {
	dup := `{"realm":{"id":"a.b","kind":"standard"},"sub_realms":["app","app"]}`
	fixed := `{"realm":{"id":"a.b","kind":"standard"},"sub_realms":["app"]}`
	res := compareTrees(t,
		tree{"realm.json": dup, "STD.json": someEntity, childManifest: stdChild},
		tree{"realm.json": fixed, "STD.json": someEntity, childManifest: stdChild})
	d := findCode(res, "RD028")
	if d == nil {
		t.Fatalf("no RD028: %v", res.Diagnostics)
	}
	for _, want := range []string{`base's realm.json is not a valid manifest`, `"app" is listed twice`} {
		if !strings.Contains(d.Message, want) {
			t.Errorf("message %q does not say %q", d.Message, want)
		}
	}

	// A kind change between valid manifests names none.
	res = compareTrees(t, tree{"realm.json": codeRealm, "STD.json": someEntity}, tree{"realm.json": stdRealm, "STD.json": someEntity})
	for _, d := range res.Diagnostics {
		if d.Code == "RD028" && strings.Contains(d.Message, "not a valid manifest") {
			t.Errorf("message names an invalid manifest: %s", d.Message)
		}
	}
}

// A code Realm's file below a standard manifest the Realm doesn't declare is
// a Standard Realm's for a source rooted at that manifest's directory, so
// RD030 holds for it there, and says for which root.
func TestCompareRealms_RD030NamesTheRoot(t *testing.T) {
	app := func(ver, rules string) string {
		return `{"entity":{"id":"APP"},"rule_set":{"version":"` + ver + `"},"rules":[` + rules + `]}`
	}
	retired := `{"id":"APP-001","revision":0,"state":"R","body":"x","added_by":"github:a","added_at":"2026-01-01"}`
	fix := `{"id":"APP-002","revision":0,"state":"D","body":"y","supersedes":"APP-001","added_by":"github:a","added_at":"2026-01-01"}`
	stray := `{"realm":{"id":"x","kind":"standard"}}`
	res := compareTrees(t,
		tree{"realm.json": codeRealm, "x/realm.json": stray, "x/APP.json": app("0.1.0", retired)},
		tree{"realm.json": codeRealm, "x/realm.json": stray, "x/APP.json": app("0.2.0", retired+","+fix)})
	assertCodeSeverity(t, res, "RD030", result.Error)
	if d := findCode(res, "RD030"); d != nil && !strings.Contains(d.Message, `for a source rooted at "x"`) {
		t.Errorf("message %q does not name the root", d.Message)
	}

	// From the tree's root, a Standard Realm's RD030 names no root.
	dead := `{"id":"STD-001","revision":0,"state":"T","body":"x","clause":"1","added_by":"github:a","added_at":"2026-01-01"}`
	sfix := `{"id":"STD-002","revision":0,"state":"D","body":"y","clause":"1","supersedes":"STD-001","added_by":"github:a","added_at":"2026-01-01"}`
	std := func(rules string) string { return `{"entity":{"id":"STD"},"rules":[` + rules + `]}` }
	res = compareTrees(t, tree{"realm.json": stdRealm, "STD.json": std(dead)}, tree{"realm.json": stdRealm, "STD.json": std(dead + "," + sfix)})
	if d := findCode(res, "RD030"); d == nil || strings.Contains(d.Message, "rooted at") {
		t.Errorf("RD030 = %v, want one naming no root", d)
	}
}
