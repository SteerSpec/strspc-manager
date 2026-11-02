package realmlint

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
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
