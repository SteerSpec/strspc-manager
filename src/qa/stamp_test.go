package qa_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// ruleSetHashRe finds each rule_set.hash value of an entity file. Group 1 is
// the value. rule_set precedes sub_entities in a fixture, so the matches follow
// the pre-order of the entity tree.
var ruleSetHashRe = regexp.MustCompile(`"rule_set"\s*:\s*\{[^{}]*?"hash"\s*:\s*(null|"[^"]*")`)

func TestMain(m *testing.M) {
	flag.Parse()
	if *update {
		if err := stampFixtures(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// fixtureRoots returns every directory holding a scenario.json.
func fixtureRoots() ([]string, error) {
	roots := []string{cleanDir}
	entries, err := os.ReadDir(brokenDir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", brokenDir, err)
	}
	for _, e := range entries {
		if e.IsDir() {
			roots = append(roots, filepath.Join(brokenDir, e.Name()))
		}
	}
	return roots, nil
}

// entityFiles lists the entity files below root that carry a valid hash: every
// JSON file but the scenario's own, the manifests, the schemas and bad.
func entityFiles(root string, bad []string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "_schema" {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if filepath.Ext(rel) != ".json" || rel == "scenario.json" || rel == "expected.json" ||
			d.Name() == "realm.json" || slices.Contains(bad, rel) {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", root, err)
	}
	return files, nil
}

func badHashes(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, "scenario.json"))
	if err != nil {
		return nil, fmt.Errorf("reading scenario: %w", err)
	}
	var sc scenario
	if err := json.Unmarshal(data, &sc); err != nil {
		return nil, fmt.Errorf("parsing %s/scenario.json: %w", root, err)
	}
	return sc.BadHash, nil
}

// stampFixtures writes the hash of every entity file that is not listed as
// intentionally bad.
func stampFixtures() error {
	roots, err := fixtureRoots()
	if err != nil {
		return err
	}
	for _, root := range roots {
		bad, err := badHashes(root)
		if err != nil {
			return err
		}
		files, err := entityFiles(root, bad)
		if err != nil {
			return err
		}
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("reading fixture: %w", err)
			}
			stamped, err := stamp(data)
			if err != nil {
				return fmt.Errorf("%s: %w (list it in bad_hash if that is intended)", path, err)
			}
			if err := os.WriteFile(path, stamped, 0o644); err != nil {
				return fmt.Errorf("stamping fixture: %w", err)
			}
		}
	}
	return nil
}

// stamp sets rule_set.hash of the file and of each sub-entity, keeping every
// other byte. A sub-entity's hash covers the sub-entity alone. ComputeHash nulls
// the hashes it finds, so stamping is idempotent.
func stamp(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("parsing: data after the JSON value")
	}
	hashes, err := treeHashes(root)
	if err != nil {
		return nil, err
	}
	locs := ruleSetHashRe.FindAllSubmatchIndex(data, -1)
	if len(locs) != len(hashes) {
		return nil, fmt.Errorf("found %d rule_set blocks for %d entities", len(locs), len(hashes))
	}
	var out bytes.Buffer
	last := 0
	for i, loc := range locs {
		out.Write(data[last:loc[2]])
		out.WriteString(strconv.Quote(hashes[i]))
		last = loc[3]
	}
	out.Write(data[last:])
	return out.Bytes(), nil
}

// treeHashes returns the hash of obj and of its sub-entities, in pre-order.
func treeHashes(obj map[string]any) ([]string, error) {
	raw, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("encoding entity: %w", err)
	}
	hash, err := entity.ComputeHash(raw)
	if err != nil {
		return nil, fmt.Errorf("hashing: %w", err)
	}
	hashes := []string{hash}
	subs, _ := obj["sub_entities"].([]any)
	for _, s := range subs {
		sub, ok := s.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("sub_entities holds a %T", s)
		}
		below, err := treeHashes(sub)
		if err != nil {
			return nil, err
		}
		hashes = append(hashes, below...)
	}
	return hashes, nil
}

// TestFixtureHashes proves RL011, RD008 and RSV004 can only fire where a
// scenario lists the file, and that sub-entity hashes are real although no
// module reads them.
func TestFixtureHashes(t *testing.T) {
	roots, err := fixtureRoots()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range roots {
		t.Run(strings.TrimPrefix(root, "testdata/"), func(t *testing.T) {
			bad, err := badHashes(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range bad {
				data, err := os.ReadFile(filepath.Join(root, name))
				if err != nil {
					t.Errorf("bad_hash lists %s: %v", name, err)
					continue
				}
				// A file that doesn't parse may stay listed; one with a correct
				// hash would escape this test for nothing.
				if stamped, err := stamp(data); err == nil && bytes.Equal(data, stamped) {
					t.Errorf("bad_hash lists %s, whose hash is correct", name)
				}
			}
			files, err := entityFiles(root, bad)
			if err != nil {
				t.Fatal(err)
			}
			for _, path := range files {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				stamped, err := stamp(data)
				if err != nil {
					t.Errorf("%s: %v (list it in bad_hash if that is intended)", path, err)
					continue
				}
				if !bytes.Equal(data, stamped) {
					t.Errorf("%s has a stale hash: run go test ./src/qa -update", path)
				}
			}
		})
	}
}
