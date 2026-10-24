package schema_test

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// MOD-004: JSON schemas are fetched from steerspec.dev through this package
// and are never embedded in the binary. The test walks src/ (skipping
// testdata fixtures) and fails on any committed JSON file whose content is a
// JSON Schema, on any file named *.schema.json, and on any //go:embed
// directive that names a schema or resolves to a file whose content is one.
// Detection is by content, so a schema saved as policy.json or embedded
// through a wildcard is caught as well.

var embedRe = regexp.MustCompile(`^\s*//go:embed\s+(.+)$`)

func TestDefaultBaseURLIsSteerspecDev(t *testing.T) {
	u, err := url.Parse(schema.BaseURL)
	if err != nil {
		t.Fatalf("parsing BaseURL %q: %v", schema.BaseURL, err)
	}
	if u.Scheme != "https" || u.Host != "steerspec.dev" {
		t.Errorf("BaseURL = %q; schemas must be fetched from https://steerspec.dev (MOD-004)", schema.BaseURL)
	}
}

func TestNoEmbeddedSchemas(t *testing.T) {
	src := filepath.Join("..")

	var found int
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		found++
		name := d.Name()
		if strings.HasSuffix(name, ".schema.json") {
			t.Errorf("%s: schema copy committed under src/ outside testdata; fetch it through the schema package instead (MOD-004)", path)
			return nil
		}
		if strings.HasSuffix(name, ".json") && isJSONSchemaFile(t, path) {
			t.Errorf("%s: file content is a JSON Schema committed under src/ outside testdata (MOD-004)", path)
			return nil
		}
		if strings.HasSuffix(name, ".go") {
			return checkEmbeds(t, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", src, err)
	}
	if found == 0 {
		t.Fatalf("no files found under %s", src)
	}
}

// checkEmbeds fails the test for every //go:embed directive in the Go file
// whose pattern names a schema, or resolves to a file whose content is a
// JSON Schema.
func checkEmbeds(t *testing.T, path string) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	dir := filepath.Dir(path)
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		m := embedRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		directive := strings.TrimSpace(m[1])
		if strings.Contains(strings.ToLower(directive), "schema") {
			t.Errorf("%s:%d: //go:embed %s embeds a schema in the binary (MOD-004)", path, line, directive)
			continue
		}
		for _, file := range resolveEmbed(t, dir, directive) {
			if isJSONSchemaFile(t, file) {
				t.Errorf("%s:%d: //go:embed %s embeds %s, whose content is a JSON Schema (MOD-004)", path, line, directive, file)
			}
		}
	}
	return sc.Err()
}

// resolveEmbed expands the patterns of a //go:embed directive relative to
// the package directory into the regular files they cover, descending into
// embedded directories like the go tool does.
func resolveEmbed(t *testing.T, dir, directive string) []string {
	t.Helper()
	var files []string
	for _, pattern := range strings.Fields(directive) {
		pattern = strings.Trim(pattern, `"`)
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			t.Fatalf("%s: bad embed pattern %q: %v", dir, pattern, err)
		}
		for _, match := range matches {
			err := filepath.WalkDir(match, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if !d.IsDir() {
					files = append(files, p)
				}
				return nil
			})
			if err != nil {
				t.Fatalf("resolving embed %q: %v", pattern, err)
			}
		}
	}
	return files
}

// isJSONSchemaFile reports whether the file parses as a JSON object that
// looks like a JSON Schema document: a "$schema" pointing at json-schema.org,
// an "$id" naming a schema, or a "type" together with "properties", "$defs"
// or "definitions". Files that are not JSON objects are never schemas.
func isJSONSchemaFile(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return false
	}
	var meta, id string
	_ = json.Unmarshal(doc["$schema"], &meta)
	_ = json.Unmarshal(doc["$id"], &id)
	if strings.Contains(meta, "json-schema.org") || strings.Contains(strings.ToLower(id), "schema") {
		return true
	}
	if _, hasType := doc["type"]; hasType {
		for _, key := range []string{"properties", "$defs", "definitions"} {
			if _, ok := doc[key]; ok {
				return true
			}
		}
	}
	return false
}
