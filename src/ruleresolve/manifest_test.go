package ruleresolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalSourceFailsOnEntityWithRealmKey(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"AAA.json": `{"entity":{"id":"AAA"},"rules":[]}`,
		"BBB.json": `{"realm":{},"entity":{"id":"BBB"},"rules":[{"id":"BBB-001","state":"I"}]}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	_, res := (&LocalSource{}).Fetch(context.Background(), dir)
	if res.OK() {
		t.Fatal("Fetch dropped an entity file without an Error")
	}
	for _, d := range res.Diagnostics {
		if d.Code == "RSV003" && filepath.Base(d.Path) == "BBB.json" {
			return
		}
	}
	t.Fatalf("no RSV003 on BBB.json: %+v", res.Diagnostics)
}

func TestLocalSourceFailsOnEntityTurnedIntoManifest(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{"AAA.json": `{"entity":{"id":"AAA"}}`, "BBB.json": `{"realm":{}}`} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, res := (&LocalSource{}).Fetch(context.Background(), dir); res.OK() {
		t.Fatal("Fetch skipped a manifest under an entity's name without an Error")
	}
}
