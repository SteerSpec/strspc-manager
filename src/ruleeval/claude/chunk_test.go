package claude

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

func fileDiff(path string, hunks ...string) string {
	var b strings.Builder
	b.WriteString("diff --git a/" + path + " b/" + path + "\n")
	b.WriteString("--- a/" + path + "\n+++ b/" + path + "\n")
	for _, h := range hunks {
		b.WriteString(h)
	}
	return b.String()
}

func hunk(start int, lines ...string) string {
	var b strings.Builder
	b.WriteString("@@ -" + itoa(start) + ",1 +" + itoa(start) + "," + itoa(len(lines)) + " @@\n")
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestSplitFiles(t *testing.T) {
	a := fileDiff("a.go", hunk(1, "x"))
	b := fileDiff("b.go", hunk(1, "y"), hunk(5, "z"))
	files := splitFiles(a + b)
	if len(files) != 2 || files[0] != a || files[1] != b {
		t.Fatalf("splitFiles = %q", files)
	}
	if got := splitFiles(""); len(got) != 0 {
		t.Errorf("empty diff → %d files", len(got))
	}
	if got := splitFiles("preamble\n" + a); len(got) != 2 || got[0] != "preamble\n" {
		t.Errorf("preamble should be its own element, got %q", got)
	}
}

func TestSplitHunks(t *testing.T) {
	h1, h2 := hunk(1, "y"), hunk(5, "z")
	header, hunks := splitHunks(fileDiff("b.go", h1, h2))
	if header != "diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n" {
		t.Errorf("header = %q", header)
	}
	if len(hunks) != 2 || hunks[0] != h1 || hunks[1] != h2 {
		t.Errorf("hunks = %q", hunks)
	}
	header, hunks = splitHunks("Binary files differ\n")
	if header != "Binary files differ\n" || len(hunks) != 0 {
		t.Errorf("no-hunk file: header=%q hunks=%q", header, hunks)
	}
}

func TestFilePath(t *testing.T) {
	cases := map[string]struct{ diff, want string }{
		"plain":         {fileDiff("src/x.go", hunk(1, "a")), "src/x.go"},
		"plus-only":     {"--- a/old.go\n+++ b/new.go\n@@ -1 +1 @@\n", "new.go"},
		"garbage":       {"garbage\n", "<unknown>"},
		"spaces":        {fileDiff("src/my file.go", hunk(1, "a")), "src/my file.go"},
		"spaces-header": {"diff --git a/src/my file.go b/src/my file.go\nBinary files differ\n", "src/my file.go"},
		"quoted":        {"diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n--- \"a/caf\\303\\251.go\"\n+++ \"b/caf\\303\\251.go\"\n@@ -1 +1 @@\n", "café.go"},
		"quoted-header": {"diff --git \"a/with \\\"q\\\".go\" \"b/with \\\"q\\\".go\"\nBinary files differ\n", "with \"q\".go"},
		"deleted":       {"diff --git a/gone.go b/gone.go\ndeleted file mode 100644\n--- a/gone.go\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", "gone.go"},
		"timestamp":     {"--- a/t.go\t2026-01-01 00:00:00\n+++ b/t.go\t2026-01-02 00:00:00\n@@ -1 +1 @@\n", "t.go"},
		"rename-header": {"diff --git a/old name.go b/new name.go\nsimilarity index 90%\n", "new name.go"},
		"rename-to":     {"diff --git a/old b/y b/docs/SPEC.md\nsimilarity index 100%\nrename from old\nrename to y b/docs/SPEC.md\n", "y b/docs/SPEC.md"},
		"rename-quoted": {"diff --git a/old \"b/caf\\303\\251 b/x.go\"\nrename from old\nrename to \"caf\\303\\251 b/x.go\"\n", "café b/x.go"},
		"copy-to":       {"diff --git a/old b/y b/docs/SPEC.md\nsimilarity index 100%\ncopy from old\ncopy to y b/docs/SPEC.md\n", "y b/docs/SPEC.md"},
	}
	for name, tc := range cases {
		if got := filePath(tc.diff); got != tc.want {
			t.Errorf("%s: filePath = %q, want %q", name, got, tc.want)
		}
	}
}

func TestChunkDiff_PacksWholeFiles(t *testing.T) {
	a := fileDiff("a.go", hunk(1, "aaaa"))
	b := fileDiff("b.go", hunk(1, "bbbb"))
	c := fileDiff("c.go", hunk(1, "cccc"))
	chunks, oversize := chunkDiff(a+b+c, len(a)+len(b)+1)
	if len(oversize) != 0 {
		t.Fatalf("unexpected oversize %+v", oversize)
	}
	if len(chunks) != 2 || chunks[0] != a+b || chunks[1] != c {
		t.Fatalf("chunks = %q", chunks)
	}
	// Everything fits in one chunk.
	chunks, _ = chunkDiff(a+b+c, 1<<20)
	if len(chunks) != 1 || chunks[0] != a+b+c {
		t.Errorf("single chunk expected, got %d", len(chunks))
	}
}

func TestChunkDiff_SplitsLargeFileOnHunks(t *testing.T) {
	h1 := hunk(1, strings.Repeat("a", 40))
	h2 := hunk(9, strings.Repeat("b", 40))
	h3 := hunk(9, strings.Repeat("c", 40))
	f := fileDiff("big.go", h1, h2, h3)
	header, _ := splitHunks(f)
	budget := len(header) + len(h1) + len(h2) + 1 // two hunks per chunk
	chunks, oversize := chunkDiff(f, budget)
	if len(oversize) != 0 {
		t.Fatalf("unexpected oversize %+v", oversize)
	}
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d: %q", len(chunks), chunks)
	}
	if chunks[0] != header+h1+h2 {
		t.Errorf("chunk 0 should carry the header once and two hunks, got %q", chunks[0])
	}
	if chunks[1] != header+h3 {
		t.Errorf("chunk 1 should repeat the header, got %q", chunks[1])
	}
}

func TestChunkDiff_OversizeHunkReported(t *testing.T) {
	small := fileDiff("small.go", hunk(1, "ok"))
	huge := fileDiff("huge.go", hunk(1, strings.Repeat("x", 500)))
	chunks, oversize := chunkDiff(small+huge, 200)
	if len(chunks) != 1 || chunks[0] != small {
		t.Fatalf("expected only the small file, got %q", chunks)
	}
	if len(oversize) != 1 || oversize[0].File != "huge.go" || oversize[0].Bytes <= 500 {
		t.Fatalf("oversize = %+v", oversize)
	}
	// A hunk-less oversize file is reported too.
	_, oversize = chunkDiff("diff --git a/bin b/bin\n"+strings.Repeat("Binary files differ\n", 20), 50)
	if len(oversize) != 1 || oversize[0].File != "bin" {
		t.Fatalf("hunk-less oversize = %+v", oversize)
	}
}

func TestChangedFiles(t *testing.T) {
	big := fileDiff("big.go", hunk(1, "a"), hunk(9, "b"))
	got := changedFiles("preamble\n" + fileDiff("a.go", hunk(1, "x")) + big)
	if strings.Join(got, ",") != "a.go,big.go" {
		t.Errorf("changedFiles = %q, want each file once and no preamble", got)
	}
}

func TestCapFiles(t *testing.T) {
	files := []string{"aa", "bb", "cc"} // 5 bytes each as "- aa\n"
	cases := []struct {
		budget   int
		kept     string
		unlisted int
	}{
		{15, "aa,bb,cc", 0},
		{14, "aa,bb", 1},
		{5, "aa", 2},
		{4, "", 3},
	}
	for _, tc := range cases {
		kept, unlisted := capFiles(files, tc.budget)
		if strings.Join(kept, ",") != tc.kept || unlisted != tc.unlisted {
			t.Errorf("capFiles(%d) = %q, %d; want %q, %d", tc.budget, kept, unlisted, tc.kept, tc.unlisted)
		}
	}
	// Escaping lengthens an entry, and the cap counts what is rendered: "<<"
	// takes 11 bytes as `- \x3c\x3c` with its newline, not 5.
	if kept, unlisted := capFiles([]string{"<<"}, 10); len(kept) != 0 || unlisted != 1 {
		t.Errorf("capFiles must measure the escaped entry, got %q, %d", kept, unlisted)
	}
}

func TestMergeVerdict_Precedence(t *testing.T) {
	dst := map[string]*ruleeval.EvalResponse{}
	mergeVerdict(dst, "R", &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable, Explanation: "na"})
	mergeVerdict(dst, "R", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant, Explanation: "ok"})
	if dst["R"].Verdict != ruleeval.Compliant {
		t.Fatalf("compliant should beat not_applicable, got %s", dst["R"].Verdict)
	}
	mergeVerdict(dst, "R", &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable})
	if dst["R"].Verdict != ruleeval.Compliant {
		t.Fatalf("not_applicable must not downgrade compliant")
	}
	v1 := &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Explanation: "first", Confidence: 0.6, Evidence: []ruleeval.Location{{File: "a.go", Line: 1}}}
	mergeVerdict(dst, "R", v1)
	if dst["R"].Verdict != ruleeval.Violated || dst["R"].Explanation != "first" {
		t.Fatalf("violated should win: %+v", dst["R"])
	}
	mergeVerdict(dst, "R", &ruleeval.EvalResponse{Verdict: ruleeval.Violated, Explanation: "second", Confidence: 0.9, Evidence: []ruleeval.Location{{File: "b.go", Line: 2}}})
	got := dst["R"]
	if got.Explanation != "first" || len(got.Evidence) != 2 || got.Confidence != 0.9 {
		t.Fatalf("second violation should accumulate evidence and max confidence, got %+v", got)
	}
	mergeVerdict(dst, "R", &ruleeval.EvalResponse{Verdict: ruleeval.Compliant})
	if dst["R"].Verdict != ruleeval.Violated {
		t.Fatal("compliant must not downgrade violated")
	}
	// Merging must not alias the source slice.
	v1.Evidence[0].Line = 99
	if dst["R"].Evidence[0].Line == 99 {
		t.Error("mergeVerdict must copy evidence")
	}
}

func TestEstimateTokens(t *testing.T) {
	if estimateTokens("") != 0 || estimateTokens("abc") != 1 || estimateTokens("abcd") != 2 {
		t.Errorf("estimateTokens: %d %d %d", estimateTokens(""), estimateTokens("abc"), estimateTokens("abcd"))
	}
	for _, n := range []int{0, 1, 2, 3, 4, 5, 6, 3907, 1 << 20} {
		if got, want := estimateTokensForBytes(n), estimateTokens(strings.Repeat("x", n)); got != want {
			t.Errorf("estimateTokensForBytes(%d) = %d, want %d", n, got, want)
		}
	}
}

func TestFirstError_PrefersCauseOverCancellation(t *testing.T) {
	cancelled := fmt.Errorf("claude: request failed: %w", context.Canceled)
	cause := fmt.Errorf("%w (max_tokens=1)", ErrTruncated)
	unavailable := fmt.Errorf("%w: HTTP 401", ruleeval.ErrUnavailable)

	if got := firstError([]error{nil, cancelled, cause}); !errors.Is(got, ErrTruncated) {
		t.Errorf("expected the real cause, got %v", got)
	}
	if got := firstError([]error{cancelled, cause, unavailable}); !errors.Is(got, ruleeval.ErrUnavailable) {
		t.Errorf("ErrUnavailable must win, got %v", got)
	}
	if got := firstError([]error{nil, cancelled}); !errors.Is(got, context.Canceled) {
		t.Errorf("cancellation alone is still reported, got %v", got)
	}
	if got := firstError([]error{nil, nil}); got != nil {
		t.Errorf("no errors → nil, got %v", got)
	}
}

func ruleWithBody(id string, n int) ruleeval.RuleInput {
	return ruleeval.RuleInput{Rule: entity.Rule{ID: id, Revision: 1, State: "I", Body: strings.Repeat("x", n)}}
}

func TestGroupRules_ByCountAndTokens(t *testing.T) {
	small := []ruleeval.RuleInput{ruleWithBody("S-001", 30), ruleWithBody("S-002", 30), ruleWithBody("S-003", 30)}
	groups, tooLarge := groupRules(small, 2, 1<<20)
	if len(tooLarge) != 0 || len(groups) != 2 || len(groups[0]) != 2 || len(groups[1]) != 1 {
		t.Fatalf("count split: groups=%d tooLarge=%d", len(groups), len(tooLarge))
	}

	// Each rule costs ~ruleTokens; a budget that fits two but not three
	// splits on tokens even though the count limit would allow all three.
	per := ruleTokens(small[0])
	groups, tooLarge = groupRules(small, 10, per*2)
	if len(tooLarge) != 0 || len(groups) != 2 || len(groups[0]) != 2 {
		t.Fatalf("token split: groups=%v tooLarge=%d", len(groups), len(tooLarge))
	}

	// A rule bigger than the whole budget is reported, the others proceed.
	mixed := []ruleeval.RuleInput{ruleWithBody("B-001", 5000), small[0], small[1]}
	groups, tooLarge = groupRules(mixed, 10, per*2)
	if len(tooLarge) != 1 || tooLarge[0].Rule.ID != "B-001" {
		t.Fatalf("expected B-001 too large, got %+v", tooLarge)
	}
	if len(groups) != 1 || len(groups[0]) != 2 {
		t.Fatalf("remaining rules should form one group, got %d groups", len(groups))
	}

	if groups, tooLarge = groupRules(nil, 10, 100); len(groups) != 0 || len(tooLarge) != 0 {
		t.Fatal("empty input → no groups")
	}
}
