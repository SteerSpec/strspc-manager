package rulelint

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// pinnedRealm copies testdata/realm_with_sub to a temp dir and pins the parent
// to core 1.14.0 and, when subPin is set, the sync sub-realm to subPin.
func pinnedRealm(t *testing.T, subPin string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "realm")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("testdata", "realm_with_sub"))); err != nil {
		t.Fatal(err)
	}
	pin := func(manifest, version string) {
		rf, err := entity.LoadRealm(manifest)
		if err != nil {
			t.Fatal(err)
		}
		rf.Dependencies = []entity.RealmDep{{
			RealmID: schema.CoreRealmID, Version: version,
			Source: "github://SteerSpec/strspc-rules@v" + version + "/rules/core",
		}}
		data, err := json.Marshal(rf)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(manifest, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	pin(filepath.Join(dir, "realm.json"), "1.14.0")
	if subPin != "" {
		pin(filepath.Join(dir, "sync", "realm.json"), subPin)
	}
	return dir
}

// schemaServer serves core 1.14.0's entity schema, which rejects every entity,
// and 1.15.0's, which accepts every entity, so RL002 shows which pin linted a
// file.
func schemaServer(t *testing.T) func(dir string) *Linter {
	t.Helper()
	routes := map[string]string{
		"/rules/v1.14.0/_schema/entity.v1.schema.json": `{"not": {}}`,
		"/rules/v1.15.0/_schema/entity.v1.schema.json": `{}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return func(dir string) *Linter {
		opts := append([]schema.Option{
			schema.WithBaseURL(srv.URL + "/schemas"),
			schema.WithCacheDir(t.TempDir()),
		}, schema.ForRealm(dir)...)
		return New(WithSchemaFetcher(schema.New(opts...)))
	}
}

// rl002Files returns the base names of the files RL002 was reported on.
func rl002Files(res *result.Result) map[string]int {
	out := make(map[string]int)
	for _, d := range res.Diagnostics {
		if d.Code == "RL002" {
			file, _, _ := strings.Cut(d.Path, ": ")
			out[filepath.Base(file)]++
		}
	}
	return out
}

func TestLintRealm_SubRealmLinters(t *testing.T) {
	linterFor := schemaServer(t)
	withSubs := func(dir string) *Linter {
		return New(WithSchemaFetcher(linterFor(dir).cfg.SchemaFetcher), WithSubRealmLinters(linterFor))
	}

	t.Run("a pinned sub-realm is linted only against its own pin", func(t *testing.T) {
		dir := pinnedRealm(t, "1.15.0")
		res := withSubs(dir).LintRealm(context.Background(), dir)
		got := rl002Files(res)
		if got["PAR.json"] == 0 {
			t.Errorf("the parent's entity was not linted against the parent's pin: %v", res.Diagnostics)
		}
		if got["SYN.json"] != 0 {
			t.Errorf("the sub-realm's entity was linted against the parent's pin: %v", res.Diagnostics)
		}
	})

	t.Run("cross-file checks still span the tree", func(t *testing.T) {
		// SYN-001 supersedes PAR-001 across the realm boundary.
		dir := pinnedRealm(t, "1.15.0")
		res := withSubs(dir).LintRealm(context.Background(), dir)
		for _, d := range res.Diagnostics {
			if d.Code == "RL012" {
				t.Errorf("unexpected RL012: %s", d.Message)
			}
		}
	})

	t.Run("an unpinned sub-realm keeps the parent's schemas", func(t *testing.T) {
		dir := pinnedRealm(t, "")
		res := withSubs(dir).LintRealm(context.Background(), dir)
		if got := rl002Files(res); got["SYN.json"] == 0 {
			t.Errorf("the unpinned sub-realm skipped the parent's pin: %v", res.Diagnostics)
		}
	})

	t.Run("a nil linter keeps the parent's", func(t *testing.T) {
		dir := pinnedRealm(t, "1.15.0")
		root := New(WithSchemaFetcher(linterFor(dir).cfg.SchemaFetcher), WithSubRealmLinters(func(string) *Linter { return nil }))
		if got := rl002Files(root.LintRealm(context.Background(), dir)); got["SYN.json"] == 0 {
			t.Error("the sub-realm skipped the parent's pin")
		}
	})

	t.Run("an unreadable sub-realm manifest keeps the parent's", func(t *testing.T) {
		dir := pinnedRealm(t, "1.15.0")
		if err := os.WriteFile(filepath.Join(dir, "sync", "realm.json"), []byte("{"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := rl002Files(withSubs(dir).LintRealm(context.Background(), dir)); got["SYN.json"] == 0 {
			t.Error("the sub-realm skipped the parent's pin")
		}
	})

	t.Run("without the option nothing changes", func(t *testing.T) {
		dir := pinnedRealm(t, "1.15.0")
		res := linterFor(dir).LintRealm(context.Background(), dir)
		if got := rl002Files(res); got["SYN.json"] == 0 {
			t.Errorf("the sub-realm was not linted against the parent's pin: %v", res.Diagnostics)
		}
	})
}
