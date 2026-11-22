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
			if entity.IsRealmJSON([]byte(tt.body)) {
				t.Error("IsRealmJSON = true for a file with entity content")
			}
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

func TestManifestWithoutEntityContentIsSkipped(t *testing.T) {
	tests := []struct{ name, body string }{
		{"manifest", `{"realm":{"id":"dev.test"},"dependencies":[],"notes":[]}`},
		{"published index", `{"version":"1.0.0","realm":{"id":"dev.test"},"schemas":[],"rules":[{"file":"ENT.json","entity_id":"ENT","hash":"blake3:0"}]}`},
		{"empty index", `{"realm":{},"rules":[]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !entity.IsRealmJSON([]byte(tt.body)) {
				t.Error("IsRealmJSON = false for a manifest")
			}
			dir := t.TempDir()
			writeWalkFile(t, dir, "index.json", tt.body)
			err := entity.WalkEntityFiles(dir, func(path string, _ []byte, _ *entity.File, _ error) error {
				t.Errorf("visited %s", filepath.Base(path))
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNullRealmKeyIsStillAnEntity(t *testing.T) {
	if _, err := entity.Parse([]byte(`{"realm":null,"entity":{"id":"ENT"}}`)); err != nil {
		t.Fatalf("Parse: %v", err)
	}
}
