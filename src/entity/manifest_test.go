package entity_test

import (
	"path/filepath"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestEntityContentBesideRealmKeyIsAnError(t *testing.T) {
	tests := []struct{ name, body string }{
		{"entity", `{"realm":{},"entity":{"id":"ENT"}}`},
		{"null entity", `{"realm":{},"entity":null}`},
		{"rule set", `{"realm":{},"rule_set":{"version":"1.0.0"}}`},
		{"sub-entities", `{"realm":{},"sub_entities":[]}`},
		{"rule with an id", `{"realm":{},"rules":[{"id":"ENT-001"}]}`},
		{"rule with a state", `{"realm":{},"rules":[{"state":"I"}]}`},
		{"rule with a body", `{"realm":{},"rules":[{"body":"MUST hold"}]}`},
		{"rule after an index entry", `{"realm":{},"rules":[{"file":"A.json"},{"id":"ENT-001"}]}`},
		{"rules not a list", `{"realm":{},"rules":{}}`},
		{"keys in another case", `{"Realm":{},"ENTITY":{"id":"ENT"}}`},
		{"scalar realm", `{"realm":false,"entity":{"id":"ENT"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := entity.Parse([]byte(tt.body)); err == nil {
				t.Error("Parse accepted entity content beside a realm key")
			}
			for _, mode := range walkModes {
				dir := t.TempDir()
				writeWalkFile(t, dir, "ENT.json", tt.body)
				var calls, failed int
				err := entity.WalkEntityFiles(dir, func(_ string, data []byte, ef *entity.File, parseErr error) error {
					calls++
					if parseErr != nil && data != nil && ef == nil {
						failed++
					}
					return nil
				}, entity.WithRecursive(mode.recursive))
				if err != nil || calls != 1 || failed != 1 {
					t.Errorf("%s walk: err = %v, calls = %d, parse failures = %d; want one parse failure", mode.name, err, calls, failed)
				}
			}
		})
	}
}

func TestOnlyAPublishedIndexIsSkippedByContent(t *testing.T) {
	const index = `{"version":"1.0.0","realm":{"id":"dev.test"},"rules":[{"file":"ENT.json","entity_id":"ENT","hash":"blake3:0"}]}`
	tests := []struct {
		name, file, body string
		skipped          bool
	}{
		{"published index", "index.json", index, true},
		{"index under an entity's name", "ENT.json", index, false},
		{"index in another case", "INDEX.json", index, false},
		{"manifest under an entity's name", "ENT.json", `{"realm":{}}`, false},
		{"manifest named index", "index.json", `{"realm":{}}`, false},
		{"index listing nothing", "index.json", `{"realm":{},"rules":[]}`, false},
		{"index entry without a file", "index.json", `{"realm":{},"rules":[{"entity_id":"ENT"}]}`, false},
		{"entity named index", "index.json", `{"realm":{},"entity":{},"rules":[{"file":"A.json"}]}`, false},
		{"entity without the key named index", "index.json", `{"entity":{"id":"INDEX"},"rules":[]}`, false},
		{"rule among index entries", "index.json", `{"realm":{},"rules":[{"file":"A.json","state":"I"}]}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, mode := range walkModes {
				dir := t.TempDir()
				writeWalkFile(t, dir, tt.file, tt.body)
				var calls, failed int
				err := entity.WalkEntityFiles(dir, func(path string, _ []byte, _ *entity.File, parseErr error) error {
					calls++
					if parseErr != nil && filepath.Base(path) == tt.file {
						failed++
					}
					return nil
				}, entity.WithRecursive(mode.recursive))
				want := 1
				if tt.skipped {
					want = 0
				}
				if err != nil || calls != want || failed != want {
					t.Errorf("%s walk: err = %v, calls = %d, failures = %d; want %d", mode.name, err, calls, failed, want)
				}
			}
		})
	}
}

func TestNullRealmKeyIsStillAnEntity(t *testing.T) {
	if _, err := entity.Parse([]byte(`{"realm":null,"entity":{"id":"ENT"}}`)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}

// render tells a manifest from an entity after parsing it.
func TestParseStillReadsAPureManifest(t *testing.T) {
	if _, err := entity.Parse([]byte(`{"realm":{"id":"dev.test"},"dependencies":[],"notes":[]}`)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}
