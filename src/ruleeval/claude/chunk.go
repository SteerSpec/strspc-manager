package claude

import (
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// estimateTokens approximates the token count of s as bytes/3, a
// deliberately conservative figure for code and diffs.
func estimateTokens(s string) int {
	return estimateTokensForBytes(len(s))
}

// estimateTokensForBytes is estimateTokens for a known byte count.
func estimateTokensForBytes(n int) int {
	return (n + 2) / 3
}

// oversizeHunk describes a hunk that cannot fit in a single request.
type oversizeHunk struct {
	File  string
	Bytes int
}

// splitFiles splits a unified diff on "diff --git" headers. Any preamble
// before the first header becomes its own element.
func splitFiles(diff string) []string {
	var files []string
	var cur strings.Builder
	for _, line := range strings.SplitAfter(diff, "\n") {
		if strings.HasPrefix(line, "diff --git ") && cur.Len() > 0 {
			files = append(files, cur.String())
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if cur.Len() > 0 {
		files = append(files, cur.String())
	}
	return files
}

// splitHunks separates a single file's diff into its header (everything
// before the first "@@") and its hunks.
func splitHunks(file string) (header string, hunks []string) {
	var cur strings.Builder
	inHunk := false
	for _, line := range strings.SplitAfter(file, "\n") {
		if strings.HasPrefix(line, "@@") {
			if inHunk {
				hunks = append(hunks, cur.String())
			} else {
				header = cur.String()
				inHunk = true
			}
			cur.Reset()
		}
		cur.WriteString(line)
	}
	if inHunk {
		hunks = append(hunks, cur.String())
	} else {
		header = cur.String()
	}
	return header, hunks
}

// filePath extracts the path of a file diff for diagnostics. It prefers
// the "+++" line (the "---" line for deletions), decoding Git's C-style
// quoting, and falls back to the "diff --git a/X b/X" header, which it
// splits in the middle so that paths containing spaces survive.
func filePath(file string) string {
	var header, oldSide string
	for _, line := range strings.SplitAfter(file, "\n") {
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "diff --git "):
			header = strings.TrimPrefix(line, "diff --git ")
		case strings.HasPrefix(line, "--- "):
			oldSide = strings.TrimPrefix(line, "--- ")
		case strings.HasPrefix(line, "+++ "):
			if p := diffSidePath(strings.TrimPrefix(line, "+++ "), "b/"); p != "" {
				return p
			}
			if p := diffSidePath(oldSide, "a/"); p != "" {
				return p // new side is /dev/null: the file was deleted
			}
		}
	}
	if header != "" {
		if p := headerPath(header); p != "" {
			return p
		}
	}
	return "<unknown>"
}

// diffSidePath decodes one side of a ---/+++ line. It returns "" for
// /dev/null or an empty side.
func diffSidePath(side, prefix string) string {
	side = strings.TrimSpace(side)
	if i := strings.Index(side, "\t"); i >= 0 {
		side = side[:i] // optional timestamp after a tab
	}
	if side == "" || side == "/dev/null" {
		return ""
	}
	side = unquoteGit(side)
	return strings.TrimPrefix(side, prefix)
}

// headerPath splits "a/X b/X" (both sides identical for a modification)
// in the middle so that a path containing spaces is kept whole; quoted
// sides are decoded. Renames (different sides) yield the new side when it
// can be identified, else "".
func headerPath(rest string) string {
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "\"") {
		// "a/X" "b/Y": take the last quoted token.
		if i := strings.LastIndex(rest, " \""); i >= 0 {
			return strings.TrimPrefix(unquoteGit(rest[i+1:]), "b/")
		}
		return ""
	}
	if len(rest)%2 == 1 {
		mid := len(rest) / 2
		if rest[mid] == ' ' && rest[:mid] == "a"+rest[mid+2:] {
			return strings.TrimPrefix(rest[mid+1:], "b/")
		}
	}
	// Different sides (rename) or unusual spacing: best effort on the last
	// "b/" token.
	if i := strings.LastIndex(rest, " b/"); i >= 0 {
		return rest[i+3:]
	}
	return ""
}

// unquoteGit decodes a Git-quoted path ("my\303\251.go") when quoted;
// unquoted input is returned unchanged.
func unquoteGit(s string) string {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return s
	}
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s[1 : len(s)-1]
}

// chunkDiff packs the diff into chunks of at most budget bytes. Whole
// files are kept together when they fit; larger files are split on hunk
// boundaries with the file header repeated once per chunk. A single hunk
// larger than the budget is reported in oversize and left out.
func chunkDiff(diff string, budget int) (chunks []string, oversize []oversizeHunk) {
	var cur strings.Builder
	curHeader := "" // header of the split file whose hunks are being appended
	flush := func() {
		if cur.Len() > 0 {
			chunks = append(chunks, cur.String())
			cur.Reset()
		}
		curHeader = ""
	}
	add := func(piece string) {
		if cur.Len() > 0 && cur.Len()+len(piece) > budget {
			flush()
		}
		cur.WriteString(piece)
	}

	for _, f := range splitFiles(diff) {
		if len(f) <= budget {
			add(f)
			curHeader = ""
			continue
		}
		header, hunks := splitHunks(f)
		if len(hunks) == 0 {
			oversize = append(oversize, oversizeHunk{File: filePath(f), Bytes: len(f)})
			continue
		}
		for _, h := range hunks {
			if len(header)+len(h) > budget {
				oversize = append(oversize, oversizeHunk{File: filePath(f), Bytes: len(header) + len(h)})
				continue
			}
			if curHeader == header && cur.Len()+len(h) <= budget {
				cur.WriteString(h) // same file continues in this chunk
				continue
			}
			add(header + h)
			curHeader = header
		}
	}
	flush()
	return chunks, oversize
}

// precedence orders verdicts for merging across chunks: a violation
// anywhere wins, then compliance, then not applicable.
func precedence(v ruleeval.Verdict) int {
	switch v {
	case ruleeval.Violated:
		return 3
	case ruleeval.Compliant:
		return 2
	case ruleeval.NotApplicable:
		return 1
	default:
		return 0
	}
}

// mergeVerdict folds v into dst[id] using precedence. Equal-precedence
// violations accumulate their evidence.
func mergeVerdict(dst map[string]*ruleeval.EvalResponse, id string, v *ruleeval.EvalResponse) {
	cur, ok := dst[id]
	if !ok {
		dst[id] = cloneResponse(v)
		return
	}
	switch {
	case precedence(v.Verdict) > precedence(cur.Verdict):
		dst[id] = cloneResponse(v)
	case precedence(v.Verdict) == precedence(cur.Verdict) && v.Verdict == ruleeval.Violated:
		cur.Evidence = append(cur.Evidence, v.Evidence...)
		if v.Confidence > cur.Confidence {
			cur.Confidence = v.Confidence
		}
		// A chunk whose detail request failed carries no explanation; a
		// later chunk's explanation fills it rather than being dropped.
		if cur.Explanation == "" {
			cur.Explanation = v.Explanation
		}
	}
}

func cloneResponse(v *ruleeval.EvalResponse) *ruleeval.EvalResponse {
	c := *v
	c.Evidence = append([]ruleeval.Location(nil), v.Evidence...)
	return &c
}
