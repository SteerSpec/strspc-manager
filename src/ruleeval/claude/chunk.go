package claude

import (
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// estimateTokens approximates the token count of s as bytes/3, a
// deliberately conservative figure for code and diffs.
func estimateTokens(s string) int {
	return (len(s) + 2) / 3
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

// filePath extracts the new-side path from a file diff header.
func filePath(file string) string {
	for _, line := range strings.SplitAfter(file, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				return strings.TrimPrefix(fields[3], "b/")
			}
		case strings.HasPrefix(line, "+++ "):
			p := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			return strings.TrimPrefix(p, "b/")
		}
	}
	return "<unknown>"
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
	}
}

func cloneResponse(v *ruleeval.EvalResponse) *ruleeval.EvalResponse {
	c := *v
	c.Evidence = append([]ruleeval.Location(nil), v.Evidence...)
	return &c
}
