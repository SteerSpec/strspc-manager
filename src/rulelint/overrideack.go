package rulelint

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkOverrideAcks runs RL044: an `override_ack` note is only on a rule that
// declares `overrides` (NTE-043). A note whose rule_ref names no rule is RL008's.
func checkOverrideAcks(ef *entity.File, res *result.Result, path string) {
	overriding := make(map[string]bool, len(ef.Rules))
	for _, r := range ef.Rules {
		overriding[r.ID] = r.Overrides != nil && *r.Overrides != ""
	}
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeOverrideAck {
			continue
		}
		if has, known := overriding[n.RuleRef]; known && !has {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL044",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: an override_ack note is on rule %s, which declares no overrides (NTE-043)", n.ID, n.RuleRef),
				Path:     path,
			})
		}
	}
}
