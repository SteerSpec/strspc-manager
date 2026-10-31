package rulelint

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

func TestRL000(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T) *result.Result
	}{
		{
			name: "nil entity file",
			run: func(*testing.T) *result.Result {
				return New().LintFile(nil)
			},
		},
		{
			name: "missing directory",
			run: func(t *testing.T) *result.Result {
				return New().LintDir(context.Background(), filepath.Join(t.TempDir(), "missing"))
			},
		},
		{
			name: "unreadable file",
			run: func(t *testing.T) *result.Result {
				if os.Geteuid() == 0 || runtime.GOOS == "windows" {
					t.Skip("needs file modes enforced: not on Windows or as root")
				}
				dir := t.TempDir()
				path := filepath.Join(dir, "ENT.json")
				if err := os.WriteFile(path, []byte(`{}`), 0o000); err != nil {
					t.Fatal(err)
				}
				return New().LintDir(context.Background(), dir)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.run(t)
			if res.OK() {
				t.Fatalf("expected an error, got %v", res.Diagnostics)
			}
			if got := res.Errors()[0]; got.Code != "RL000" {
				t.Errorf("first error = %s, want RL000", got)
			}
		})
	}
}
