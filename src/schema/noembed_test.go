package schema_test

import (
	"bufio"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/schema"
)

// MOD-004: JSON schemas are fetched from steerspec.dev through this package
// and are never embedded in the binary. The test walks src/ (skipping
// testdata fixtures) and enforces an explicit allowlist: every file that any
// //go:embed directive resolves to must be listed in allowedEmbeds, and every
// JSON file committed under src/ outside testdata must be listed too. A file
// that is not listed fails regardless of its name or content, so a schema
// saved as policy.json or pulled in through a wildcard cannot slip through.
// Listed files are additionally checked by content and by name so that an
// allowlisted file cannot later be turned into a schema copy.

// allowedEmbeds maps each permitted embedded or committed non-Go data file,
// as a slash-separated path relative to src/, to the reason it is allowed.
// Adding an entry is a reviewed decision: the file must not be a JSON Schema.
var allowedEmbeds = map[string]string{
	"render/templates/default.md.tmpl": "default Markdown rendering template for the render package",
}

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
		rel := relToSrc(t, src, path)
		if strings.HasSuffix(d.Name(), ".json") {
			checkAllowed(t, src, rel, "committed under src/ outside testdata")
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			return checkEmbeds(t, src, path)
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
// that names a schema, or resolves to a file missing from allowedEmbeds or
// failing the content check.
func checkEmbeds(t *testing.T, src, path string) error {
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
		files := resolveEmbed(t, dir, directive)
		if len(files) == 0 {
			t.Errorf("%s:%d: //go:embed %s matches no file", path, line, directive)
		}
		for _, file := range files {
			checkAllowed(t, src, relToSrc(t, src, file), "embedded by "+path+":"+strconv.Itoa(line))
		}
	}
	return sc.Err()
}

// checkAllowed fails the test unless rel (relative to src/) is listed in
// allowedEmbeds, and additionally fails when a listed file is or looks like
// a schema.
func checkAllowed(t *testing.T, src, rel, how string) {
	t.Helper()
	if _, ok := allowedEmbeds[rel]; !ok {
		t.Errorf("src/%s is %s but is not in allowedEmbeds; schemas are fetched through the schema package, and any other data file needs a reviewed allowlist entry (MOD-004)", rel, how)
		return
	}
	if strings.HasSuffix(rel, ".schema.json") || strings.Contains(strings.ToLower(filepath.Base(rel)), "schema") {
		t.Errorf("src/%s is allowlisted but named like a schema (MOD-004)", rel)
	}
	if isJSONSchemaFile(t, filepath.Join(src, filepath.FromSlash(rel))) {
		t.Errorf("src/%s is allowlisted but its content is a JSON Schema (MOD-004)", rel)
	}
}

// relToSrc returns path relative to src/ with forward slashes.
func relToSrc(t *testing.T, src, path string) string {
	t.Helper()
	rel, err := filepath.Rel(src, path)
	if err != nil {
		t.Fatalf("relativising %s: %v", path, err)
	}
	return filepath.ToSlash(rel)
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

// isJSONSchemaFile is the extra guard applied to allowlisted files: it
// reports whether the file parses as a JSON object that looks like a JSON
// Schema document ("$schema" pointing at json-schema.org, an "$id" naming a
// schema, or "type" together with "properties", "$defs" or "definitions").
// It is deliberately not the primary check, since valid schemas can also be
// bare booleans, empty objects or objects using only keywords such as $ref;
// those are excluded by the allowlist itself.
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
