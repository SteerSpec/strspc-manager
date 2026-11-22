package rulehistory_test

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/rulehistory"
)

// History records what happened, also in a commit that diff now rejects.
func TestBuildReadsCommitWithRealmKey(t *testing.T) {
	after := []byte(`{"realm":{},"entity":{"id":"ENT"},"rule_set":{"version":"1.0.0"},"rules":[{"id":"ENT-001","state":"D","body":"MUST hold"}]}`)
	h := rulehistory.Build([]rulehistory.Commit{{SHA: "c1", After: after}})
	if len(h.Warnings) != 0 || len(h.Events) == 0 {
		t.Fatalf("events = %+v, warnings = %+v", h.Events, h.Warnings)
	}
}
