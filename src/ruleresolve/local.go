package ruleresolve

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// LocalSource implements Source for local filesystem paths.
type LocalSource struct{}

// Fetch walks a local directory recursively and returns all entity files found.
func (s *LocalSource) Fetch(ctx context.Context, ref string) ([]SourceFile, *result.Result) {
	res := &result.Result{}

	abs, err := filepath.Abs(ref)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV002",
			Severity: result.Error,
			Message:  fmt.Sprintf("resolving path %q: %s", ref, err),
			Path:     ref,
		})
		return nil, res
	}

	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		msg := fmt.Sprintf("source path is not a directory: %s", abs)
		if err != nil {
			msg = fmt.Sprintf("source path: %s", err)
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV002",
			Severity: result.Error,
			Message:  msg,
			Path:     abs,
		})
		return nil, res
	}

	var files []SourceFile

	walkErr := entity.WalkEntityFiles(abs, func(path string, data []byte, ef *entity.File, parseErr error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if parseErr != nil {
			if data == nil {
				// Read error.
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RSV002",
					Severity: result.Error,
					Message:  fmt.Sprintf("reading file: %s", parseErr),
					Path:     path,
				})
			} else {
				// Parse error.
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RSV003",
					Severity: result.Error,
					Message:  fmt.Sprintf("parsing entity: %s", parseErr),
					Path:     path,
				})
			}
			return nil
		}

		if ef.Entity.ID == "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV003",
				Severity: result.Error,
				Message:  "entity.id is empty",
				Path:     path,
			})
			return nil
		}

		// Verify hash if present.
		if ef.RuleSet.Hash != nil && *ef.RuleSet.Hash != "" {
			computed, hashErr := entity.ComputeHash(data)
			if hashErr != nil {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RSV004",
					Severity: result.Error,
					Message:  fmt.Sprintf("computing hash: %s", hashErr),
					Path:     path,
				})
				return nil
			}
			if computed != *ef.RuleSet.Hash {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RSV004",
					Severity: result.Error,
					Message:  fmt.Sprintf("hash mismatch: expected %s, got %s", *ef.RuleSet.Hash, computed),
					Path:     path,
				})
				return nil
			}
		}

		files = append(files, SourceFile{File: ef, Path: path})
		return nil
	})

	if walkErr != nil {
		code := "RSV002"
		if errors.Is(walkErr, context.Canceled) || errors.Is(walkErr, context.DeadlineExceeded) {
			code = "RSV000"
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     code,
			Severity: result.Error,
			Message:  fmt.Sprintf("walking directory: %s", walkErr),
			Path:     abs,
		})
	}

	if len(files) == 0 && res.OK() {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV006",
			Severity: result.Warning,
			Message:  fmt.Sprintf("no entity files found in %s", abs),
			Path:     abs,
		})
	}

	return files, res
}
