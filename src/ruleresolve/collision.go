package ruleresolve

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkCollisions detects duplicate entity IDs across resolved files
// from different sources.
func checkCollisions(files []*ResolvedFile, res *result.Result) {
	// EUID → resolved source path of the first provider.
	euids := make(map[string]string)

	var walk func(f *entity.File, rf *ResolvedFile)
	walk = func(f *entity.File, rf *ResolvedFile) {
		id := f.Entity.ID
		if id == "" {
			return
		}
		if prev, ok := euids[id]; ok && prev != rf.ResolvedSource {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV005",
				Severity: result.Error,
				Message:  fmt.Sprintf("EUID %q provided by both %s and %s", id, prev, rf.ResolvedSource),
				Path:     rf.ResolvedSource,
			})
		} else if !ok {
			euids[id] = rf.ResolvedSource
		}

		for i := range f.SubEntities {
			walk(&f.SubEntities[i], rf)
		}
	}

	for _, rf := range files {
		walk(rf.File, rf)
	}
}
