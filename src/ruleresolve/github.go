package ruleresolve

import (
	"context"
	"fmt"

	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// GitHubSource implements Source for github://owner/repo@ref/path URIs.
// It materialises the referenced directory in the local cache through the
// Fetcher and then delegates to LocalSource, so parse errors (RSV003) and
// hash verification (RSV004) behave exactly as for local directories.
type GitHubSource struct {
	Fetcher *remote.Fetcher
}

// Fetch downloads (or reuses the cached copy of) the referenced directory
// and returns the entity files it contains. Download failures are reported
// as RSV007.
func (s *GitHubSource) Fetch(ctx context.Context, ref string) ([]SourceFile, *result.Result) {
	res := &result.Result{}

	parsed, err := remote.ParseGitHub(ref)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV007",
			Severity: result.Error,
			Message:  err.Error(),
			Path:     ref,
		})
		return nil, res
	}

	if s.Fetcher == nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV007",
			Severity: result.Error,
			Message:  "github source has no fetcher configured",
			Path:     parsed.String(),
		})
		return nil, res
	}

	dir, err := s.Fetcher.EnsureDir(ctx, parsed)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV007",
			Severity: result.Error,
			Message:  fmt.Sprintf("fetching %s: %s", parsed.String(), err),
			Path:     parsed.String(),
		})
		return nil, res
	}

	return (&LocalSource{}).Fetch(ctx, dir)
}
