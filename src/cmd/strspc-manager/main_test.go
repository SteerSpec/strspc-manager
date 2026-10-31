package main

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/SteerSpec/strspc-manager/src/internal/version"
	"github.com/SteerSpec/strspc-manager/src/result"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestVersion_PrintsBuildMetadata(t *testing.T) {
	var out strings.Builder
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"version"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("version: %v", err)
	}
	for _, want := range []string{"strspc-manager", version.Version, version.Commit, version.Date} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("version output lacks %q:\n%s", want, out.String())
		}
	}
}

func TestRealmLint_RequiresADirectory(t *testing.T) {
	cmd := newRootCmd()
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"realm-lint"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error without a directory argument")
	}
}

func TestRealmLint_AcceptsAValidRealm(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"realm": {"id": "dev.steerspec.core", "title": "Core", "version": "1.0.0"}, "dependencies": []}`
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Accepts everything, and resolving every schema locally keeps the test
	// off the network.
	for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json", "bootstrap.schema.json"} {
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out strings.Builder
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"realm-lint", "--core", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("realm-lint: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Realm is valid") {
		t.Errorf("expected the success message, got:\n%s", out.String())
	}
}

func TestPrintDiagnostics(t *testing.T) {
	tests := []struct {
		name string
		diag result.Diagnostic
		want string
	}{
		{
			name: "error with path",
			diag: result.Diagnostic{Code: "RL001", Severity: result.Error, Message: "bad JSON", Path: "ENT.json"},
			want: "[error] RL001 bad JSON (ENT.json)\n",
		},
		{
			name: "warning without path",
			diag: result.Diagnostic{Code: "RL012", Severity: result.Warning, Message: "not found"},
			want: "[warning] RL012 not found\n",
		},
		{
			name: "info without path",
			diag: result.Diagnostic{Code: "RM002", Severity: result.Info, Message: "skipped"},
			want: "[info] RM002 skipped\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out strings.Builder
			cmd := &cobra.Command{}
			cmd.SetOut(&out)
			printDiagnostics(cmd, &result.Result{Diagnostics: []result.Diagnostic{tt.diag}})
			// On a terminal, lipgloss wraps the prefix and code in colours.
			if got := ansiRe.ReplaceAllString(out.String(), ""); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRealmLint_CoreFlagUsesOwnSchemas(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"realm": {"id": "dev.steerspec.core", "title": "Core", "version": "1.0.0"}, "dependencies": []}`
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Rejects everything, so RM002 can only come from this copy, and resolving
	// every schema locally keeps the test off the network.
	for _, name := range []string{"realm.v1.schema.json", "entity.v1.schema.json", "bootstrap.schema.json"} {
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), []byte(`{"not": {}}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out strings.Builder
	cmd := newRootCmd()
	cmd.SetOut(&out)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"realm-lint", "--core", dir})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected a validation failure, got:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "RM002") {
		t.Errorf("expected RM002, got:\n%s", out.String())
	}
}
