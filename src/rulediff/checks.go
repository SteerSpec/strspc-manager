package rulediff

import (
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

var (
	// ruleNumRe extracts the numeric suffix from a rule ID (e.g. "FOO-042" → "042").
	ruleNumRe = regexp.MustCompile(`-(\d{3})$`)

	// handleRe matches a GitHub @handle, a bot's included: hyphens only between
	// alphanumerics.
	handleRe = regexp.MustCompile(`^@[A-Za-z0-9]+(-[A-Za-z0-9]+)*(\[bot\])?$`)

	// domainRe matches a domain of at least two labels of up to 63 characters,
	// each without a leading or trailing hyphen. net/mail does not check this.
	domainRe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+$`)
)

// maxDomainLen is the longest domain name DNS allows.
const maxDomainLen = 253

// checkRules runs RD001–RD006, RD010–RD016, RD018, RD019, RD021 and RD022 for one entity's rules and notes.
// RD017 (entity inherits) is run by checkInherits.
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

	// RD011–RD016: notes.
	checkNotes(base.Notes, head.Notes, res, path)
}

// checkExistingRule runs RD001, RD002, RD003, RD006 for a rule present in both versions.
func checkExistingRule(b, h entity.Rule, headMap map[string]entity.Rule, res *result.Result, path string) {
	// RD001: body edited in Draft — revision must increment, state must stay D.
	if b.State == entity.StateDraft && h.Body != b.Body {
		if h.Revision != b.Revision+1 {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD001",
				Severity: result.Error,
				Message:  fmt.Sprintf("draft body edited but revision not incremented: expected %d, got %d", b.Revision+1, h.Revision),
				Path:     path,
			})
		}
		if h.State != entity.StateDraft {
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
	if b.State != entity.StateDraft && h.Body != b.Body {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD002",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule body is immutable in state %q: supersede the rule to change its body", b.State),
			Path:     path,
		})
	}

	// RD018: overrides is immutable once a rule has left Draft (RUL-007);
	// adding, removing or retargeting it would change which inherited rule
	// the entity enforces without a new rule.
	if b.State != entity.StateDraft && !sameRef(b.Overrides, h.Overrides) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD018",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule overrides is immutable in state %q: changed from %s to %s; supersede the rule to change it", b.State, refString(b.Overrides), refString(h.Overrides)),
			Path:     path,
		})
	}

	// RD021: a rule's revision counts edits to the rule, so nothing else may
	// move it, a mapping to a standard above all (NTE-018).
	if h.Revision != b.Revision && h.Body == b.Body && !isDraftReferenceEdit(b, h) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD021",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule revision changed from %d to %d with no change to the rule", b.Revision, h.Revision),
			Path:     path,
		})
	}

	// RD003: state transitions must follow the forward-only state machine.
	if h.State != b.State {
		if err := entity.ValidateTransition(b.State, h.State); err != nil {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD003",
				Severity: result.Error,
				Message:  "invalid state transition: " + err.Error(),
				Path:     path,
			})
		}
	}

	// RD006: when a superseding rule *transitions to* I, the superseded rule should be R.
	// Scoped to the transition (b.State != I && h.State == I) to avoid false positives
	// in later PRs where the superseder is already I and unrelated changes are made.
	// Spec §7.2 allows retirement in a linked subsequent PR, so severity is Warning.
	if h.Supersedes != nil && b.State != entity.StateImplemented && h.State == entity.StateImplemented {
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
		} else if superseded.State != entity.StateRetired {
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

// isDraftReferenceEdit reports whether h is one edit of the Draft rule b's
// `overrides` or `supersedes`, the only other edits that move a revision.
func isDraftReferenceEdit(b, h entity.Rule) bool {
	changed := !sameRef(b.Overrides, h.Overrides) || !sameRef(b.Supersedes, h.Supersedes)
	return b.State == entity.StateDraft && changed && h.Revision == b.Revision+1
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

// checkNewRuleConstraints runs RD004, RD011 and RD019 for any new rule (used by both
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
	if r.State != entity.StateDraft {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD004",
			Severity: result.Error,
			Message:  fmt.Sprintf("new rule must have state D, got %q", r.State),
			Path:     path,
		})
	}
	checkAddedBy("rule", r.AddedBy, res, path)
}

// checkAddedBy runs RD011 and RD019 for the added_by of a new rule or note.
func checkAddedBy(kind, addedBy string, res *result.Result, path string) {
	if strings.TrimSpace(addedBy) == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD011",
			Severity: result.Error,
			Message:  fmt.Sprintf("new %s missing added_by", kind),
			Path:     path,
		})
		return
	}
	if !validAddedBy(addedBy) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD019",
			Severity: result.Error,
			Message:  fmt.Sprintf("new %s has invalid added_by %q: must be an email address or @handle", kind, addedBy),
			Path:     path,
		})
	}
}

// validAddedBy reports whether s is a GitHub @handle or a bare email address.
// A display name or angle brackets are not bare.
func validAddedBy(s string) bool {
	if handleRe.MatchString(s) {
		return true
	}
	addr, err := mail.ParseAddress(s)
	if err != nil || addr.Address != s {
		return false
	}
	domain := s[strings.LastIndex(s, "@")+1:]
	return len(domain) <= maxDomainLen && domainRe.MatchString(domain)
}

// checkNotes runs RD011–RD016 for the notes of one entity: notes are never
// removed (NTE-009), append-only notes never change (NTE-010), and every other
// change to a note's content or type increments its revision by one, which
// starts at 0 (NTE-008).
func checkNotes(baseNotes, headNotes []entity.Note, res *result.Result, path string) {
	headByID := make(map[string]entity.Note, len(headNotes))
	for _, n := range headNotes {
		headByID[n.ID] = n
	}

	for _, b := range baseNotes {
		notePath := path + "/notes/" + b.ID
		h, ok := headByID[b.ID]
		appendOnly := entity.IsAppendOnlyNoteType(b.Type)

		switch {
		case appendOnly && (!ok || h.Type != b.Type):
			// RD012: append-only notes must not be removed or retyped.
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD012",
				Severity: result.Error,
				Message:  fmt.Sprintf("append-only note %q (type %q) was removed or retyped", b.ID, b.Type),
				Path:     notePath,
			})
		case !ok:
			// RD014: no note is ever removed; append-only removal is RD012.
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD014",
				Severity: result.Error,
				Message:  fmt.Sprintf("note %q (type %q) removed: revise it in place instead of deleting", b.ID, b.Type),
				Path:     notePath,
			})
		case appendOnly && h.Content != b.Content:
			// RD016: append-only notes are immutable once added.
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RD016",
				Severity: result.Error,
				Message:  fmt.Sprintf("content of append-only note %q (type %q) changed: add a new note instead", b.ID, b.Type),
				Path:     notePath,
			})
		default:
			checkNoteRevision(b, h, res, notePath)
			checkStandardRetype(b, h, res, notePath)
		}
	}

	baseNoteSet := make(map[string]bool, len(baseNotes))
	for _, n := range baseNotes {
		baseNoteSet[n.ID] = true
	}
	for _, n := range headNotes {
		if !baseNoteSet[n.ID] {
			checkNewNote(n, res, path+"/notes/"+n.ID)
		}
	}
}

// checkStandardRetype runs RD022: a `standard` note is retyped only to or from
// `reference` (NTE-017), and a withdrawal keeps its content (NTE-019).
func checkStandardRetype(b, h entity.Note, res *result.Result, path string) {
	if b.Type == h.Type || (b.Type != entity.NoteTypeStandard && h.Type != entity.NoteTypeStandard) {
		return
	}
	var msg string
	switch {
	case b.Type != entity.NoteTypeReference && h.Type != entity.NoteTypeReference:
		msg = fmt.Sprintf("note retyped from %q to %q: a `standard` note changes only to or from `reference`", b.Type, h.Type)
	case h.Type == entity.NoteTypeReference && h.Content != b.Content:
		msg = "withdrawing a mapping keeps the note's content"
	default:
		return
	}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RD022",
		Severity: result.Error,
		Message:  msg,
		Path:     path,
	})
}

// checkNoteRevision runs RD013 for a note present in both versions: a change
// to its content or type must increment its revision by exactly one, and its
// revision must not move without such a change.
func checkNoteRevision(b, h entity.Note, res *result.Result, path string) {
	changed := h.Content != b.Content || h.Type != b.Type
	var msg string
	switch {
	case changed && h.Revision != b.Revision+1:
		msg = fmt.Sprintf("note content or type changed but revision not incremented: expected %d, got %d", b.Revision+1, h.Revision)
	case !changed && h.Revision != b.Revision:
		msg = fmt.Sprintf("note revision changed from %d to %d without a content or type change", b.Revision, h.Revision)
	default:
		return
	}
	res.Add(result.Diagnostic{
		Module:   module,
		Code:     "RD013",
		Severity: result.Error,
		Message:  msg,
		Path:     path,
	})
}

// checkNewNote runs RD011, RD015 and RD019 for a note that appears only in head.
func checkNewNote(n entity.Note, res *result.Result, path string) {
	checkAddedBy("note", n.AddedBy, res, path)
	if n.Revision != 0 {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD015",
			Severity: result.Error,
			Message:  fmt.Sprintf("new note must have revision 0, got %d", n.Revision),
			Path:     path,
		})
	}
}

// checkVersion runs RD007: rule_set.version must be strictly higher.
func checkVersion(base, head *entity.File, res *result.Result, path string) {
	if !isNewerSemver(base.RuleSet.Version, head.RuleSet.Version) {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD007",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule_set.version must be a valid semver and strictly higher than %q, got %q", base.RuleSet.Version, head.RuleSet.Version),
			Path:     path + "/rule_set.version",
		})
	}
}

// sameRef reports whether two optional rule references are equal.
func sameRef(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// refString renders an optional rule reference for a diagnostic message.
func refString(r *string) string {
	if r == nil {
		return "none"
	}
	return strconv.Quote(*r)
}

// checkInherits runs RD017: once an entity has a rule beyond Draft, its
// `inherits` value may not change or be removed, since re-parenting changes
// the inherited rules its published rules were written against. Adding
// `inherits` where there was none only adds rules and is allowed. So is
// removing it once no rule is Published or Implemented: nothing relies on the
// parent, and a consumer may have to leave a parent narrowed to `private`.
func checkInherits(base, head *entity.File, res *result.Result, path string) {
	if base.Entity.Inherits == "" || head.Entity.Inherits == base.Entity.Inherits {
		return
	}
	removed := head.Entity.Inherits == ""
	for _, r := range base.Rules {
		if r.State == entity.StateDraft {
			continue
		}
		if removed && r.State != entity.StatePublished && r.State != entity.StateImplemented {
			continue
		}
		msg := fmt.Sprintf("inherits changed from %q to %q", base.Entity.Inherits, head.Entity.Inherits)
		if removed {
			msg = fmt.Sprintf("inherits %q removed", base.Entity.Inherits)
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD017",
			Severity: result.Error,
			Message:  msg + fmt.Sprintf(" while rule %s is in state %q: create a new entity instead", r.ID, r.State),
			Path:     path + "/entity.inherits",
		})
		return
	}
}

// checkVisibility runs RD020 on any change to the `visibility` of an entity.
// Removing the key is an Error, because it also catches a tool that drops the
// field and so silently widens the entity; the fix is to set `public`
// explicitly. Any other change is Info, so strict mode doesn't block it, and
// says which version bump it needs. baseContainer and headContainer are the
// effective visibilities of the entity containing this one.
func checkVisibility(base, head *entity.File, baseContainer, headContainer string, res *result.Result, path string) {
	from, to := base.Entity.Visibility, head.Entity.Visibility
	if from == to {
		return
	}
	path += "/entity.visibility"
	if to == "" {
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RD020",
			Severity: result.Error,
			Message:  fmt.Sprintf("visibility `%s` removed: set `public` explicitly if the entity is meant to be public", from),
			Path:     path,
		})
		return
	}
	before, after := entity.EffectiveVisibility(from, baseContainer), entity.EffectiveVisibility(to, headContainer)
	msg := fmt.Sprintf("visibility changed from `%s` to `%s`", from, to)
	if from == "" {
		msg = fmt.Sprintf("visibility changed from absent to `%s`", to)
	}
	switch {
	case entity.VisibilityWider(before, after):
		msg += ": narrowing needs a MAJOR bump of realm.version"
	case entity.VisibilityWider(after, before):
		msg += ": widening needs a MINOR bump of realm.version"
	}
	res.Add(result.Diagnostic{Module: module, Code: "RD020", Severity: result.Info, Message: msg, Path: path})
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
		checkNewNote(n, res, path+"/notes/"+n.ID)
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
