package ruleresolve

import (
	"slices"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// entityTree builds an entity file with the given sub-entities.
func entityTree(id string, subs ...entity.File) entity.File {
	f := entity.File{SubEntities: subs}
	f.Entity.ID = id
	return f
}

// resolvedFrom wraps an entity file as resolved from source.
func resolvedFrom(source string, f entity.File) *ResolvedFile {
	return &ResolvedFile{File: &f, ResolvedSource: source, Path: source + "/" + f.Entity.ID + ".json"}
}

func TestCheckCollisions(t *testing.T) {
	tests := []struct {
		name     string
		files    []*ResolvedFile
		wantMsgs []string
		wantPath string
	}{
		{
			name: "sub-entity collides with a top-level entity",
			files: []*ResolvedFile{
				resolvedFrom("a", entityTree("AAA", entityTree("DUP"))),
				resolvedFrom("b", entityTree("DUP")),
			},
			wantMsgs: []string{`EUID "DUP" provided by both a and b`},
			wantPath: "b/DUP.json",
		},
		{
			name: "deeply nested sub-entities collide",
			files: []*ResolvedFile{
				resolvedFrom("a", entityTree("AAA", entityTree("BBB", entityTree("DUP")))),
				resolvedFrom("b", entityTree("CCC", entityTree("DUP"))),
			},
			wantMsgs: []string{`EUID "DUP" provided by both a and b`},
			wantPath: "b/CCC.json",
		},
		{
			name: "every later source is compared with the first provider",
			files: []*ResolvedFile{
				resolvedFrom("a", entityTree("DUP")),
				resolvedFrom("b", entityTree("DUP")),
				resolvedFrom("c", entityTree("DUP")),
			},
			wantMsgs: []string{
				`EUID "DUP" provided by both a and b`,
				`EUID "DUP" provided by both a and c`,
			},
			wantPath: "b/DUP.json",
		},
		{
			name: "one source may repeat an entity",
			files: []*ResolvedFile{
				resolvedFrom("a", entityTree("DUP")),
				resolvedFrom("a", entityTree("AAA", entityTree("DUP"))),
			},
		},
		{
			// LocalSource rejects an empty top-level ID but not a nested one.
			name: "sub-entities without an id never collide",
			files: []*ResolvedFile{
				resolvedFrom("a", entityTree("AAA", entityTree(""))),
				resolvedFrom("b", entityTree("BBB", entityTree(""))),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := &result.Result{}
			checkCollisions(tt.files, res)

			var msgs []string
			for _, d := range res.Diagnostics {
				if d.Code != "RSV005" || d.Severity != result.Error {
					t.Errorf("unexpected diagnostic: %v", d)
				}
				msgs = append(msgs, d.Message)
			}
			if !slices.Equal(msgs, tt.wantMsgs) {
				t.Fatalf("messages = %q, want %q", msgs, tt.wantMsgs)
			}
			if tt.wantPath != "" && res.Diagnostics[0].Path != tt.wantPath {
				t.Errorf("path = %q, want %q", res.Diagnostics[0].Path, tt.wantPath)
			}
		})
	}
}
