package rulediff

import "testing"

// Finding 9: repairs that change nothing about evaluation pass.
func TestCompareRealms_Repairs(t *testing.T) {
	for _, tc := range []struct {
		name       string
		base, head tree
		forbidden  string
	}{
		{"unparsable base manifest repaired", tree{"realm.json": brokenJSON}, tree{"realm.json": codeRealm}, "RD000"},
		{"unparsable base manifest deleted", tree{"realm.json": brokenJSON, "APP.json": someEntity}, tree{"APP.json": someEntity}, "RD000"},
		{"invalid kind removed", tree{"realm.json": unknownKind}, tree{"realm.json": codeRealm}, "RD028"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if res := compareTrees(t, tc.base, tc.head); hasDiag(res, tc.forbidden) {
				t.Errorf("%s: %v", tc.forbidden, res.Diagnostics)
			}
		})
	}
	// A broken head is still refused, and so is a valid kind made invalid.
	assertHasCode(t, compareTrees(t, tree{"realm.json": codeRealm}, tree{"realm.json": brokenJSON}), "RD000")
	assertHasCode(t, compareTrees(t, tree{"realm.json": codeRealm}, tree{"realm.json": unknownKind}), "RD028")
}
