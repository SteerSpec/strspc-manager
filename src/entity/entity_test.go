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

func TestRealmFileRuleIdentifierFormat(t *testing.T) {
	t.Run("object", func(t *testing.T) {
		input := `{"realm": {"id": "dev.test", "title": "Test", "version": "0.1.0"},
			"rule_identifier_format": {"brackets": "{,}", "euid_position": "closing",
				"id_splitter": "_", "revision_splitter": "#", "state_splitter": "\\"}}`
		var rf RealmFile
		if err := json.Unmarshal([]byte(input), &rf); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		want := RuleIdentifierFormat{Brackets: "{,}", EUIDPosition: "closing", IDSplitter: "_", RevisionSplitter: "#", StateSplitter: `\`}
		if rf.RuleIdentifierFormat == nil || *rf.RuleIdentifierFormat != want {
			t.Errorf("RuleIdentifierFormat = %+v, want %+v", rf.RuleIdentifierFormat, want)
		}
	})

	t.Run("null", func(t *testing.T) {
		var rf RealmFile
		if err := json.Unmarshal([]byte(`{"rule_identifier_format": null}`), &rf); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if rf.RuleIdentifierFormat != nil {
			t.Errorf("RuleIdentifierFormat = %+v, want nil", rf.RuleIdentifierFormat)
		}
	})
}

// TestInheritanceFieldsRoundTrip checks that inherits and overrides survive a
// parse/marshal round trip and are omitted when unset, so files that do not
// use inheritance keep the same canonical hash after a rewrite.
func TestInheritanceFieldsRoundTrip(t *testing.T) {
	input := `{
		"$schema": "./_schema/entity.v1.schema.json",
		"entity": {"id": "SAVACCT", "title": "Savings", "inherits": "dev.example.core:ACCT"},
		"rule_set": {"version": "0.1.0", "timestamp": "2026-10-29T00:00:00Z", "hash": null},
		"rules": [
			{"id": "SAVACCT-001", "revision": 0, "state": "D", "body": "b", "added_by": "t", "added_at": "2026-10-29", "supersedes": null, "overrides": "dev.example.core:ACCT-005"},
			{"id": "SAVACCT-002", "revision": 0, "state": "D", "body": "b", "added_by": "t", "added_at": "2026-10-29", "supersedes": null}
		],
		"notes": []
	}`
	f, err := Parse([]byte(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if f.Entity.Inherits != "dev.example.core:ACCT" {
		t.Errorf("Inherits = %q", f.Entity.Inherits)
	}
	if f.Rules[0].Overrides == nil || *f.Rules[0].Overrides != "dev.example.core:ACCT-005" {
		t.Errorf("Overrides = %v", f.Rules[0].Overrides)
	}
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out := string(data)
	if !strings.Contains(out, `"inherits":"dev.example.core:ACCT"`) || !strings.Contains(out, `"overrides":"dev.example.core:ACCT-005"`) {
		t.Errorf("fields lost on marshal: %s", out)
	}
	if strings.Count(out, `"overrides"`) != 1 {
		t.Errorf("overrides must be omitted when nil: %s", out)
	}
}

// TestInheritanceFieldsKeepHash checks that rewriting a file that does not
// use inheritance adds no fields and leaves its canonical hash unchanged.
func TestInheritanceFieldsKeepHash(t *testing.T) {
	data := []byte(`{
		"$schema": "./_schema/entity.v1.schema.json",
		"entity": {"id": "ACCT", "title": "Account", "parent": "BANK"},
		"rule_set": {"version": "0.1.0", "timestamp": "2026-10-29T00:00:00Z", "hash": null},
		"rules": [
			{"id": "ACCT-001", "revision": 0, "state": "P", "body": "b", "added_by": "t", "added_at": "2026-10-29", "supersedes": null},
			{"id": "ACCT-002", "revision": 1, "state": "D", "body": "c", "added_by": "t", "added_at": "2026-10-29", "supersedes": "ACCT-001"}
		],
		"notes": [
			{"id": "ACCT-001/01", "rule_ref": "ACCT-001", "type": "rationale", "content": "why", "added_by": "t", "added_at": "2026-10-29", "revision": 0}
		]
	}`)
	f, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rewritten, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(rewritten), `"inherits"`) || strings.Contains(string(rewritten), `"overrides"`) {
		t.Errorf("rewrite added inheritance fields: %s", rewritten)
	}
	before, err := ComputeHash(data)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	after, err := ComputeHash(rewritten)
	if err != nil {
		t.Fatalf("hash rewritten: %v", err)
	}
	if before != after {
		t.Errorf("hash changed on rewrite: %s → %s", before, after)
	}
}
