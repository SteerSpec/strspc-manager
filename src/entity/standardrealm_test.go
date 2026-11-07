package entity

import (
	"strings"
	"testing"
)

func TestIsLiveStandardRule(t *testing.T) {
	sup := "STD-001"
	tests := []struct {
		name string
		rule Rule
		want bool
	}{
		{"draft", Rule{State: StateDraft}, true},
		{"draft fix", Rule{State: StateDraft, Supersedes: &sup}, false},
		{"published", Rule{State: StatePublished}, true},
		{"published fix", Rule{State: StatePublished, Supersedes: &sup}, true},
		{"implemented", Rule{State: StateImplemented}, true},
		{"abandoned", Rule{State: StateAbandoned}, false},
		{"retired", Rule{State: StateRetired}, false},
		{"terminated", Rule{State: StateTerminated}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsLiveStandardRule(tt.rule); got != tt.want {
				t.Errorf("IsLiveStandardRule = %v, want %v", got, tt.want)
			}
		})
	}
}

// The manifest hash leaves out the hash key itself (RLMMNFST-013), so adding
// or changing it doesn't change the hash, and any other change does.
func TestComputeRealmHash(t *testing.T) {
	base := `{"$schema":"./_schema/realm.v1.schema.json","realm":{"id":"dev.test.std","title":"S","version":"1.0.0"}}`
	withHash := `{"$schema":"./_schema/realm.v1.schema.json","hash":"blake3:` + strings.Repeat("0", 64) + `","realm":{"id":"dev.test.std","title":"S","version":"1.0.0"}}`
	reordered := `{
		"realm": {"version": "1.0.0", "title": "S", "id": "dev.test.std"},
		"$schema": "./_schema/realm.v1.schema.json"
	}`
	bumped := `{"$schema":"./_schema/realm.v1.schema.json","realm":{"id":"dev.test.std","title":"S","version":"1.0.1"}}`

	want, err := ComputeRealmHash([]byte(base))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(want, "blake3:") || len(want) != len("blake3:")+64 {
		t.Fatalf("hash %q is not blake3:<64 hex>", want)
	}
	// "Computed as for a Rule Set Hash": with no rule_set and no hash to drop,
	// both hashes are of the same canonical bytes.
	if ruleSetStyle, err := ComputeHash([]byte(base)); err != nil || ruleSetStyle != want {
		t.Errorf("ComputeHash = %s, %v; want %s, as ComputeRealmHash", ruleSetStyle, err, want)
	}
	// Pinned so that a change to the canonical form shows up here, not as a
	// mismatch in every Realm that declares a hash.
	if want != "blake3:f137d1655f6316aae9090e63833391b0a8b8769445801942d5abc2178f118233" {
		t.Errorf("hash of the base manifest changed: %s", want)
	}
	for name, in := range map[string]string{"with hash": withHash, "reordered": reordered} {
		got, err := ComputeRealmHash([]byte(in))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got != want {
			t.Errorf("%s: hash %s, want %s", name, got, want)
		}
	}
	got, err := ComputeRealmHash([]byte(bumped))
	if err != nil {
		t.Fatal(err)
	}
	if got == want {
		t.Error("a version bump must change the manifest hash")
	}

	// Removing the key is not nulling it: a null hash is hashed as content.
	nulled, err := ComputeRealmHash([]byte(`{"hash":null}`))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := ComputeRealmHash([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if nulled != empty {
		t.Errorf("a top-level hash key is removed whatever its value: %s != %s", nulled, empty)
	}
	// Only the top-level key is removed.
	nested, err := ComputeRealmHash([]byte(`{"realm":{"hash":"x"}}`))
	if err != nil {
		t.Fatal(err)
	}
	bare, err := ComputeRealmHash([]byte(`{"realm":{}}`))
	if err != nil {
		t.Fatal(err)
	}
	if nested == bare {
		t.Error("a hash key below the top level must be hashed")
	}
}

func TestComputeRealmHash_InvalidInput(t *testing.T) {
	for _, in := range []string{``, `[]`, `{"a":1} {}`, `{`} {
		if _, err := ComputeRealmHash([]byte(in)); err == nil {
			t.Errorf("ComputeRealmHash(%q) succeeded, want an error", in)
		}
	}
}

func TestValidateStandardRef_ProfileNeedsRealm(t *testing.T) {
	ref := StandardRef{ID: "owasp.asvs", Relation: RelationImplements, Version: "5.0.0", Profile: "L1"}
	if err := ValidateStandardRef(ref); err == nil || !strings.Contains(err.Error(), "STDREF-019") {
		t.Errorf("profile without realm: got %v, want a STDREF-019 error", err)
	}
	ref.Realm = "dev.steerspec.std.owasp.asvs.v5"
	if err := ValidateStandardRef(ref); err != nil {
		t.Errorf("profile with realm: %v", err)
	}
}
