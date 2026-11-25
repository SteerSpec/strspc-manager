package rulelint_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/rulelint"
)

func TestLintDirReportsEntityWithRealmKey(t *testing.T) {
	dir := t.TempDir()
	body := `{"realm":{},"entity":{"id":"ENT"},"rules":[]}`
	if err := os.WriteFile(filepath.Join(dir, "ENT.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, d := range rulelint.New().LintDir(context.Background(), dir).Diagnostics {
		if d.Code == "RL001" {
			return
		}
	}
	t.Fatal("LintDir reported no RL001")
}

func TestLintBytesReportsManifestAsEntity(t *testing.T) {
	for _, d := range rulelint.New().LintBytes(context.Background(), []byte(`{"realm":{}}`)).Diagnostics {
		if d.Code == "RL001" {
			return
		}
	}
	t.Fatal("LintBytes reported no RL001")
}
