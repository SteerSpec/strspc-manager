package ruleresolve

import (
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// checkCollisions detects duplicate entity IDs across resolved files
// from different sources.
func checkCollisions(files []*ResolvedFile, res *result.Result) {
	// EUID → source URI of the first provider.
	euids := make(map[string]string)

	var walk func(f *entity.File, origin SourceEntry)
	walk = func(f *entity.File, origin SourceEntry) {
		id := f.Entity.ID
		if id == "" {
			return
		}
		if prev, ok := euids[id]; ok && prev != origin.Source {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV005",
				Severity: result.Error,
				Message:  fmt.Sprintf("EUID %q provided by both %s and %s", id, prev, origin.Source),
			})
		} else if !ok {
			euids[id] = origin.Source
		}

		for i := range f.SubEntities {
			walk(&f.SubEntities[i], origin)
		}
	}

	for _, rf := range files {
		walk(rf.File, rf.Origin)
	}
}
