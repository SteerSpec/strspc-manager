package entity

import (
	"encoding/json"
	"strings"
	"testing"
)

// A manifest's contributors survive a parse and rewrite, as they do when the
// CLI rewrites realm.json for `realm dep add` or `realm standards add`.
func TestRealmContributorsRoundTrip(t *testing.T) {
	in := `{"realm":{"id":"dev.example.test","title":"x","version":"1.0.0",` +
		`"contributors":[{"id":"github:pleberre","email":"dev@example.com","github_id":1234},{"id":"github:steerspec-sync[bot]"}]},` +
		`"dependencies":[],"rule_identifier_format":null,"sub_realms":[]}`
	rf, err := ParseRealm([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Contributor{
		{ID: "github:pleberre", Email: "dev@example.com", GitHubID: 1234},
		{ID: "github:steerspec-sync[bot]"},
	}
	if len(rf.Realm.Contributors) != len(want) {
		t.Fatalf("contributors = %+v", rf.Realm.Contributors)
	}
	for i := range want {
		if rf.Realm.Contributors[i] != want[i] {
			t.Errorf("contributor %d = %+v, want %+v", i, rf.Realm.Contributors[i], want[i])
		}
	}
	out, err := json.Marshal(rf)
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseRealm(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Realm.Contributors) != len(want) || back.Realm.Contributors[0] != want[0] || back.Realm.Contributors[1] != want[1] {
		t.Fatalf("rewritten manifest lost contributors: %s", out)
	}
}

// A manifest without contributors keeps its bytes: no empty array, and no
// empty email or zero github_id on an entry.
func TestRealmContributorsOmittedWhenEmpty(t *testing.T) {
	data, err := json.Marshal(RealmMeta{ID: "a.b", Title: "x", Version: "1.0.0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "contributors") {
		t.Fatalf("%s holds contributors", data)
	}
	data, err = json.Marshal(Contributor{ID: "github:pleberre"})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"id":"github:pleberre"}` {
		t.Fatalf("got %s", data)
	}
}
