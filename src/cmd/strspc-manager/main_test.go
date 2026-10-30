package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
