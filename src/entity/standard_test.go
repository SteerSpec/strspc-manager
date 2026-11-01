package entity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateStandardRef(t *testing.T) {
	tests := []struct {
		name string
		ref  StandardRef
		want string
	}{
		{"id and relation", StandardRef{ID: "owasp.asvs", Relation: RelationImplements}, ""},
		{"all fields", StandardRef{ID: "ietf.rfc2119", Relation: RelationDerivedFrom, Version: "BCP 14", URL: "https://www.rfc-editor.org/rfc/rfc2119"}, ""},
		{"leading digit and inner hyphens", StandardRef{ID: "3gpp.ts-23-501", Relation: RelationImplements}, ""},
		{"no dot", StandardRef{ID: "owasp", Relation: RelationImplements}, "id"},
		{"edge hyphen", StandardRef{ID: "owasp.asvs-", Relation: RelationImplements}, "id"},
		{"uppercase", StandardRef{ID: "OWASP.asvs", Relation: RelationImplements}, "id"},
		{"missing relation", StandardRef{ID: "owasp.asvs"}, "relation"},
		{"unknown relation", StandardRef{ID: "owasp.asvs", Relation: "cites"}, "relation"},
		{"relative url", StandardRef{ID: "owasp.asvs", Relation: RelationImplements, URL: "owasp.org/asvs"}, "url"},
		{"script url", StandardRef{ID: "owasp.asvs", Relation: RelationImplements, URL: "javascript://x/%0aalert(1)"}, "url"},
		{"no host", StandardRef{ID: "owasp.asvs", Relation: RelationImplements, URL: "https://:443"}, "url"},
		{"unknown scheme", StandardRef{ID: "owasp.asvs", Relation: RelationImplements, URL: "x://y"}, "url"},
		{"http url", StandardRef{ID: "owasp.asvs", Relation: RelationImplements, URL: "http://owasp.org/asvs"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateStandardRef(tt.ref)
			switch {
			case tt.want == "" && err != nil:
				t.Fatalf("unexpected error: %v", err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("got %v, want an error about %s", err, tt.want)
			}
		})
	}
}

func TestValidateStandardRefs_DuplicateID(t *testing.T) {
	errs := ValidateStandardRefs([]StandardRef{
		{ID: "owasp.asvs", Relation: RelationImplements},
		{ID: "owasp.asvs", Relation: RelationDerivedFrom},
	})
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "more than once") {
		t.Fatalf("got %v, want one duplicate error", errs)
	}
}

func TestParseStandardNote(t *testing.T) {
	tests := []struct {
		content, id, clause string
		ok                  bool
	}{
		{"owasp.asvs:2.1.1", "owasp.asvs", "2.1.1", true},
		{"ietf.rfc2119:1", "ietf.rfc2119", "1", true},
		{"iso.27001:A:5.1", "iso.27001", "A:5.1", true},
		{"owasp.asvs:", "", "", false},
		{"owasp.asvs:   ", "", "", false},
		{"owasp.asvs", "", "", false},
		{":2.1.1", "", "", false},
		{"owasp:2.1.1", "", "", false},
		{"owasp.asvs: 2.1.1", "", "", false},
		{"owasp.asvs:2.1.1\n", "", "", false},
		{"owasp.asvs:\u200b", "", "", false},
		{"iso.27001:A 5.1", "iso.27001", "A 5.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.content, func(t *testing.T) {
			id, clause, err := ParseStandardNote(tt.content)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, tt.ok)
			}
			if id != tt.id || clause != tt.clause {
				t.Fatalf("got (%q, %q), want (%q, %q)", id, clause, tt.id, tt.clause)
			}
		})
	}
}

func TestStandardNoteTypeIsMutable(t *testing.T) {
	if !IsValidNoteType(NoteTypeStandard) {
		t.Fatal("standard must be a valid note type")
	}
	if IsAppendOnlyNoteType(NoteTypeStandard) {
		t.Fatal("standard must stay revisable (NTE-017)")
	}
}

// Files that declare no standards must keep their bytes, or every existing
// entity's rule_set.hash and realm.json would change under this build.
func TestStandardsOmittedWhenEmpty(t *testing.T) {
	for _, v := range []any{Entity{ID: "ABC", Title: "x"}, RealmMeta{ID: "a.b", Title: "x", Version: "1.0.0"}} {
		data, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "standards") {
			t.Fatalf("%s holds standards", data)
		}
	}
}

func TestStandardsRoundTrip(t *testing.T) {
	in := `{"id":"ABC","title":"x","standards":[{"id":"owasp.asvs","relation":"implements","version":"5.0.0","url":"https://owasp.org/asvs"}]}`
	var e Entity
	if err := json.Unmarshal([]byte(in), &e); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != in {
		t.Fatalf("got %s", out)
	}
}

// Every core entity file must round-trip byte for byte through the structs.
func TestCoreFilesKeepTheirHash(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", ".strspc", "cache", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	local, err := filepath.Glob(filepath.Join("..", "..", "rules", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, p := range append(paths, local...) {
		if filepath.Base(p) == "realm.json" {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Parse(data)
		if err != nil {
			continue
		}
		if f.RuleSet.Hash == nil {
			continue
		}
		out, err := json.Marshal(f)
		if err != nil {
			t.Fatal(err)
		}
		got, err := ComputeHash(out)
		if err != nil {
			t.Fatal(err)
		}
		if got != *f.RuleSet.Hash {
			t.Errorf("%s: hash changed through the structs: %s", p, got)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no entity file checked")
	}
}

func TestResolveStandard_NearestFirst(t *testing.T) {
	near := []StandardRef{{ID: "owasp.asvs", Relation: RelationDerivedFrom}}
	far := []StandardRef{{ID: "owasp.asvs", Relation: RelationImplements}, {ID: "iab.tcf", Relation: RelationImplements}}
	if got := ResolveStandard("owasp.asvs", near, far); got == nil || got.Relation != RelationDerivedFrom {
		t.Fatalf("got %+v, want the nearest declaration", got)
	}
	if got := ResolveStandard("iab.tcf", near, far); got == nil {
		t.Fatal("a farther scope must resolve")
	}
	if got := ResolveStandard("ietf.rfc2119", near, far); got != nil {
		t.Fatalf("got %+v, want nil", got)
	}
}

// TestParentRealm checks when a manifest above a directory is its parent.
func TestParentRealm(t *testing.T) {
	write := func(t *testing.T, id string, subs ...string) string {
		t.Helper()
		dir := t.TempDir()
		data, err := json.Marshal(RealmFile{Realm: RealmMeta{ID: id, Title: "R", Version: "1.0.0"}, SubRealms: subs})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "realm.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	tests := []struct {
		name    string
		realm   string
		subs    []string
		childID string
		want    bool
	}{
		{"lists it", "dev.test.root", []string{"team"}, "dev.test.root.team", true},
		{"doesn't list it", "dev.test.root", nil, "dev.test.root.team", false},
		{"another realm's id", "dev.test.other", []string{"team"}, "dev.test.root.team", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := write(t, tt.realm, tt.subs...)
			got := ParentRealm(filepath.Join(dir, "team"), tt.childID)
			if (got != nil) != tt.want {
				t.Errorf("ParentRealm = %+v, want found=%v", got, tt.want)
			}
		})
	}
	if got := ParentRealm(t.TempDir(), "dev.test.root.team"); got != nil {
		t.Errorf("no manifest above: got %+v", got)
	}
}
