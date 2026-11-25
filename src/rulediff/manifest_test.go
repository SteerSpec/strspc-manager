package rulediff_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
)

const (
	plainEntity = `{"entity":{"id":"ENT"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},` +
		`"rules":[{"id":"ENT-001","revision":1,"state":"I","body":"MUST hold"}]}`
	mixedEntity = `{"realm":{},"entity":{"id":"ENT"},"rule_set":{"version":"1.0.1","timestamp":"2026-01-02T00:00:00Z","hash":null},` +
		`"rules":[{"id":"ENT-001","revision":1,"state":"I","body":"MUST hold"}]}`
	repairedEntity = `{"entity":{"id":"ENT"},"rule_set":{"version":"1.0.2","timestamp":"2026-01-03T00:00:00Z","hash":null},` +
		`"rules":[{"id":"ENT-001","revision":1,"state":"I","body":"MUST hold"}]}`
)

func hasCode(res *result.Result, code string) bool {
	for _, d := range res.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestDiffRejectsRealmKeyInHeadEntity(t *testing.T) {
	if res := rulediff.Compare([]byte(plainEntity), []byte(mixedEntity)); !hasCode(res, "RD000") {
		t.Errorf("Compare: no RD000: %+v", res.Diagnostics)
	}
	if res := rulediff.CompareNew([]byte(mixedEntity)); !hasCode(res, "RD000") {
		t.Errorf("CompareNew: no RD000: %+v", res.Diagnostics)
	}
}

// A base that already has the key must not block the change that removes it,
// nor hide what else that change does.
func TestDiffComparesRepairOfBaseWithRealmKey(t *testing.T) {
	if res := rulediff.Compare([]byte(mixedEntity), []byte(repairedEntity)); !res.OK() {
		t.Errorf("repair: %+v", res.Diagnostics)
	}
	gutted := `{"entity":{"id":"ENT"},"rule_set":{"version":"1.0.2","timestamp":"2026-01-03T00:00:00Z","hash":null},"rules":[]}`
	if res := rulediff.Compare([]byte(mixedEntity), []byte(gutted)); !hasCode(res, "RD005") {
		t.Errorf("repair that deletes a rule: no RD005: %+v", res.Diagnostics)
	}
}

// Turning an entity into a pure manifest deletes its rules.
func TestDiffRejectsEntityTurnedIntoManifest(t *testing.T) {
	if res := rulediff.Compare([]byte(plainEntity), []byte(`{"realm":{},"rules":[{"file":"ENT.json"}]}`)); res.OK() {
		t.Error("no Error")
	}
}

// CompareRealms reads content without Compare, so the key must not cost RD030.
func TestCompareRealmsReadsThroughRealmKey(t *testing.T) {
	const manifest = `{"realm":{"id":"dev.test","title":"T","version":"1.0.0","kind":"standard"}}`
	file := func(key, state string) string {
		return `{` + key + `"entity":{"id":"ENT"},"rules":[{"id":"ENT-001","state":"R","body":"old"},` +
			`{"id":"ENT-002","state":"` + state + `","body":"new","supersedes":"ENT-001"}]}`
	}
	tree := func(body string) string {
		dir := t.TempDir()
		for name, data := range map[string]string{"realm.json": manifest, "ENT.json": body} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	const key = `"realm":{},`
	if res := rulediff.CompareRealms(tree(file("", "D")), tree(file(key, "P"))); !hasCode(res, "RD030") {
		t.Errorf("head with the key: no RD030: %+v", res.Diagnostics)
	}
	if res := rulediff.CompareRealms(tree(file(key, "P")), tree(file("", "P"))); hasCode(res, "RD030") {
		t.Errorf("base with the key: false RD030: %+v", res.Diagnostics)
	}
}

// A stray file with a realm key and no entity content holds no rules, so the
// change that removes it is not a rule deletion; a mixed file still is.
func TestCompareDirAllowsDeletingBareManifest(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		rd005      bool
	}{
		{"bare manifest", `{"realm":{"id":"x"}}`, false},
		{"mixed file", mixedEntity, true},
		{"entity file", plainEntity, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base, head := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(base, "STRAY.json"), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := hasCode(rulediff.CompareDir(base, head), "RD005"); got != tc.rd005 {
				t.Errorf("RD005 = %v, want %v", got, tc.rd005)
			}
		})
	}
	if !rulediff.DeletionAllowed([]byte(`{"realm":{}}`)) || rulediff.DeletionAllowed([]byte(plainEntity)) {
		t.Error("DeletionAllowed disagrees with CompareDir")
	}
}
