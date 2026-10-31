package qa_test

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// strictModules are the modules that promote warnings under WithStrict.
var strictModules = []string{"rule-lint", "rule-diff", "realm-lint", "rule-refs"}

// TestBrokenScenarios compares the exact diagnostics of each scenario with its
// expected.json. Under -update it rewrites the file, but the hand-written claims
// still have to hold.
func TestBrokenScenarios(t *testing.T) {
	for _, name := range brokenScenarios(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(brokenDir, name)
			sc := loadScenario(t, dir)
			if len(sc.Claims) == 0 {
				t.Fatal("scenario.json claims no code: say what the scenario is for")
			}
			got := runFixture(t, dir, sc, false)
			if *update {
				if err := writeExpected(dir, got); err != nil {
					t.Fatal(err)
				}
			}
			want, err := readExpected(dir)
			if errors.Is(err, fs.ErrNotExist) {
				t.Fatal(errNoExpected)
			}
			if err != nil {
				t.Fatal(err)
			}
			assertFindings(t, want, got)
			for _, f := range got {
				if filepath.IsAbs(f.Path) {
					t.Errorf("%s has a machine-dependent path", f)
				}
			}
			codes := make(map[string]bool)
			for _, f := range got {
				codes[f.Code] = true
			}
			for _, code := range sc.Claims {
				if !codes[code] {
					t.Errorf("the scenario claims %s, which did not fire", code)
				}
			}
		})
	}
}

// TestStrictPromotesWarnings reruns every scenario in strict mode: a warning of
// a module with a strict mode becomes an error, and nothing else moves.
func TestStrictPromotesWarnings(t *testing.T) {
	for _, name := range brokenScenarios(t) {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(brokenDir, name)
			sc := loadScenario(t, dir)
			want, err := readExpected(dir)
			if err != nil {
				t.Fatal(err)
			}
			for i, f := range want {
				if f.Severity == "warning" && slices.Contains(strictModules, f.Module) {
					want[i].Severity = "error"
				}
			}
			slices.SortFunc(want, compareFindings)
			assertFindings(t, want, runFixture(t, dir, sc, true))
		})
	}
}

// TestCleanBaseline proves testdata/clean draws no error and no warning from any
// module, in either mode.
func TestCleanBaseline(t *testing.T) {
	sc := loadScenario(t, cleanDir)
	for _, tt := range []struct {
		name   string
		strict bool
	}{
		{name: "default", strict: false},
		{name: "strict", strict: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, f := range runFixture(t, cleanDir, sc, tt.strict) {
				if f.Severity != "info" {
					t.Errorf("clean fixture drew %s", f)
				}
			}
		})
	}
}

// TestCleanCoversTheModel keeps the baseline honest: a clean fixture that lost
// its supersession or its sub-realm would pass while proving less.
func TestCleanCoversTheModel(t *testing.T) {
	states := make(map[string]bool)
	notes := make(map[string]bool)
	var inherits, overrides, supersedes, subEntities int
	var walk func(*entity.File)
	walk = func(f *entity.File) {
		if f.Entity.Inherits != "" {
			inherits++
		}
		for _, r := range f.Rules {
			states[r.State] = true
			if r.Overrides != nil {
				overrides++
			}
			if r.Supersedes != nil {
				supersedes++
			}
		}
		for _, n := range f.Notes {
			notes[n.Type] = true
		}
		for i := range f.SubEntities {
			subEntities++
			walk(&f.SubEntities[i])
		}
	}
	realm := filepath.Join(cleanDir, "realm")
	err := entity.WalkEntityFiles(realm, func(path string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			t.Errorf("%s: %v", path, parseErr)
			return nil
		}
		walk(ef)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, s := range []string{entity.StateDraft, entity.StateAbandoned, entity.StatePublished, entity.StateImplemented, entity.StateRetired, entity.StateTerminated} {
		if !states[s] {
			t.Errorf("no rule in state %s", s)
		}
	}
	for _, n := range entity.NoteTypes() {
		if !notes[n] {
			t.Errorf("no note of type %s", n)
		}
	}
	for _, tt := range []struct {
		what string
		got  int
	}{
		{what: "inherits", got: inherits},
		{what: "overrides", got: overrides},
		{what: "supersedes", got: supersedes},
		{what: "sub_entities", got: subEntities},
	} {
		if tt.got == 0 {
			t.Errorf("no entity or rule uses %s", tt.what)
		}
	}

	rf, err := entity.LoadRealm(filepath.Join(realm, "realm.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rf.SubRealms) == 0 || len(rf.Dependencies) == 0 {
		t.Errorf("the realm needs a sub-realm and a dependency, got %d and %d", len(rf.SubRealms), len(rf.Dependencies))
	}
	for _, dir := range []string{"base", "head", "cited"} {
		if !isDir(filepath.Join(cleanDir, dir)) {
			t.Errorf("clean has no %s/, so a module has no input", dir)
		}
	}
}

// scenarioCodes returns each code of the expected.json files, with the module
// recorded for it.
func scenarioCodes(t *testing.T) map[string][]finding {
	t.Helper()
	if *update {
		t.Skip("expected.json is being rewritten")
	}
	codes := make(map[string][]finding)
	for _, name := range brokenScenarios(t) {
		want, err := readExpected(filepath.Join(brokenDir, name))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				t.Fatal(errNoExpected)
			}
			t.Fatal(err)
		}
		for _, f := range want {
			codes[f.Code] = append(codes[f.Code], f)
		}
	}
	return codes
}

func sortedKeys[V any](m map[string]V) []string {
	return slices.Sorted(maps.Keys(m))
}
