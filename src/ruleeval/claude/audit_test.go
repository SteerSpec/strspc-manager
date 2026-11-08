package claude

import (
	"fmt"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// numbered returns a file of n lines "line 1" … "line n".
func numbered(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	return b.String()
}

// TestSplitLines checks a final newline ends the last line.
func TestSplitLines(t *testing.T) {
	for in, want := range map[string]int{"": 0, "a": 1, "a\n": 1, "a\nb": 2, "a\n\n": 2} {
		if got := len(splitLines(in)); got != want {
			t.Errorf("splitLines(%q) has %d lines, want %d", in, got, want)
		}
	}
}

// TestChunkFiles_PacksWholeFiles checks small files share a part in order
// and are each shown whole.
func TestChunkFiles_PacksWholeFiles(t *testing.T) {
	files := []ruleeval.File{{Path: "a.go", Content: "package a\n"}, {Path: "b.go", Content: "package b\n"}, {Path: "e.go"}}
	parts, oversize := chunkFiles(files, 1<<20)
	if len(parts) != 1 || len(oversize) != 0 {
		t.Fatalf("parts = %d, oversize = %v", len(parts), oversize)
	}
	want := prompt.FileSlice("a.go", 1, 1, []string{"package a"}) + prompt.FileSlice("b.go", 1, 1, []string{"package b"}) + prompt.FileSlice("e.go", 1, 0, nil)
	if parts[0] != want {
		t.Errorf("part =\n%s\nwant\n%s", parts[0], want)
	}
	a := prompt.FileSlice("a.go", 1, 1, []string{"package a"})
	if parts, _ := chunkFiles(files[:2], len(a)+1); len(parts) != 2 || parts[0] != a {
		t.Errorf("a budget for one file should give one part per file, got %q", parts)
	}
}

// TestChunkFiles_SplitsLargeFile checks a file larger than a part is shown
// in consecutive line ranges, each within the budget, numbered as in the
// file.
func TestChunkFiles_SplitsLargeFile(t *testing.T) {
	content := numbered(100)
	lines := splitLines(content)
	budget := 400
	parts, oversize := chunkFiles([]ruleeval.File{{Path: "big.go", Content: content}}, budget)
	if len(oversize) != 0 || len(parts) < 3 {
		t.Fatalf("parts = %d, oversize = %v", len(parts), oversize)
	}
	next := 1
	for _, p := range parts {
		if len(p) > budget {
			t.Errorf("part of %d bytes exceeds the budget of %d", len(p), budget)
		}
		var first, last, total int
		if _, err := fmt.Sscanf(strings.SplitN(p, "\n", 2)[0], `<file path="big.go" lines="%d-%d of %d">`, &first, &last, &total); err != nil {
			t.Fatalf("bad header in %q: %v", p, err)
		}
		if first != next || total != 100 || p != prompt.FileSlice("big.go", first, total, lines[first-1:last]) {
			t.Errorf("part shows lines %d-%d of %d, want to start at %d", first, last, total, next)
		}
		next = last + 1
	}
	if next != 101 {
		t.Errorf("parts end at line %d, want every line shown", next-1)
	}
}

// TestChunkFiles_Oversize checks a line too large for a part is reported
// and left out while the rest of the file is shown, and that a path too
// long for any part reports the whole file once.
func TestChunkFiles_Oversize(t *testing.T) {
	content := "short\n" + strings.Repeat("x", 500) + "\nshort again\n"
	parts, oversize := chunkFiles([]ruleeval.File{{Path: "w.go", Content: content}}, 200)
	if len(oversize) != 1 || oversize[0].File != "w.go" || oversize[0].Line != 2 || oversize[0].Bytes <= 200 {
		t.Fatalf("oversize = %+v, want line 2 of w.go", oversize)
	}
	joined := strings.Join(parts, "")
	if !strings.Contains(joined, "1\tshort\n") || !strings.Contains(joined, "3\tshort again\n") || strings.Contains(joined, "xxx") {
		t.Errorf("the other lines should be shown and the long one left out: %q", joined)
	}
	long := strings.Repeat("d/", 150) + "f.go"
	if _, oversize := chunkFiles([]ruleeval.File{{Path: long, Content: numbered(5)}}, 200); len(oversize) != 1 || oversize[0].Line != 0 {
		t.Errorf("oversize = %+v, want the whole file reported once", oversize)
	}
}

// TestPlanAudit_PartsStandAlone checks a split file set shows each part in
// a files block of its own, without the list of the set, which only the
// judgement carries, and reports a line it cannot send.
func TestPlanAudit_PartsStandAlone(t *testing.T) {
	files := []ruleeval.File{{Path: "a.go", Content: numbered(40)}, {Path: "b.go", Content: numbered(40) + strings.Repeat("y", 2000) + "\n"}}
	parts, block, errs := planAudit(files, 300)
	if len(parts) < 2 {
		t.Fatalf("parts = %d, want a split", len(parts))
	}
	for i, p := range parts {
		if b := block(p); b != "<files>\n"+p+"</files>\n" {
			t.Errorf("part %d is not a files block of its own: %q", i, b)
		}
	}
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "line 41 of b.go") || !strings.Contains(errs[0].Error(), "not audited") {
		t.Errorf("errs = %v, want line 41 of b.go reported", errs)
	}
	if parts, _, errs := planAudit(files[:1], 1<<16); len(parts) != 1 || len(errs) != 0 {
		t.Errorf("a set that fits is one part: %d parts, %v", len(parts), errs)
	}
}
