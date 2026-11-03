// Package rulehistory derives the change history of an entity file from the
// successive versions of that file (Rule Manager Spec §6.2, issue #207).
//
// The package reads no version-control system itself. A caller, such as the
// CLI walking `git log` or a service reading the GitHub API, supplies one
// Commit per change with the file content before and after it, and Build
// classifies what happened to each rule. Nothing is stored in the entity file.
package rulehistory

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// Kind classifies one change to one rule.
type Kind string

// Change kinds, in the order Build emits them for a single rule in a single
// commit.
const (
	// Added marks a rule that first appears in the commit.
	Added Kind = "added"
	// BodyChanged marks a change to the normative text of a rule.
	BodyChanged Kind = "body_changed"
	// Promoted marks a move from Draft to Published or from Published to
	// Implemented.
	Promoted Kind = "promoted"
	// Retired marks a move from Implemented to Retired.
	Retired Kind = "retired"
	// Terminated marks a move from Retired to Terminated.
	Terminated Kind = "terminated"
	// Abandoned marks a move from Draft to Abandoned.
	Abandoned Kind = "abandoned"
	// StateChanged marks any other state change, such as D→I or a move back,
	// which the lifecycle forbids but an old commit may still hold.
	StateChanged Kind = "state_changed"
	// MetadataChanged marks a change to a rule that touches neither its body
	// nor its state, such as added_by or a revision bump alone.
	MetadataChanged Kind = "metadata_changed"
	// Superseded marks a rule that a rule added in the same file in the
	// commit supersedes.
	Superseded Kind = "superseded"
	// Removed marks a rule that disappears from the file. RD005 forbids it,
	// but older history may hold it.
	Removed Kind = "removed"
)

// Commit is one change to an entity file. Before is the file at the
// commit's first parent and After the file at the commit; a nil Before means
// the commit added the file, and a nil After means it deleted it. Pairing
// each commit with its own parent, rather than with the previous commit in
// the list, keeps the history right when branches interleave.
type Commit struct {
	// SHA identifies the commit.
	SHA string
	// Date is the commit date.
	Date time.Time
	// Subject is the first line of the commit message.
	Subject string
	// Before is the file content at the first parent, or nil.
	Before []byte
	// After is the file content at the commit, or nil.
	After []byte
}

// Event is one change to one rule.
type Event struct {
	// Entity is the EUID of the entity holding the rule, with its containing
	// entities first for a sub-entity ("ENT > SUB").
	Entity string `json:"entity"`
	// RuleID identifies the rule.
	RuleID string `json:"rule_id"`
	// Kind classifies the change.
	Kind Kind `json:"kind"`
	// FromState is the state before the change, set only on a change of
	// state and on a removed rule.
	FromState string `json:"from_state,omitempty"`
	// ToState is the state of the rule after the commit, empty for a
	// removed rule.
	ToState string `json:"to_state,omitempty"`
	// Revision is the rule revision after the change (before it, for a
	// removed rule).
	Revision int `json:"revision"`
	// RuleSetVersion is the rule_set.version of the entity owning the rule
	// (a sub-entity has its own) after the commit, or before it for a
	// removed rule.
	RuleSetVersion string `json:"rule_set_version"`
	// Supersedes is the rule this rule supersedes, set when the commit sets
	// that reference: on an added rule, or on a Draft that adds or changes it.
	Supersedes string `json:"supersedes,omitempty"`
	// SupersededBy is the rule that supersedes a superseded rule.
	SupersededBy string `json:"superseded_by,omitempty"`
	// Commit is the commit SHA.
	Commit string `json:"commit"`
	// Date is the commit date in RFC 3339 form.
	Date string `json:"date"`
	// PR is the pull request number read from the commit subject, or 0.
	PR int `json:"pr,omitempty"`
	// Subject is the first line of the commit message.
	Subject string `json:"subject"`
}

// Warning reports a commit, or a version, that Build or Filter could not
// fully account for.
type Warning struct {
	// Commit is the commit SHA, empty when the warning is not about one.
	Commit string `json:"commit,omitempty"`
	// Message describes the problem.
	Message string `json:"message"`
}

// History is the change history of one entity file.
type History struct {
	// Events lists the changes, oldest commit first. Within a commit they
	// follow the rule order of the file, then the order of the Kind
	// constants; removed rules come last.
	Events []Event `json:"events"`
	// Warnings lists what could not be classified.
	Warnings []Warning `json:"warnings,omitempty"`
}

// Build classifies the changes of commits, given oldest first.
//
// A Before that does not parse is replaced by the last version that did,
// with a warning. A commit whose After does not parse is skipped, with a
// warning, and its Before, when it parses, becomes the last version known.
func Build(commits []Commit) *History {
	h := &History{Events: []Event{}}
	var lastGood *entity.File
	for _, c := range commits {
		var before *entity.File
		var beforeErr error
		if c.Before != nil {
			before, beforeErr = entity.Parse(c.Before)
		}
		var after *entity.File
		if c.After != nil {
			f, err := entity.Parse(c.After)
			if err != nil {
				h.Warnings = append(h.Warnings, Warning{Commit: c.SHA, Message: "skipped: the file does not parse after the commit: " + err.Error()})
				if before != nil {
					// The version before the commit is still the last
					// one known; a later commit may need it.
					lastGood = before
				}
				continue
			}
			after = f
		}
		if beforeErr != nil {
			h.Warnings = append(h.Warnings, Warning{Commit: c.SHA, Message: "the file does not parse before the commit; compared with the last version that did: " + beforeErr.Error()})
			before = lastGood
		}
		h.Events = append(h.Events, classify(c, before, after)...)
		if after != nil {
			lastGood = after
		}
	}
	return h
}

// ruleAt is a rule with the entity path holding it and that entity's rule
// set version.
type ruleAt struct {
	entity  string
	version string
	rule    entity.Rule
}

// flatten lists the rules of f and its sub-entities, depth first in file
// order.
func flatten(f *entity.File) []ruleAt {
	if f == nil {
		return nil
	}
	var out []ruleAt
	var walk func(f *entity.File, path string)
	walk = func(f *entity.File, path string) {
		for _, r := range f.Rules {
			out = append(out, ruleAt{entity: path, version: f.RuleSet.Version, rule: r})
		}
		for i := range f.SubEntities {
			sub := &f.SubEntities[i]
			walk(sub, path+" > "+sub.Entity.ID)
		}
	}
	walk(f, f.Entity.ID)
	return out
}

// classify returns the events of one commit.
func classify(c Commit, before, after *entity.File) []Event {
	base := Event{Commit: c.SHA, Date: c.Date.UTC().Format(time.RFC3339), PR: PRNumber(c.Subject), Subject: c.Subject}
	beforeRules := flatten(before)
	afterRules := flatten(after)
	old := make(map[string]entity.Rule, len(beforeRules))
	for _, r := range beforeRules {
		old[r.rule.ID] = r.rule
	}
	present := make(map[string]bool, len(afterRules))
	supersededBy := map[string]string{}
	for _, r := range afterRules {
		present[r.rule.ID] = true
		if ref := newSupersedes(old, r.rule); ref != "" {
			if _, ok := old[ref]; ok {
				supersededBy[ref] = r.rule.ID
			}
		}
	}

	var events []Event
	for _, r := range afterRules {
		ev := base
		ev.Entity, ev.RuleSetVersion, ev.RuleID, ev.Revision, ev.ToState = r.entity, r.version, r.rule.ID, r.rule.Revision, r.rule.State
		ev.Supersedes = newSupersedes(old, r.rule)
		prev, existed := old[r.rule.ID]
		if !existed {
			ev.Kind = Added
			events = append(events, ev)
			continue
		}
		changed := false
		if prev.Body != r.rule.Body {
			e := ev
			e.Kind = BodyChanged
			events = append(events, e)
			changed = true
		}
		if prev.State != r.rule.State {
			e := ev
			e.Kind = stateKind(prev.State, r.rule.State)
			e.FromState = prev.State
			events = append(events, e)
			changed = true
		}
		if !changed && !sameRule(prev, r.rule) {
			e := ev
			e.Kind = MetadataChanged
			events = append(events, e)
		}
		if by, ok := supersededBy[r.rule.ID]; ok {
			e := ev
			e.Kind = Superseded
			e.SupersededBy = by
			events = append(events, e)
		}
	}
	for _, r := range beforeRules {
		if present[r.rule.ID] {
			continue
		}
		ev := base
		ev.Entity, ev.RuleSetVersion, ev.RuleID, ev.Kind, ev.FromState, ev.Revision = r.entity, r.version, r.rule.ID, Removed, r.rule.State, r.rule.Revision
		events = append(events, ev)
	}
	return events
}

// newSupersedes returns the rule that r supersedes when the commit sets that
// reference: r is new, or a Draft that adds or changes its supersedes (RD021).
// It returns "" otherwise.
func newSupersedes(old map[string]entity.Rule, r entity.Rule) string {
	if r.Supersedes == nil {
		return ""
	}
	if prev, existed := old[r.ID]; existed && sameRef(prev.Supersedes, r.Supersedes) {
		return ""
	}
	return *r.Supersedes
}

// stateKind names a move from one state to another. Only the lifecycle's
// own transitions get a name; any other move is StateChanged.
func stateKind(from, to string) Kind {
	switch from + to {
	case entity.StateDraft + entity.StatePublished, entity.StatePublished + entity.StateImplemented:
		return Promoted
	case entity.StateImplemented + entity.StateRetired:
		return Retired
	case entity.StateRetired + entity.StateTerminated:
		return Terminated
	case entity.StateDraft + entity.StateAbandoned:
		return Abandoned
	default:
		return StateChanged
	}
}

// sameRule reports whether two versions of a rule hold the same values.
func sameRule(a, b entity.Rule) bool {
	return a.ID == b.ID && a.Revision == b.Revision && a.State == b.State && a.Body == b.Body &&
		a.AddedBy == b.AddedBy && a.AddedAt == b.AddedAt &&
		sameRef(a.Supersedes, b.Supersedes) && sameRef(a.Overrides, b.Overrides)
}

func sameRef(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

var (
	prSuffixRe = regexp.MustCompile(`\(#(\d+)\)`)
	prMergeRe  = regexp.MustCompile(`^Merge pull request #(\d+)\b`)
)

// PRNumber reads the pull request number from a commit subject: the last
// "(#N)", as a squash merge writes it, or "Merge pull request #N" as a merge
// commit does. It returns 0 when there is none.
func PRNumber(subject string) int {
	if m := prSuffixRe.FindAllStringSubmatch(subject, -1); len(m) > 0 {
		return atoi(m[len(m)-1][1])
	}
	if m := prMergeRe.FindStringSubmatch(subject); m != nil {
		return atoi(m[1])
	}
	return 0
}

// atoi parses a run of digits, or returns 0 when it overflows an int.
func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// String renders a warning for humans.
func (w Warning) String() string {
	if w.Commit == "" {
		return w.Message
	}
	return fmt.Sprintf("%s: %s", w.Commit, w.Message)
}
