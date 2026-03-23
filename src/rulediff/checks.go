package rulediff

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/entityops"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// ruleNumRe extracts the numeric suffix from a rule ID (e.g. "FOO-042" → "042").
var ruleNumRe = regexp.MustCompile(`-(\d{3})$`)

// appendOnlyNoteTypes are note types that must never be removed once added (RD012).
var appendOnlyNoteTypes = map[string]bool{
	"changelog":  true,
	"supersedes": true,
	"extends":    true,
	"related":    true,
}

// checkRules runs RD001–RD006, RD010–RD012 for one entity's rules and notes.
func checkRules(base, head *entity.File, res *result.Result, path string) {
	baseMap, baseNums := buildRuleIndex(base.Rules)
	headMap := buildRuleMap(head.Rules)

	// RD005: rule removal — rules must never be deleted.
	for id := range baseMap {
		if _, ok := headMap[id]; !ok {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD005",
				Severity: result.Error,
				Message:  "rule removed: transition to state A or T instead of deleting",
				Path:     path + "/" + id,
			})
		}
	}

	// Single pass: RD001-RD004, RD006, RD010-RD011 per rule.
	var newRuleNums []int
	for id, h := range headMap {
		if b, exists := baseMap[id]; exists {
			checkExistingRule(b, h, headMap, res, path+"/"+id)
		} else {
			checkNewRule(h, baseNums, res, path+"/"+id)
			if n := extractRuleNum(h.ID); n >= 0 {
				newRuleNums = append(newRuleNums, n)
			}
		}
	}

	// RD004: new rule numbers must be consecutive starting at max(base)+1.
	checkSequentialRuleNums(newRuleNums, maxRuleNum(baseNums)+1, res, path)

	// RD011 (new notes) and RD012 (append-only notes).
	checkNotes(base.Notes, head.Notes, res, path)
}

// checkExistingRule runs RD001, RD002, RD003, RD006 for a rule present in both versions.
func checkExistingRule(b, h entity.Rule, headMap map[string]entity.Rule, res *result.Result, path string) {
	// RD001: body edited in Draft — revision must increment, state must stay D.
	if b.State == entityops.StateDraft && h.Body != b.Body {
		if h.Revision != b.Revision+1 {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD001",
				Severity: result.Error,
				Message:  fmt.Sprintf("draft body edited but revision not incremented: expected %d, got %d", b.Revision+1, h.Revision),
				Path:     path,
			})
		}
		if h.State != entityops.StateDraft {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD001",
				Severity: result.Error,
				Message:  fmt.Sprintf("draft body edited but state changed to %q: body edits and state transitions must be separate", h.State),
				Path:     path,
			})
		}
	}

	// RD002: body is immutable once a rule has left Draft.
	if b.State != entityops.StateDraft && h.Body != b.Body {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD002",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule body is immutable in state %q: supersede the rule to change its body", b.State),
			Path:     path,
		})
	}

	// RD003: state transitions must follow the forward-only state machine.
	if h.State != b.State {
		if err := entityops.ValidateTransition(b.State, h.State); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD003",
				Severity: result.Error,
				Message:  "invalid state transition: " + err.Error(),
				Path:     path,
			})
		}
	}

	// RD006: when a superseding rule reaches I, the superseded rule should be R.
	// Spec §7.2 allows retirement to happen in a linked subsequent PR, so this is
	// a Warning rather than an Error (strict mode promotes it to Error).
	if h.Supersedes != nil && h.State == entityops.StateImplemented {
		supersededID := *h.Supersedes
		superseded, ok := headMap[supersededID]
		if !ok {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD006",
				Severity: result.Warning,
				Message:  fmt.Sprintf("superseding rule reached I but superseded rule %q not found in file", supersededID),
				Path:     path,
			})
		} else if superseded.State != entityops.StateRetired {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD006",
				Severity: result.Warning,
				Message:  fmt.Sprintf("superseding rule reached I but superseded rule %q has state %q (expected R; may be a linked follow-up PR)", supersededID, superseded.State),
				Path:     path,
			})
		}
	}
}

// checkNewRule runs RD004, RD010, RD011 for a rule that appears only in head.
func checkNewRule(h entity.Rule, baseNums map[int]bool, res *result.Result, path string) {
	checkNewRuleConstraints(h, res, path)

	// RD010: new rule numbers must not reuse any number from the base version.
	if num := extractRuleNum(h.ID); num >= 0 && baseNums[num] {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD010",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule number %03d was previously used and must not be reused", num),
			Path:     path,
		})
	}
}

// checkNewRuleConstraints runs RD004 and RD011 for any new rule (used by both
// checkNewRule and checkNewEntityTree).
func checkNewRuleConstraints(r entity.Rule, res *result.Result, path string) {
	if r.Revision != 0 {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD004",
			Severity: result.Error,
			Message:  fmt.Sprintf("new rule must have revision 0, got %d", r.Revision),
			Path:     path,
		})
	}
	if r.State != entityops.StateDraft {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD004",
			Severity: result.Error,
			Message:  fmt.Sprintf("new rule must have state D, got %q", r.State),
			Path:     path,
		})
	}
	if strings.TrimSpace(r.AddedBy) == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD011",
			Severity: result.Error,
			Message:  "new rule missing added_by",
			Path:     path,
		})
	}
}

// checkNotes runs RD012 (append-only note removal) and RD011 (new note without added_by).
func checkNotes(baseNotes, headNotes []entity.Note, res *result.Result, path string) {
	baseNoteSet := make(map[string]bool, len(baseNotes))
	for _, n := range baseNotes {
		baseNoteSet[n.ID] = true
	}
	// Map id → type so RD012 can verify the type was not changed away from append-only.
	headNoteTypes := make(map[string]string, len(headNotes))
	for _, n := range headNotes {
		headNoteTypes[n.ID] = n.Type
	}

	// RD012: append-only notes must not be removed or have their type changed.
	for _, n := range baseNotes {
		if !appendOnlyNoteTypes[n.Type] {
			continue
		}
		headType, ok := headNoteTypes[n.ID]
		if !ok || headType != n.Type {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD012",
				Severity: result.Error,
				Message:  fmt.Sprintf("append-only note %q (type %q) was removed or retyped", n.ID, n.Type),
				Path:     path + "/notes/" + n.ID,
			})
		}
	}

	// RD011: new notes must have added_by.
	for _, n := range headNotes {
		if !baseNoteSet[n.ID] && strings.TrimSpace(n.AddedBy) == "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD011",
				Severity: result.Error,
				Message:  "new note missing added_by",
				Path:     path + "/notes/" + n.ID,
			})
		}
	}
}

// checkVersion runs RD007: rule_set.version must be strictly higher.
func checkVersion(base, head *entity.File, res *result.Result, path string) {
	if !isNewerSemver(base.RuleSet.Version, head.RuleSet.Version) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD007",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule_set.version must be strictly higher than %q, got %q", base.RuleSet.Version, head.RuleSet.Version),
			Path:     path + "/rule_set.version",
		})
	}
}

// checkTimestamp runs RD009: rule_set.timestamp must be updated.
func checkTimestamp(base, head *entity.File, res *result.Result, path string) {
	if head.RuleSet.Timestamp == base.RuleSet.Timestamp {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD009",
			Severity: result.Error,
			Message:  "rule_set.timestamp must be updated",
			Path:     path + "/rule_set.timestamp",
		})
	}
}

// checkHashBytes runs RD008: rule_set.hash must match the recomputed blake3 hash.
// A nil hash field is skipped (hash is optional).
func checkHashBytes(head *entity.File, headData []byte, res *result.Result, path string) {
	if head.RuleSet.Hash == nil {
		return
	}
	computed, err := entity.ComputeHash(headData)
	if err != nil {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD008",
			Severity: result.Error,
			Message:  "failed to compute hash: " + err.Error(),
			Path:     path + "/rule_set.hash",
		})
		return
	}
	if computed != *head.RuleSet.Hash {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD008",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule_set.hash mismatch: computed %q, file has %q", computed, *head.RuleSet.Hash),
			Path:     path + "/rule_set.hash",
		})
	}
}

// checkNewEntityTree validates all rules and notes in a new entity (no prior version).
// Used by DiffNew and for sub-entities added for the first time.
func checkNewEntityTree(head *entity.File, res *result.Result, path string) {
	var nums []int
	for _, r := range head.Rules {
		checkNewRuleConstraints(r, res, path+"/"+r.ID)
		if n := extractRuleNum(r.ID); n >= 0 {
			nums = append(nums, n)
		}
	}
	// RD004: rules in a new entity must start at 1 and be consecutive.
	checkSequentialRuleNums(nums, 1, res, path)

	for _, n := range head.Notes {
		if strings.TrimSpace(n.AddedBy) == "" {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD011",
				Severity: result.Error,
				Message:  "new note missing added_by",
				Path:     path + "/notes/" + n.ID,
			})
		}
	}
	for i := range head.SubEntities {
		sub := &head.SubEntities[i]
		checkNewEntityTree(sub, res, path+" > "+sub.Entity.ID)
	}
}

// buildRuleIndex returns both a map of rules by ID and a set of rule numbers
// in a single pass over the slice.
func buildRuleIndex(rules []entity.Rule) (map[string]entity.Rule, map[int]bool) {
	byID := make(map[string]entity.Rule, len(rules))
	nums := make(map[int]bool, len(rules))
	for _, r := range rules {
		byID[r.ID] = r
		if n := extractRuleNum(r.ID); n >= 0 {
			nums[n] = true
		}
	}
	return byID, nums
}

// buildRuleMap indexes rules by ID for O(1) lookup.
func buildRuleMap(rules []entity.Rule) map[string]entity.Rule {
	m := make(map[string]entity.Rule, len(rules))
	for _, r := range rules {
		m[r.ID] = r
	}
	return m
}

// extractRuleNum returns the numeric suffix of a rule ID (e.g. "FOO-042" → 42), or -1.
func extractRuleNum(id string) int {
	m := ruleNumRe.FindStringSubmatch(id)
	if len(m) < 2 {
		return -1
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return -1
	}
	return n
}

// maxRuleNum returns the highest number in the set, or 0 if empty.
func maxRuleNum(nums map[int]bool) int {
	max := 0
	for n := range nums {
		if n > max {
			max = n
		}
	}
	return max
}

// checkSequentialRuleNums emits RD004 if nums (sorted ascending) do not form
// a consecutive sequence starting at nextExpected. No-op if nums is empty.
func checkSequentialRuleNums(nums []int, nextExpected int, res *result.Result, path string) {
	if len(nums) == 0 {
		return
	}
	sort.Ints(nums)
	for i, n := range nums {
		if n != nextExpected+i {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD004",
				Severity: result.Error,
				Message:  fmt.Sprintf("new rule number %03d is not sequential: expected %03d", n, nextExpected+i),
				Path:     path,
			})
		}
	}
}
