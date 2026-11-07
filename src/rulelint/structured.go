package rulelint

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkStructured runs RL042 and RL043 on the `structured` notes of an entity:
// the content is `<format>:<payload>` (NTE-037) and a rule holds at most one
// (NTE-040). The payload is opaque to SteerSpec (NTE-039), so nothing inside it
// is checked.
func checkStructured(ef *entity.File, res *result.Result, path string) {
	first := make(map[string]string)
	for _, n := range ef.Notes {
		if n.Type != entity.NoteTypeStructured {
			continue
		}
		if _, _, err := entity.ParseStructuredNote(n.Content); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL042",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: %s", n.ID, err),
				Path:     path,
			})
		}
		if id, ok := first[n.RuleRef]; ok {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL043",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %s: rule %s already holds the structured note %s (NTE-040)", n.ID, n.RuleRef, id),
				Path:     path,
			})
			continue
		}
		first[n.RuleRef] = n.ID
	}
}
