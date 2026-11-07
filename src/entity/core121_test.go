package entity

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// standardEntityJSON is a Standard Realm entity file that uses every field core
// 1.21.0 adds to entity.v1, written in struct field order.
const standardEntityJSON = `{"$schema":"./_schema/entity.v1.schema.json",` +
	`"entity":{"id":"VTWO","title":"Authentication","standards":[` +
	`{"id":"owasp.asvs","relation":"implements","version":"5.0.0","url":"https://owasp.org/asvs","realm":"org.owasp.asvs","profile":"L2"}]},` +
	`"rule_set":{"version":"0.1.0","timestamp":"2026-11-04T00:00:00Z","hash":null},` +
	`"rules":[{"id":"VTWO-001","revision":0,"state":"D","body":"Passwords MUST be at least 12 characters long.",` +
	`"added_by":"github:alice","added_at":"2026-11-04","supersedes":null,"clause":"V2.1.1","profiles":["L1","L2","L3"]}],` +
	`"notes":[]}`

// standardRealmJSON is a Standard Realm manifest that uses every field core
// 1.21.0 adds to realm.v1, written in struct field order.
const standardRealmJSON = `{"$schema":"./_schema/realm.v1.schema.json",` +
	`"realm":{"id":"org.owasp.asvs","title":"OWASP ASVS","version":"5.0.0","language":"en",` +
	`"standards":[{"id":"owasp.asvs","relation":"derived_from","version":"5.0.0","realm":"org.owasp.asvs","profile":"L1"}],` +
	`"kind":"standard","license":"CC-BY-SA-4.0",` +
	`"attribution":{"text":"OWASP Application Security Verification Standard","url":"https://owasp.org/asvs"},` +
	`"edition":"5.0.0","profiles":["L1","L2","L3"],"contributors":[{"id":"github:alice"}]},` +
	`"dependencies":[{"realm_id":"dev.steerspec.core","version":"1.21.0"}],` +
	`"notes":[` +
	`{"id":"REALM/001","type":"exclusion","ref":"owasp.asvs:V2.1.1","content":"Not applicable.","added_by":"github:alice","added_at":"2026-11-04","revision":0},` +
	`{"id":"REALM/002","type":"rationale","content":"Why.","added_by":"github:alice","added_at":"2026-11-04","revision":1}],` +
	`"hash":"blake3:0000000000000000000000000000000000000000000000000000000000000000"}`

func TestCore121EntityFieldsRoundTrip(t *testing.T) {
	f, err := Parse([]byte(standardEntityJSON))
	if err != nil {
		t.Fatal(err)
	}
	r := f.Rules[0]
	if r.Clause != "V2.1.1" || strings.Join(r.Profiles, ",") != "L1,L2,L3" {
		t.Errorf("rule clause %q, profiles %v", r.Clause, r.Profiles)
	}
	ref := f.Entity.Standards[0]
	if ref.Realm != "org.owasp.asvs" || ref.Profile != "L2" {
		t.Errorf("standard realm %q, profile %q", ref.Realm, ref.Profile)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != standardEntityJSON {
		t.Errorf("round trip changed the file:\n got %s\nwant %s", out, standardEntityJSON)
	}
}

func TestCore121RealmFieldsRoundTrip(t *testing.T) {
	rf, err := ParseRealm([]byte(standardRealmJSON))
	if err != nil {
		t.Fatal(err)
	}
	m := rf.Realm
	if m.Kind != KindStandard || m.License != "CC-BY-SA-4.0" || m.Edition != "5.0.0" ||
		m.Attribution == nil || m.Attribution.URL != "https://owasp.org/asvs" || len(m.Profiles) != 3 {
		t.Errorf("realm meta %+v", m)
	}
	if len(rf.Notes) != 2 || rf.Notes[0].Ref != "owasp.asvs:V2.1.1" || rf.Notes[1].Ref != "" || rf.Notes[1].Revision != 1 {
		t.Errorf("realm notes %+v", rf.Notes)
	}
	if !strings.HasPrefix(rf.Hash, "blake3:") {
		t.Errorf("hash %q", rf.Hash)
	}
	out, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != standardRealmJSON {
		t.Errorf("round trip changed the manifest:\n got %s\nwant %s", out, standardRealmJSON)
	}
}

// The fixtures are valid against the core schemas this repo pins, so the
// round-trip tests exercise files the schemas accept.
func TestCore121FixturesMatchSchemas(t *testing.T) {
	for schemaFile, doc := range map[string]string{
		"entity.v1.schema.json": standardEntityJSON,
		"realm.v1.schema.json":  standardRealmJSON,
	} {
		raw, err := os.ReadFile(filepath.Join("..", "..", "rules", "_schema", schemaFile))
		if err != nil {
			t.Fatal(err)
		}
		sch, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		c := jsonschema.NewCompiler()
		c.UseLoader(jsonschema.SchemeURLLoader{})
		if err := c.AddResource(schemaFile, sch); err != nil {
			t.Fatal(err)
		}
		compiled, err := c.Compile(schemaFile)
		if err != nil {
			t.Fatal(err)
		}
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(doc))
		if err != nil {
			t.Fatal(err)
		}
		if err := compiled.Validate(inst); err != nil {
			t.Errorf("%s: %v", schemaFile, err)
		}
	}
}

// A file that uses none of the new fields marshals without them, so its bytes
// and hash stay as they are.
func TestCore121FieldsOmittedWhenEmpty(t *testing.T) {
	rf := RealmFile{Realm: RealmMeta{ID: "dev.test", Title: "Test", Version: "0.1.0"}}
	r := Rule{ID: "TST-001", State: StateDraft, Body: "x", AddedBy: "github:a", AddedAt: "2026-11-04"}
	ref := StandardRef{ID: "owasp.asvs", Relation: RelationImplements}
	for _, v := range []any{rf, r, ref} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"clause", "profiles", `"realm":"`, "profile", "kind", "license", "attribution", "edition", "notes", "hash"} {
			if strings.Contains(string(data), key) {
				t.Errorf("%s holds %s", data, key)
			}
		}
	}
}

// The rule set hash is computed over the file the structs marshal, so it must
// cover clause and profiles: hashing through the structs gives the hash of the
// file as written, and that hash moves when either field does.
func TestCore121RuleFieldsInHash(t *testing.T) {
	want, err := ComputeHash([]byte(standardEntityJSON))
	if err != nil {
		t.Fatal(err)
	}
	f, err := Parse([]byte(standardEntityJSON))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ComputeHash(out)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("hash through the structs %s, want %s", got, want)
	}
	for name, edit := range map[string]func(*Rule){
		"clause":   func(r *Rule) { r.Clause = "" },
		"profiles": func(r *Rule) { r.Profiles = nil },
	} {
		g, err := Parse([]byte(standardEntityJSON))
		if err != nil {
			t.Fatal(err)
		}
		edit(&g.Rules[0])
		data, err := json.Marshal(g)
		if err != nil {
			t.Fatal(err)
		}
		h, err := ComputeHash(data)
		if err != nil {
			t.Fatal(err)
		}
		if h == want {
			t.Errorf("hash ignores %s", name)
		}
	}
}
