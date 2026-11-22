package rulediff_test

import (
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
	if res := rulediff.Compare([]byte(plainEntity), []byte(`{"realm":{},"rules":[{"file":"ENT.json"}]}`)); !hasCode(res, "RD005") {
		t.Errorf("no RD005: %+v", res.Diagnostics)
	}
}
