package realmlint

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

func TestWithEntityLintingDelegated(t *testing.T) {
	t.Run("no info RM005, in the realm or its sub-realms", func(t *testing.T) {
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", nil, []string{"sub"}, nil)
		writeRealm(t, filepath.Join(dir, "sub"), "dev.example.test.sub", "0.1.0", nil, nil, nil)

		if res := New().Lint(context.Background(), dir); !hasCodeWithSeverity(res, "RM005", result.Info) {
			t.Fatalf("without the option: expected info RM005, got %v", res.Diagnostics)
		}
		res := New(WithEntityLintingDelegated()).Lint(context.Background(), dir)
		if hasCodeWithSeverity(res, "RM005", result.Info) {
			t.Errorf("unexpected info RM005: %v", res.Diagnostics)
		}
	})

	t.Run("a pinned sub-realm's rule linter is not used", func(t *testing.T) {
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", nil, []string{"sub"}, nil)
		sub := filepath.Join(dir, "sub")
		const subCore = "1.15.0"
		writeRealm(t, sub, "dev.example.test.sub", "0.1.0",
			// Built from a variable: the strspc-rules pin job fails on a literal
			// core pin outside its file list. The test never fetches the pin.
			coreDep(subCore, "github://SteerSpec/strspc-rules@v"+subCore+"/rules/core"), nil, nil)
		entityFile, err := os.ReadFile(filepath.Join("..", "rulelint", "testdata", "valid", "basic.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sub, "TST.json"), entityFile, 0o644); err != nil {
			t.Fatal(err)
		}
		// Without a schema fetcher this rule linter reports RL002 on every file
		// it lints, so any RL code shows it ran.
		called := false
		linters := func(string) (*schema.Fetcher, *rulelint.Linter) {
			called = true
			return nil, rulelint.New()
		}

		res := New(WithEntityLintingDelegated(), WithSubRealmLinters(linters)).Lint(context.Background(), dir)
		if !called {
			t.Fatal("the sub-realm linters were not consulted")
		}
		for _, d := range res.Diagnostics {
			if strings.HasPrefix(d.Code, "RL") {
				t.Errorf("entity file linted despite delegation: %s %s: %s", d.Code, d.Path, d.Message)
			}
		}

		// Without delegation the same setup does lint it, so the check above
		// can fail.
		res = New(WithSubRealmLinters(linters)).Lint(context.Background(), dir)
		if !hasCode(res, "RL002") {
			t.Errorf("without delegation: expected RL002 from the sub-realm's rule linter, got %v", res.Diagnostics)
		}
	})

	t.Run("RM005 errors are still reported", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a directory the test user cannot read")
		}
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", nil, nil, nil)
		locked := filepath.Join(dir, "locked")
		if err := os.Mkdir(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

		res := New(WithEntityLintingDelegated()).Lint(context.Background(), dir)
		if !hasCodeWithSeverity(res, "RM005", result.Error) {
			t.Errorf("expected error RM005 for the unreadable directory, got %v", res.Diagnostics)
		}
		if hasCodeWithSeverity(res, "RM005", result.Info) {
			t.Errorf("unexpected info RM005: %v", res.Diagnostics)
		}
	})
}
