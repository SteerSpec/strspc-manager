package realmlint

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
)

// contributorRealm copies the parent and `sync` sub-realm fixture, leaves out
// the sub-realm's language (RM017), and sets the contributors of each
// manifest; nil leaves the key out.
func contributorRealm(t *testing.T, parent, sub []map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(testdataPath(t), "invalid", "sub_realm_language")
	if err := os.CopyFS(dir, os.DirFS(src)); err != nil {
		t.Fatal(err)
	}
	set := func(path string, contributors []map[string]any) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		realm := m["realm"].(map[string]any)
		delete(realm, "language")
		if contributors != nil {
			realm["contributors"] = contributors
		}
		if data, err = json.Marshal(m); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	set(filepath.Join(dir, "realm.json"), parent)
	set(filepath.Join(dir, "sync", "realm.json"), sub)
	return dir
}

func codeDiags(res *result.Result, code string) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

func TestRM019_Contributors(t *testing.T) {
	tests := []struct {
		name    string
		list    []map[string]any
		wantMsg []string
	}{
		{"valid", []map[string]any{{"id": "github:pleberre", "email": "dev@example.com", "github_id": 1}, {"id": "github:steerspec-sync[bot]"}}, nil},
		{"handle", []map[string]any{{"id": "@pleberre"}}, []string{"contributors[0]", "CTRB-001"}},
		{"unlisted provider", []map[string]any{{"id": "gitlab:pleberre"}}, []string{"contributors[0]", "CTRB-002"}},
		{"repeated", []map[string]any{{"id": "github:pleberre"}, {"id": "github:PLeberre"}}, []string{"contributors[1]", `repeats "github:pleberre"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := New().Lint(context.Background(), contributorRealm(t, tt.list, nil))
			got := codeDiags(res, "RM019")
			if tt.wantMsg == nil {
				if len(got) != 0 {
					t.Fatalf("RM019: %v", got)
				}
				if !res.OK() {
					t.Fatalf("not OK: %v", res.Diagnostics)
				}
				return
			}
			if len(got) != 1 || got[0].Severity != result.Error {
				t.Fatalf("RM019: %v", got)
			}
			for _, w := range tt.wantMsg {
				if !strings.Contains(got[0].Message, w) {
					t.Errorf("message %q lacks %q", got[0].Message, w)
				}
			}
			if filepath.Base(got[0].Path) != "realm.json" || strings.Contains(got[0].Path, "sync") {
				t.Errorf("path = %q, want the parent manifest", got[0].Path)
			}
		})
	}
}

// RM019 runs on a sub-realm's manifest too, and RM020 reports that it
// declares contributors at all.
func TestRM020_SubRealmContributors(t *testing.T) {
	res := New().Lint(context.Background(), contributorRealm(t,
		[]map[string]any{{"id": "github:pleberre"}},
		[]map[string]any{{"id": "pleberre"}}))
	for _, code := range []string{"RM019", "RM020"} {
		got := codeDiags(res, code)
		if len(got) != 1 || got[0].Severity != result.Error || !strings.HasSuffix(got[0].Path, filepath.Join("sync", "realm.json")) {
			t.Errorf("%s: %v", code, got)
		}
	}
	if got := codeDiags(res, "RM020"); len(got) == 1 && !strings.Contains(got[0].Message, "CTRB-006") {
		t.Errorf("RM020 message %q does not cite CTRB-006", got[0].Message)
	}

	res = New().Lint(context.Background(), contributorRealm(t, []map[string]any{{"id": "github:pleberre"}}, nil))
	if got := codeDiags(res, "RM020"); len(got) != 0 {
		t.Errorf("RM020 without sub-realm contributors: %v", got)
	}
}
