package entity

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// errRealmKey is Parse's error for a file that is both manifest and entity.
var errRealmKey = errors.New(`an entity file can't have a top-level "realm" key`)

// Parse decodes raw JSON into a File. Use this when the data is already in
// memory (e.g. received over HTTP in strspc-cloud). Entity content beside a
// top-level "realm" key is an error.
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing entity JSON: %w", err)
	}
	if hasRealmKey(data) && !IsRealmJSON(data) {
		return nil, fmt.Errorf("parsing entity JSON: %w", errRealmKey)
	}
	return &f, nil
}

// Load reads and parses an entity JSON file from the given path.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading entity file %q: %w", path, err)
	}
	return Parse(data)
}

// ParseRealm decodes raw JSON into a RealmFile.
func ParseRealm(data []byte) (*RealmFile, error) {
	var r RealmFile
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parsing realm JSON: %w", err)
	}
	return &r, nil
}

// IsRealmJSON reports whether data is a Realm manifest, or a published index
// of one: it has a top-level "realm" key and no entity content. Entity content
// is an "entity", "rule_set" or "sub_entities" key, or "rules" that aren't a
// list of objects free of "id", "state" and "body".
func IsRealmJSON(data []byte) bool {
	if !hasRealmKey(data) {
		return false
	}
	var content struct {
		// Entity, RuleSet and SubEntities are non-empty when the key is present,
		// even as null.
		Entity      json.RawMessage `json:"entity"`
		RuleSet     json.RawMessage `json:"rule_set"`
		SubEntities json.RawMessage `json:"sub_entities"`
		// Rules holds what tells a rule from an index entry.
		Rules []struct {
			ID    json.RawMessage `json:"id"`
			State json.RawMessage `json:"state"`
			Body  json.RawMessage `json:"body"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(data, &content); err != nil {
		return false // Not provably a manifest, so not to be skipped.
	}
	if content.Entity != nil || content.RuleSet != nil || content.SubEntities != nil {
		return false
	}
	for _, r := range content.Rules {
		if r.ID != nil || r.State != nil || r.Body != nil {
			return false
		}
	}
	return true
}

// hasRealmKey reports whether data has a non-null top-level "realm" key.
func hasRealmKey(data []byte) bool {
	var probe struct {
		// Realm is present (non-nil) only in a Realm manifest.
		Realm *json.RawMessage `json:"realm"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	return probe.Realm != nil
}

// LoadRealm reads and parses a realm.json file from the given path.
func LoadRealm(path string) (*RealmFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading realm file %q: %w", path, err)
	}
	return ParseRealm(data)
}
