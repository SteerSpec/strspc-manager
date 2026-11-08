package claude

import (
	"fmt"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// mode is what tells a diff evaluation from an audit of a file set: the
// system prompt, the detail request and the name of a part in errors.
type mode struct {
	audit        bool
	system       string
	detailSchema func() map[string]any
	instruction  func(ruleID string, v ruleeval.Verdict) string
	part         string
}

// modeFor returns the mode of req: an audit when it carries files.
func modeFor(req ruleeval.BatchRequest) mode {
	if req.Files != nil {
		return mode{audit: true, system: prompt.AuditSystem(), detailSchema: prompt.AuditDetailSchema,
			instruction: prompt.AuditDetailInstruction, part: "part of the file set"}
	}
	return mode{system: prompt.System(), detailSchema: prompt.DetailSchema,
		instruction: prompt.DetailInstructionFor, part: "diff chunk"}
}

// planDiff splits diff into chunks that fit budgetTokens and returns them
// with the function that renders a chunk as its diff block, and the
// recoverable errors: a file list too long to give whole, hunks too large
// to send.
func planDiff(diff string, budgetTokens int) (chunks []string, block func(string) string, errs []error) {
	chunks, oversize := chunkDiff(diff, bytesForTokens(budgetTokens))
	block = prompt.DiffBlock
	if len(chunks) > 1 || len(oversize) > 0 {
		files, unlisted := capFiles(changedFiles(diff), bytesForTokens(budgetTokens)/fileListShare)
		if unlisted > 0 {
			// Without the full list a rule spanning files cannot be judged on
			// what a part leaves out, so the result must not pass as complete.
			errs = append(errs, fmt.Errorf(
				"diff too large to list every file: %d of %d files left off, so rules spanning files were judged on less",
				unlisted, len(files)+unlisted))
		}
		block = func(chunk string) string { return prompt.PartialDiffBlock(chunk, files, unlisted) }
		budgetTokens -= estimateTokens(block("")) - estimateTokens(prompt.DiffBlock(""))
		chunks, oversize = chunkDiff(diff, bytesForTokens(budgetTokens))
	}
	for _, o := range oversize {
		errs = append(errs, fmt.Errorf(
			"hunk in %s (%d bytes, ~%d tokens) exceeds the input budget of ~%d tokens and was not evaluated",
			o.File, o.Bytes, estimateTokensForBytes(o.Bytes), budgetTokens))
	}
	return chunks, block, errs
}

// planAudit splits files into parts of rendered FileSlices that fit
// budgetTokens, and returns them with the function that renders a part as
// its files block, what no part shows (lines too large to send) and the
// recoverable errors that report it. A set that needs more than one part is
// audited in parts (auditInParts), where a part collects findings and
// judges nothing, so it is shown alone, without the list of the whole set.
func planAudit(files []ruleeval.File, budgetTokens int) (parts []string, block func(string) string, unread []prompt.Unread, errs []error) {
	parts, oversize := chunkFiles(files, bytesForTokens(budgetTokens))
	for _, o := range oversize {
		unread = append(unread, prompt.Unread{File: o.File, Line: o.Line})
		what := fmt.Sprintf("line %d of %s", o.Line, o.File)
		if o.Line == 0 {
			what = "file " + o.File
		}
		errs = append(errs, fmt.Errorf(
			"%s (%d bytes, ~%d tokens) exceeds the input budget of ~%d tokens and was not audited",
			what, o.Bytes, estimateTokensForBytes(o.Bytes), budgetTokens))
	}
	return parts, prompt.FilesBlock, unread, errs
}

// filePaths returns the path of every file, in order.
func filePaths(files []ruleeval.File) []string {
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.Path
	}
	return paths
}

// oversizeLine describes a line of an audited file that cannot fit in a
// single request, or with Line 0 a file whose path alone cannot.
type oversizeLine struct {
	// File is the path of the file.
	File string
	// Line is the line number, from 1; 0 for the whole file.
	Line int
	// Bytes is the size of the line rendered in its block.
	Bytes int
}

// splitLines splits content into lines; a final newline ends the last line
// rather than opening an empty one.
func splitLines(content string) []string {
	if content == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(content, "\n"), "\n")
}

// chunkFiles packs files, in order, into parts of at most budget bytes of
// rendered FileSlices. A file is kept whole when it fits; a larger one is
// split into line ranges, each a block of its own. A line too large for a
// part on its own is reported in oversize and left out.
func chunkFiles(files []ruleeval.File, budget int) (parts []string, oversize []oversizeLine) {
	parts, _, oversize = chunkFileParts(files, budget)
	return parts, oversize
}

// chunkFileParts is chunkFiles that also returns, for each part, the paths
// of the files it shows, whole or in part, in order.
func chunkFileParts(files []ruleeval.File, budget int) (parts []string, shown [][]string, oversize []oversizeLine) {
	var (
		cur      strings.Builder
		curPaths []string
	)
	add := func(path, piece string) {
		if cur.Len() > 0 && cur.Len()+len(piece) > budget {
			parts = append(parts, cur.String())
			shown = append(shown, curPaths)
			cur.Reset()
			curPaths = nil
		}
		cur.WriteString(piece)
		if len(curPaths) == 0 || curPaths[len(curPaths)-1] != path {
			curPaths = append(curPaths, path)
		}
	}
	for _, f := range files {
		lines := splitLines(f.Content)
		total := len(lines)
		if whole := prompt.FileSlice(f.Path, 1, total, lines); len(whole) <= budget {
			add(f.Path, whole)
			continue
		}
		// The widest tag a slice of this file can have, and its closing tag.
		frame := len(prompt.FileSliceHeader(f.Path, total, total, total)) + len("</file>\n")
		if frame >= budget {
			oversize = append(oversize, oversizeLine{File: f.Path, Bytes: frame})
			continue
		}
		for start := 0; start < total; {
			size, end := frame, start
			for end < total && size+prompt.FileLineBytes(lines[end], total) <= budget {
				size += prompt.FileLineBytes(lines[end], total)
				end++
			}
			if end == start {
				oversize = append(oversize, oversizeLine{File: f.Path, Line: start + 1, Bytes: frame + prompt.FileLineBytes(lines[start], total)})
				start++
				continue
			}
			add(f.Path, prompt.FileSlice(f.Path, start+1, total, lines[start:end]))
			start = end
		}
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
		shown = append(shown, curPaths)
	}
	return parts, shown, oversize
}
