package realmlint_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

// The walker rejects some names whose bytes alone lint clean.
func TestLintReportsWalkerRejectionThatLintsClean(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"realm.json": `{"realm":{"id":"dev.test","title":"T","version":"1.0.0"}}`,
		"index.json": `{"$schema":"./_schema/entity.v1.schema.json","entity":{"id":"IDX","title":"Index"},"rule_set":{"version":"1.0.0","timestamp":"2026-01-01T00:00:00Z","hash":null},"rules":[],"notes":[]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	res := realmlint.New(realmlint.WithRuleLinter(rulelint.New())).Lint(context.Background(), dir)
	for _, d := range res.Diagnostics {
		if d.Code == "RM005" && d.Severity == result.Error && filepath.Base(d.Path) == "index.json" {
			return
		}
	}
	t.Fatalf("no RM005 Error on index.json: %+v", res.Diagnostics)
}
