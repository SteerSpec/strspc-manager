package ruleresolve

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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

	walkErr := filepath.WalkDir(abs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}

		data, readErr := os.ReadFile(path)
		if readErr != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV003",
				Severity: result.Error,
				Message:  fmt.Sprintf("reading file: %s", readErr),
				Path:     path,
			})
			return nil
		}

		if entity.IsRealmJSON(data) {
			return nil
		}

		f, parseErr := entity.Parse(data)
		if parseErr != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RSV003",
				Severity: result.Error,
				Message:  fmt.Sprintf("parsing entity: %s", parseErr),
				Path:     path,
			})
			return nil
		}

		// Verify hash if present.
		if f.RuleSet.Hash != nil && *f.RuleSet.Hash != "" {
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
			if computed != *f.RuleSet.Hash {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RSV004",
					Severity: result.Error,
					Message:  fmt.Sprintf("hash mismatch: expected %s, got %s", *f.RuleSet.Hash, computed),
					Path:     path,
				})
				return nil
			}
		}

		files = append(files, SourceFile{File: f, Path: path})
		return nil
	})

	if walkErr != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RSV002",
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
