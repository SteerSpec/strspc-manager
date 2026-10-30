package realmlint

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// coreSource uses a version no release will hold: the pin PR's pin_core.py
// fails on a live core pin in a file it does not rewrite.
const coreSource = "github://SteerSpec/strspc-rules@v9.9.9/rules/core"

// writeRealm writes a realm.json and a _schema/ holding schemas (file name →
// content) to dir.
func writeRealm(t *testing.T, dir, id, version string, deps []entity.RealmDep, subRealms []string, schemas map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "_schema"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"realm":        map[string]string{"id": id, "title": "Test", "version": version},
		"dependencies": deps,
		"sub_realms":   subRealms,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	for name, content := range schemas {
		if err := os.WriteFile(filepath.Join(dir, "_schema", name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// fetchFrom returns a DependencyFetcher resolving every dependency to dir.
func fetchFrom(dir string) DependencyFetcher {
	return func(context.Context, string, entity.RealmDep) (string, error) {
		return dir, nil
	}
}

func coreDep(version, source string) []entity.RealmDep {
	return []entity.RealmDep{{RealmID: coreRealmID, Version: version, Source: source}}
}

func TestRM014_SourceTag(t *testing.T) {
	tests := []struct {
		name    string
		version string
		source  string
		want    bool
	}{
		{"match", "9.9.9", coreSource, false},
		{"mismatch", "1.8.0", coreSource, true},
		{"tag without v", "9.9.9", "github://SteerSpec/strspc-rules@9.9.9/rules/core", false},
		{"commit sha", "9.9.9", "github://SteerSpec/strspc-rules@7a8c604/rules/core", false},
		{"latest", "9.9.9", "github://SteerSpec/strspc-rules@latest/rules/core", false},
		{"relative path", "9.9.9", "../core", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep(tt.version, tt.source), nil, nil)
			res := New().Lint(context.Background(), dir)
			if got := hasCodeWithSeverity(res, "RM014", result.Error); got != tt.want {
				t.Errorf("RM014 = %v, want %v: %v", got, tt.want, res.Diagnostics)
			}
		})
	}
}

func TestRM014_DependencyManifest(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		version string
		want    bool
	}{
		{"match", coreRealmID, "9.9.9", false},
		{"other version", coreRealmID, "1.10.0", true},
		{"other realm", "dev.example.other", "9.9.9", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, core := t.TempDir(), t.TempDir()
			writeRealm(t, core, tt.id, tt.version, nil, nil, nil)
			writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil, nil)
			res := New(WithDependencyFetcher(fetchFrom(core))).Lint(context.Background(), dir)
			if got := hasCodeWithSeverity(res, "RM014", result.Error); got != tt.want {
				t.Errorf("RM014 = %v, want %v: %v", got, tt.want, res.Diagnostics)
			}
		})
	}
}

func TestRM015_SchemaDrift(t *testing.T) {
	coreSchemas := map[string]string{"entity.v1.schema.json": `{"a": 1, "b": 2}`, "config.v1.schema.json": `{}`}
	tests := []struct {
		name    string
		schemas map[string]string
		want    bool
	}{
		{"same", map[string]string{"entity.v1.schema.json": `{"a": 1, "b": 2}`}, false},
		{"same value, other formatting", map[string]string{"entity.v1.schema.json": "{\n  \"b\": 2,\n  \"a\": 1\n}\n"}, false},
		{"different", map[string]string{"entity.v1.schema.json": `{"a": 1}`}, true},
		{"file only in the realm", map[string]string{"entity.v1.schema.json": `{"a": 1, "b": 2}`, "local.schema.json": `{}`}, false},
		{"invalid JSON", map[string]string{"entity.v1.schema.json": `{`}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir, core := t.TempDir(), t.TempDir()
			writeRealm(t, core, coreRealmID, "9.9.9", nil, nil, coreSchemas)
			writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil, tt.schemas)
			res := New(WithDependencyFetcher(fetchFrom(core))).Lint(context.Background(), dir)
			if got := hasCodeWithSeverity(res, "RM015", result.Warning); got != tt.want {
				t.Errorf("RM015 = %v, want %v: %v", got, tt.want, res.Diagnostics)
			}
		})
	}
}

func TestRM015_SkipsMismatchedCore(t *testing.T) {
	for _, tt := range []struct{ name, id, version string }{
		{"other version", coreRealmID, "1.10.0"},
		{"other realm", "dev.example.other", "9.9.9"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir, core := t.TempDir(), t.TempDir()
			writeRealm(t, core, tt.id, tt.version, nil, nil, map[string]string{"entity.v1.schema.json": `{"a": 1}`})
			writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil,
				map[string]string{"entity.v1.schema.json": `{"a": 2}`})
			res := New(WithDependencyFetcher(fetchFrom(core))).Lint(context.Background(), dir)
			assertHasCode(t, res, "RM014")
			if hasCode(res, "RM015") {
				t.Errorf("unexpected RM015: %v", res.Diagnostics)
			}
		})
	}
}

func TestRM015_UnreadableCoreSchema(t *testing.T) {
	dir, core := t.TempDir(), t.TempDir()
	writeRealm(t, core, coreRealmID, "9.9.9", nil, nil, nil)
	// A directory makes the core's copy exist but fail to read, on any platform.
	if err := os.Mkdir(filepath.Join(core, "_schema", "entity.v1.schema.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil,
		map[string]string{"entity.v1.schema.json": `{"a": 1}`})
	res := New(WithDependencyFetcher(fetchFrom(core))).Lint(context.Background(), dir)
	if !hasCodeWithSeverity(res, "RM015", result.Warning) {
		t.Errorf("expected RM015 warning, got: %v", res.Diagnostics)
	}
}

func TestRM015_OnlyComparesCore(t *testing.T) {
	dir, dep := t.TempDir(), t.TempDir()
	writeRealm(t, dep, "dev.example.other", "1.0.0", nil, nil, map[string]string{"entity.v1.schema.json": `{"a": 1}`})
	writeRealm(t, dir, "dev.example.test", "0.1.0",
		[]entity.RealmDep{{RealmID: "dev.example.other", Version: "1.0.0", Source: "../other"}},
		nil, map[string]string{"entity.v1.schema.json": `{"b": 2}`})
	res := New(WithDependencyFetcher(fetchFrom(dep))).Lint(context.Background(), dir)
	if hasCode(res, "RM015") {
		t.Errorf("unexpected RM015: %v", res.Diagnostics)
	}
}

func TestRM015_SubRealm(t *testing.T) {
	coreSchemas := map[string]string{"entity.v1.schema.json": `{"a": 1}`}
	drifted := map[string]string{"entity.v1.schema.json": `{"a": 2}`}

	t.Run("inherits the parent's core", func(t *testing.T) {
		dir, core := t.TempDir(), t.TempDir()
		writeRealm(t, core, coreRealmID, "9.9.9", nil, nil, coreSchemas)
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), []string{"sub"}, coreSchemas)
		writeRealm(t, filepath.Join(dir, "sub"), "dev.example.test.sub", "0.1.0", nil, nil, drifted)
		res := New(WithDependencyFetcher(fetchFrom(core))).Lint(context.Background(), dir)
		assertDiagPath(t, res, "RM015", filepath.Join(dir, "sub", "_schema", "entity.v1.schema.json"))
	})

	t.Run("pins its own core", func(t *testing.T) {
		dir, core, subCore := t.TempDir(), t.TempDir(), t.TempDir()
		writeRealm(t, core, coreRealmID, "9.9.9", nil, nil, coreSchemas)
		writeRealm(t, subCore, coreRealmID, "1.8.0", nil, nil, drifted)
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), []string{"sub"}, coreSchemas)
		writeRealm(t, filepath.Join(dir, "sub"), "dev.example.test.sub", "0.1.0",
			coreDep("1.8.0", "github://SteerSpec/strspc-rules@v1.8.0/rules/core"), nil, drifted)
		fetch := func(_ context.Context, _ string, dep entity.RealmDep) (string, error) {
			if dep.Version == "1.8.0" {
				return subCore, nil
			}
			return core, nil
		}
		res := New(WithDependencyFetcher(fetch)).Lint(context.Background(), dir)
		if hasCode(res, "RM015") {
			t.Errorf("unexpected RM015: %v", res.Diagnostics)
		}
	})

	t.Run("does not fall back when its own core is unavailable", func(t *testing.T) {
		dir, core := t.TempDir(), t.TempDir()
		writeRealm(t, core, coreRealmID, "9.9.9", nil, nil, coreSchemas)
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), []string{"sub"}, coreSchemas)
		writeRealm(t, filepath.Join(dir, "sub"), "dev.example.test.sub", "0.1.0",
			coreDep("1.8.0", "github://SteerSpec/strspc-rules@v1.8.0/rules/core"), nil, drifted)
		fetch := func(_ context.Context, _ string, dep entity.RealmDep) (string, error) {
			if dep.Version == "1.8.0" {
				return "", errors.New("network down")
			}
			return core, nil
		}
		res := New(WithDependencyFetcher(fetch)).Lint(context.Background(), dir)
		assertHasCode(t, res, "RM016")
		if hasCode(res, "RM015") {
			t.Errorf("unexpected RM015: %v", res.Diagnostics)
		}
	})
}

func TestSubRealmLinters(t *testing.T) {
	// Core 1.10.0 rejects everything, so only its schemas can report RM002/RL002.
	routes := map[string]string{
		"/rules/v9.9.9/_schema/realm.v1.schema.json":   `{}`,
		"/rules/v9.9.9/_schema/entity.v1.schema.json":  `{}`,
		"/rules/v1.10.0/_schema/realm.v1.schema.json":  `{"not": {}}`,
		"/rules/v1.10.0/_schema/entity.v1.schema.json": `{"not": {}}`,
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

	linters := func(dir string) (*schema.Fetcher, *rulelint.Linter) {
		opts := append([]schema.Option{
			schema.WithBaseURL(srv.URL + "/schemas"),
			schema.WithCacheDir(t.TempDir()),
		}, schema.ForRealm(dir)...)
		fetcher := schema.New(opts...)
		return fetcher, rulelint.New(rulelint.WithSchemaFetcher(fetcher))
	}
	lint := func(dir string) *result.Result {
		fetcher, rl := linters(dir)
		return New(WithSchemaFetcher(fetcher), WithRuleLinter(rl), WithSubRealmLinters(linters)).Lint(context.Background(), dir)
	}
	entityFile, err := os.ReadFile(filepath.Join("..", "rulelint", "testdata", "valid", "basic.json"))
	if err != nil {
		t.Fatal(err)
	}
	writeSub := func(t *testing.T, deps []entity.RealmDep) string {
		t.Helper()
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), []string{"sub"}, nil)
		sub := filepath.Join(dir, "sub")
		writeRealm(t, sub, "dev.example.test.sub", "0.1.0", deps, nil, nil)
		if err := os.WriteFile(filepath.Join(sub, "TST.json"), entityFile, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}

	t.Run("a sub-realm with its own pin uses it", func(t *testing.T) {
		dir := writeSub(t, coreDep("1.10.0", "github://SteerSpec/strspc-rules@v1.10.0/rules/core"))
		res := lint(dir)
		assertDiagPath(t, res, "RM002", filepath.Join(dir, "sub", "realm.json"))
		assertHasCode(t, res, "RL002")
	})

	t.Run("a sub-realm without a pin inherits its parent's", func(t *testing.T) {
		res := lint(writeSub(t, nil))
		if hasCode(res, "RM002") || hasCode(res, "RL002") {
			t.Errorf("unexpected schema failure: %v", res.Diagnostics)
		}
	})
}

func TestRM016_DependencyUnavailable(t *testing.T) {
	t.Run("fetch fails", func(t *testing.T) {
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("1.8.0", coreSource), nil, nil)
		fetch := func(context.Context, string, entity.RealmDep) (string, error) {
			return "", errors.New("network down")
		}
		res := New(WithDependencyFetcher(fetch)).Lint(context.Background(), dir)
		if !hasCodeWithSeverity(res, "RM016", result.Warning) {
			t.Errorf("expected RM016 warning, got: %v", res.Diagnostics)
		}
		// The offline half of RM014 still runs.
		assertHasCode(t, res, "RM014")
	})

	t.Run("no manifest", func(t *testing.T) {
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil, nil)
		res := New(WithDependencyFetcher(fetchFrom(t.TempDir()))).Lint(context.Background(), dir)
		assertHasCode(t, res, "RM016")
	})

	t.Run("strict is error", func(t *testing.T) {
		dir := t.TempDir()
		writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil, nil)
		res := New(WithStrict(true), WithDependencyFetcher(fetchFrom(t.TempDir()))).Lint(context.Background(), dir)
		if !hasCodeWithSeverity(res, "RM016", result.Error) {
			t.Errorf("expected RM016 error, got: %v", res.Diagnostics)
		}
	})
}

func TestDependencyChecks_NoFetcher(t *testing.T) {
	dir := t.TempDir()
	writeRealm(t, dir, "dev.example.test", "0.1.0", coreDep("9.9.9", coreSource), nil, nil)
	res := New().Lint(context.Background(), dir)
	for _, code := range []string{"RM014", "RM015", "RM016"} {
		if hasCode(res, code) {
			t.Errorf("unexpected %s without a fetcher: %v", code, res.Diagnostics)
		}
	}
}

// assertDiagPath fails the test unless the only diagnostic with code has path.
func assertDiagPath(t *testing.T, res *result.Result, code, path string) {
	t.Helper()
	var paths []string
	for _, d := range res.Diagnostics {
		if d.Code == code {
			paths = append(paths, d.Path)
		}
	}
	if len(paths) != 1 || paths[0] != path {
		t.Errorf("%s paths = %v, want [%s]", code, paths, path)
	}
}
