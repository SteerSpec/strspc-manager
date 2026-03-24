package entity

import (
	"encoding/json"
	"fmt"
	"os"
)

// Parse decodes raw JSON into a File. Use this when the data is already in
// memory (e.g. received over HTTP in strspc-cloud).
func Parse(data []byte) (*File, error) {
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parsing entity JSON: %w", err)
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

// IsRealmJSON detects Realm manifest files by checking for a "realm" top-level key.
func IsRealmJSON(data []byte) bool {
	var probe struct {
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
