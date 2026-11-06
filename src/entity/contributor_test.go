package entity

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestParseContributorID(t *testing.T) {
	tests := []struct {
		in, idp, subject string
		err              string
	}{
		{in: "github:pleberre", idp: "github", subject: "pleberre"},
		{in: "github:steerspec-sync[bot]", idp: "github", subject: "steerspec-sync[bot]"},
		{in: "github:a-b-1", idp: "github", subject: "a-b-1"},
		{in: "pleberre", err: "CTRB-001"},
		{in: ":pleberre", err: "CTRB-001"},
		{in: "github:", err: "CTRB-001"},
		{in: "gitlab:pleberre", err: "CTRB-002"},
		{in: "GitHub:pleberre", err: "CTRB-002"},
		{in: "github:a--b", err: "invalid github subject"},
		{in: "github:@pleberre", err: "invalid github subject"},
		{in: "github:pleberre ", err: "invalid github subject"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			idp, subject, err := ParseContributorID(tt.in)
			if tt.err != "" {
				if err == nil || !strings.Contains(err.Error(), tt.err) {
					t.Fatalf("err = %v, want one containing %q", err, tt.err)
				}
				if IsContributorID(tt.in) {
					t.Error("IsContributorID = true")
				}
				return
			}
			if err != nil || idp != tt.idp || subject != tt.subject {
				t.Fatalf("got %q, %q, %v", idp, subject, err)
			}
			if !IsContributorID(tt.in) {
				t.Error("IsContributorID = false")
			}
		})
	}
}

func TestIdentityProviders(t *testing.T) {
	got := IdentityProviders()
	if len(got) != 1 || got[0] != "github" {
		t.Fatalf("got %v", got)
	}
	got[0] = "changed"
	if IdentityProviders()[0] != "github" {
		t.Error("the returned slice aliases the provider list")
	}
}

// An @handle or a bare email address is not a contributor identifier since the
// error release of #248.
func TestIsContributorIDRejectsLegacyForms(t *testing.T) {
	for s, want := range map[string]bool{
		"github:pleberre":   true,
		"@pleberre":         false,
		"dev@example.com":   false,
		"gitlab:pleberre":   false,
		"pleberre":          false,
		"PLB <a@b.example>": false,
		"":                  false,
	} {
		if got := IsContributorID(s); got != want {
			t.Errorf("IsContributorID(%q) = %v, want %v", s, got, want)
		}
	}
}

func TestFindContributor(t *testing.T) {
	list := []Contributor{
		{ID: "github:pleberre", Email: "Dev@Example.com"},
		{ID: "github:steerspec-sync[bot]"},
		{ID: "broken"},
	}
	tests := []struct {
		addedBy string
		want    string
	}{
		{"github:pleberre", "github:pleberre"},
		{"github:PLeberre", "github:pleberre"},
		{"@pleberre", ""},
		{"@PLEBERRE", ""},
		{"dev@example.com", ""},
		{"github:steerspec-sync[bot]", "github:steerspec-sync[bot]"},
		{"@steerspec-sync[bot]", ""},
		{"github:someone", ""},
		{"@someone", ""},
		{"other@example.com", ""},
		{"broken", ""},
		{"pleberre", ""},
	}
	for _, tt := range tests {
		t.Run(tt.addedBy, func(t *testing.T) {
			got := FindContributor(list, tt.addedBy)
			switch {
			case tt.want == "" && got != nil:
				t.Fatalf("got %+v, want none", *got)
			case tt.want != "" && (got == nil || got.ID != tt.want):
				t.Fatalf("got %+v, want %s", got, tt.want)
			}
		})
	}
	if FindContributor(nil, "github:pleberre") != nil {
		t.Error("an empty list names someone")
	}
}

func writeRealm(t *testing.T, dir, id, subRealms, contributors string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"realm":{"id":"` + id + `","title":"x","version":"1.0.0"` + contributors + `},` +
		`"dependencies":[],"rule_identifier_format":null,"sub_realms":` + subRealms + `}`
	if err := os.WriteFile(filepath.Join(dir, "realm.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRealmContributors(t *testing.T) {
	root := t.TempDir()
	writeRealm(t, root, "dev.test.root", `["team"]`, `,"contributors":[{"id":"github:parent"}]`)
	writeRealm(t, filepath.Join(root, "team"), "dev.test.root.team", `[]`, `,"contributors":[{"id":"github:own"}]`)
	lone := filepath.Join(t.TempDir(), "lone")
	writeRealm(t, lone, "dev.test.lone", `[]`, `,"contributors":[{"id":"github:lone"}]`)
	bare := t.TempDir()
	writeRealm(t, bare, "dev.test.bare", `[]`, "")

	for name, tt := range map[string]struct {
		dir  string
		want []string
	}{
		"top-level realm":                {root, []string{"github:parent"}},
		"sub-realm uses parent CTRB-006": {filepath.Join(root, "team"), []string{"github:parent"}},
		"realm with no parent on disk":   {lone, []string{"github:lone"}},
		"realm declaring none":           {bare, nil},
		"directory without manifest":     {t.TempDir(), nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := RealmContributors(tt.dir)
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, c := range got {
				ids = append(ids, c.ID)
			}
			if strings.Join(ids, ",") != strings.Join(tt.want, ",") {
				t.Fatalf("got %v, want %v", ids, tt.want)
			}
		})
	}

	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, "realm.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := RealmContributors(broken); err == nil {
		t.Error("a broken manifest gave no error")
	}

	// A broken manifest above may be the parent's: an error, not the
	// child's own list.
	writeRealm(t, filepath.Join(broken, "team"), "dev.test.broken.team", `[]`, `,"contributors":[{"id":"github:own"}]`)
	if _, err := RealmContributors(filepath.Join(broken, "team")); err == nil {
		t.Error("a broken parent manifest gave no error")
	}
}

// A Realm above that does not list dir as a sub-realm, or whose ID does not
// make dir's, is not its parent.
func TestRealmContributorsNotParent(t *testing.T) {
	for name, tt := range map[string]struct{ subRealms, childID string }{
		"not listed":   {`[]`, "dev.test.root.team"},
		"ID unrelated": {`["team"]`, "dev.test.other"},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeRealm(t, root, "dev.test.root", tt.subRealms, `,"contributors":[{"id":"github:parent"}]`)
			writeRealm(t, filepath.Join(root, "team"), tt.childID, `[]`, `,"contributors":[{"id":"github:own"}]`)
			got, err := RealmContributors(filepath.Join(root, "team"))
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != 1 || got[0].ID != "github:own" {
				t.Fatalf("got %+v, want the Realm's own list", got)
			}
		})
	}
}
