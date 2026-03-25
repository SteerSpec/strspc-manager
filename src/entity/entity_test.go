package entity

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRealmDepSourceUnmarshal(t *testing.T) {
	input := `{
		"realm_id": "dev.steerspec.core",
		"version": "0.1.0",
		"source": "github://SteerSpec/strspc-rules@latest/rules/core"
	}`
	var dep RealmDep
	if err := json.Unmarshal([]byte(input), &dep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if dep.Source != "github://SteerSpec/strspc-rules@latest/rules/core" {
		t.Errorf("Source = %q, want %q", dep.Source, "github://SteerSpec/strspc-rules@latest/rules/core")
	}
}

func TestRealmFileSubRealmsRoundTrip(t *testing.T) {
	input := `{
		"$schema": "./_schema/realm.v1.schema.json",
		"realm": {"id": "dev.test", "title": "Test", "version": "0.1.0"},
		"dependencies": [],
		"sub_realms": ["sync", "auth"],
		"rule_identifier_format": null
	}`
	var rf RealmFile
	if err := json.Unmarshal([]byte(input), &rf); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rf.SubRealms) != 2 || rf.SubRealms[0] != "sync" || rf.SubRealms[1] != "auth" {
		t.Errorf("SubRealms = %v, want [sync auth]", rf.SubRealms)
	}

	// Round-trip: marshal back and verify sub_realms present.
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"sub_realms"`) {
		t.Errorf("expected sub_realms in output, got: %s", data)
	}
}

func TestRealmFileSubRealmsOmitEmpty(t *testing.T) {
	rf := RealmFile{Realm: RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"}}
	data, err := json.Marshal(rf)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "sub_realms") {
		t.Errorf("expected sub_realms to be omitted, got: %s", data)
	}
}

func TestRealmDepSourceOmitEmpty(t *testing.T) {
	dep := RealmDep{RealmID: "dev.steerspec.core", Version: "0.1.0"}
	data, err := json.Marshal(dep)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "source") {
		t.Errorf("expected source to be omitted, got: %s", data)
	}
}
