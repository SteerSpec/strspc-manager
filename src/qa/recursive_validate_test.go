package qa_test

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
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// TestRecursiveValidateWiring wires realm-lint and rule-lint the way
// `strspc realm validate --recursive` does: realm-lint without a rule linter
// but with entity linting delegated, rule-lint's LintRealm linting the entity
// files, and both given per-sub-realm linters. A sub-realm pinned to a newer
// core must be checked against its own pin only, and no finding may be
// reported twice.
func TestRecursiveValidateWiring(t *testing.T) {
	// Core 1.14.0 rejects every entity file; 1.15.0 accepts them. Both accept
	// every manifest.
	routes := map[string]string{
		"/rules/v1.14.0/_schema/entity.v1.schema.json": `{"not": {}}`,
		"/rules/v1.14.0/_schema/realm.v1.schema.json":  `{}`,
		"/rules/v1.15.0/_schema/entity.v1.schema.json": `{}`,
		"/rules/v1.15.0/_schema/realm.v1.schema.json":  `{}`,
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
	fetcherFor := func(dir string) *schema.Fetcher {
		opts := append([]schema.Option{
			schema.WithBaseURL(srv.URL + "/schemas"),
			schema.WithCacheDir(t.TempDir()),
		}, schema.ForRealm(dir)...)
		return schema.New(opts...)
	}
	ruleLinterFor := func(dir string) *rulelint.Linter {
		return rulelint.New(rulelint.WithSchemaFetcher(fetcherFor(dir)))
	}
	subRealmLinters := func(dir string) (*schema.Fetcher, *rulelint.Linter) {
		return fetcherFor(dir), ruleLinterFor(dir)
	}

	// The parent pins core 1.14.0 and its sub-realm `sync` pins 1.15.0.
	dir := filepath.Join(t.TempDir(), "realm")
	if err := os.CopyFS(dir, os.DirFS(filepath.Join("..", "rulelint", "testdata", "realm_with_sub"))); err != nil {
		t.Fatal(err)
	}
	pinCore(t, filepath.Join(dir, "realm.json"), "1.14.0")
	pinCore(t, filepath.Join(dir, "sync", "realm.json"), "1.15.0")

	ctx := context.Background()
	realmRes := realmlint.New(
		realmlint.WithSchemaFetcher(fetcherFor(dir)),
		realmlint.WithEntityLintingDelegated(),
		realmlint.WithSubRealmLinters(subRealmLinters),
	).Lint(ctx, dir)
	ruleRes := rulelint.New(
		rulelint.WithSchemaFetcher(fetcherFor(dir)),
		rulelint.WithSubRealmLinters(ruleLinterFor),
	).LintRealm(ctx, dir)
	all := append(append([]result.Diagnostic{}, realmRes.Diagnostics...), ruleRes.Diagnostics...)

	rl002 := make(map[string]int)
	seen := make(map[result.Diagnostic]bool)
	for _, d := range all {
		if seen[d] {
			t.Errorf("reported twice: %s %s %s: %s", d.Module, d.Code, d.Path, d.Message)
		}
		seen[d] = true
		if d.Code == "RL002" {
			file, _, _ := strings.Cut(d.Path, ": ")
			rl002[filepath.Base(file)]++
		}
		if d.Code == "RM005" && d.Severity == result.Info {
			t.Errorf("info RM005 despite delegation: %s", d.Message)
		}
	}
	if rl002["PAR.json"] != 1 {
		t.Errorf("PAR.json: %d RL002, want exactly 1 (from the parent's pin)", rl002["PAR.json"])
	}
	if rl002["SYN.json"] != 0 {
		t.Errorf("SYN.json: %d RL002, want 0 (its own pin accepts it)", rl002["SYN.json"])
	}
}

// pinCore makes the manifest at path depend on core at version.
func pinCore(t *testing.T, path, version string) {
	t.Helper()
	rf, err := entity.LoadRealm(path)
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
