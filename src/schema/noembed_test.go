package schema_test

import (
	"bufio"
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
// testdata fixtures) and fails on a committed *.schema.json or on a
// //go:embed directive that names a schema.

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
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		return checkEmbeds(t, path)
	})
	if err != nil {
		t.Fatalf("walking %s: %v", src, err)
	}
	if found == 0 {
		t.Fatalf("no files found under %s", src)
	}
}

// checkEmbeds fails the test for every //go:embed directive in the file
// whose pattern names a schema.
func checkEmbeds(t *testing.T, path string) error {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()

	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		m := embedRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		if strings.Contains(strings.ToLower(m[1]), "schema") {
			t.Errorf("%s:%d: //go:embed %s embeds a schema in the binary (MOD-004)", path, line, strings.TrimSpace(m[1]))
		}
	}
	return sc.Err()
}
