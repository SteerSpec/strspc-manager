package rulelint

import (
	"fmt"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkAddedBy runs RL032: the added_by of every rule and note of the entity
// must be a contributor identifier (CTRB-003). RL032 is Info while the
// contributor format is in transition (#248), so strict mode keeps it and an
// @handle or email still passes. An empty added_by is left to the schema.
func checkAddedBy(ef *entity.File, res *result.Result, path string) {
	for _, r := range ef.Rules {
		checkAddedByValue("rule "+r.ID, r.AddedBy, res, path)
	}
	for _, n := range ef.Notes {
		checkAddedByValue("note "+n.ID, n.AddedBy, res, path)
	}
}

func checkAddedByValue(what, addedBy string, res *result.Result, path string) {
	if addedBy == "" {
		return
	}
	_, _, err := entity.ParseContributorID(addedBy)
	if err == nil {
		return
	}
	msg := fmt.Sprintf("%s: added_by %q is not a contributor identifier (CTRB-003)", what, addedBy)
	if id := "github:" + strings.TrimPrefix(addedBy, "@"); strings.HasPrefix(addedBy, "@") && entity.IsContributorID(id) {
		msg += fmt.Sprintf("; write %s", id)
	} else {
		msg += ": " + err.Error()
	}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RL032",
		Severity: result.Info,
		Message:  msg,
		Path:     path,
	})
}
