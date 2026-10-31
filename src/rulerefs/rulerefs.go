// Package rulerefs validates the rule identifiers that text files cite, such as
// agent prompts, READMEs and CI config, against a set of known rules. It
// reports citations that are unknown or no longer live (RF001–RF006). It is
// deterministic and reads no files: the caller chooses them and passes their
// content to Checker.CheckText.
package rulerefs

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/render"
	"github.com/SteerSpec/strspc-manager/src/result"
)

const module = "rule-refs"

// ignoreMarker exempts the line that holds it and the next line. Any comment
// syntax can carry it, such as an HTML comment in markdown.
const ignoreMarker = "strspc-refs: ignore"

var (
	// citationRe finds the stored form of a rule identifier, which is a
	// substring of the rendered one under the default format, optionally
	// qualified by a realm. Group 1 is the realm, group 2 the EUID and group 3
	// the rule number.
	citationRe = regexp.MustCompile(`\b(?:([a-z][a-z0-9]*(?:\.[a-z][a-z0-9]*)*):)?([A-Za-z0-9]{3,18})-(\d{3})\b`)

	// urlRe finds URLs, whose path segments are not citations.
	urlRe = regexp.MustCompile(`[a-z][a-z0-9+.-]*://\S+`)

	// reportableEUIDRe selects the unknown prefixes worth reporting. EUIDs in
	// practice are uppercase, while a lowercase or numeric shape is usually a
	// branch name or a range, such as feat-123 or 100-200.
	reportableEUIDRe = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,17}$`)
)

// Source is the set of rules of one Realm, or of an entity source with no
// manifest.
type Source struct {
	// RealmID is the Realm's realm_id; citations qualified with it resolve
	// only here. Empty when the source has no manifest.
	RealmID string
	// Format is the Realm's rule_identifier_format; nil keeps the RLIFRMT
	// defaults. For a sub-realm, pass the parent Realm's format (RLM-015).
	Format *entity.RuleIdentifierFormat
	// Entities holds the parsed entity files; their sub-entities are included.
	Entities []*entity.File
}

// Config holds options for the Checker.
type Config struct {
	// Strict treats warnings as errors.
	Strict bool
	// AllowedStates lists the rule states whose citations are accepted.
	AllowedStates []string
	// Ignore lists exact identifiers that are not citations, such as SHA-256.
	Ignore []string
}

// Option configures the Checker.
type Option func(*Config)

// WithStrict causes warnings to be reported as errors.
func WithStrict(b bool) Option {
	return func(c *Config) { c.Strict = b }
}

// WithAllowedStates accepts citations of rules in the given states.
func WithAllowedStates(states ...string) Option {
	return func(c *Config) { c.AllowedStates = append(c.AllowedStates, states...) }
}

// WithIgnore names identifiers that are not citations, such as SHA-256.
func WithIgnore(ids ...string) Option {
	return func(c *Config) { c.Ignore = append(c.Ignore, ids...) }
}

// citedRule is what a citation is judged against.
type citedRule struct {
	state          string
	successor      string // the rule that replaces this one, if any
	successorState string
}

// form is a Realm's rendered identifier shape, when it differs from the stored
// one. It matches only that Realm's EUIDs: shapes such as MAX_100 and 100-200
// are too common to report as unknown prefixes.
type form struct {
	re         *regexp.Regexp
	euidGroup  int
	numGroup   int
	knownEUIDs map[string]bool
}

// Checker judges the citations in text against a set of sources.
type Checker struct {
	cfg     Config
	allowed map[string]bool
	ignored map[string]bool
	rules   map[string]*citedRule            // by rule ID; the first source wins
	realms  map[string]map[string]*citedRule // by realm ID, then rule ID
	euids   map[string]bool
	forms   []form
}

// New creates a Checker for the given sources, in priority order. It returns an
// error for a state outside the rule lifecycle or an invalid
// rule_identifier_format.
func New(sources []Source, opts ...Option) (*Checker, error) {
	c := &Checker{
		allowed: make(map[string]bool),
		ignored: make(map[string]bool),
		rules:   make(map[string]*citedRule),
		realms:  make(map[string]map[string]*citedRule),
		euids:   make(map[string]bool),
	}
	for _, opt := range opts {
		opt(&c.cfg)
	}
	for _, s := range c.cfg.AllowedStates {
		if !entity.IsValidState(s) {
			return nil, fmt.Errorf("unknown rule state %q", s)
		}
		c.allowed[s] = true
	}
	for _, id := range c.cfg.Ignore {
		c.ignored[id] = true
	}

	var replaced []*entity.Rule
	for _, s := range sources {
		formatter, err := render.NewRuleIDFormatter(s.Format)
		if err != nil {
			return nil, fmt.Errorf("realm %q: %w", s.RealmID, err)
		}
		own := make(map[string]*citedRule)
		euids := make(map[string]bool)
		for _, ef := range s.Entities {
			collect(ef, own, euids, &replaced)
		}
		for id, r := range own {
			if _, dup := c.rules[id]; !dup {
				c.rules[id] = r
			}
		}
		for euid := range euids {
			c.euids[euid] = true
		}
		if s.RealmID != "" {
			c.realms[s.RealmID] = own
		}
		if f, ok := newForm(formatter, euids); ok {
			c.forms = append(c.forms, f)
		}
	}
	for _, r := range replaced {
		if target := c.rules[*r.Supersedes]; target != nil {
			target.recordSuccessor(r)
		}
	}
	return c, nil
}

// collect adds the rules of ef and of its sub-entities to rules and euids, and
// appends every rule that supersedes another to replaced.
func collect(ef *entity.File, rules map[string]*citedRule, euids map[string]bool, replaced *[]*entity.Rule) {
	if ef == nil {
		return
	}
	euids[ef.Entity.ID] = true
	for i := range ef.Rules {
		r := &ef.Rules[i]
		rules[r.ID] = &citedRule{state: r.State}
		if r.Supersedes != nil {
			*replaced = append(*replaced, r)
		}
	}
	for i := range ef.SubEntities {
		collect(&ef.SubEntities[i], rules, euids, replaced)
	}
}

// successorRank orders the states of a superseder by how much they replace the
// rule. A Draft or Abandoned superseder has no rank: it replaces nothing yet.
var successorRank = map[string]int{
	entity.StatePublished:   1,
	entity.StateRetired:     2,
	entity.StateTerminated:  2,
	entity.StateImplemented: 3,
}

// recordSuccessor keeps r as the rule's successor if r outranks the current one.
func (c *citedRule) recordSuccessor(r *entity.Rule) {
	if successorRank[r.State] > successorRank[c.successorState] {
		c.successor = r.ID
		c.successorState = r.State
	}
}

// newForm returns the form of f, and false when f keeps the stored shape.
func newForm(f render.RuleIDFormatter, euids map[string]bool) (form, bool) {
	const euid, num = `([A-Za-z0-9]{3,18})`, `(\d{3})`
	split := f.IDSplitter
	if split == "" {
		split = "-"
	}
	if f.EUIDClosing {
		return form{regexp.MustCompile(`\b` + num + split + euid + `\b`), 2, 1, euids}, true
	}
	if split != "-" {
		return form{regexp.MustCompile(`\b` + euid + split + num + `\b`), 1, 2, euids}, true
	}
	return form{}, false
}

// site is the place a citation was found.
type site struct {
	path string
	line int
}

// add records d, a finding at s.
func (s site) add(res *result.Result, d result.Diagnostic) {
	d.Module = module
	d.Path = s.path
	d.Line = s.line
	res.Add(d)
}

// CheckText reports the citations in data that are unknown or no longer live.
// path names the text in the diagnostics.
func (c *Checker) CheckText(path string, data []byte) *result.Result {
	res := &result.Result{}
	exempt := false
	for i, line := range strings.Split(string(data), "\n") {
		marked := strings.Contains(line, ignoreMarker)
		if !marked && !exempt {
			c.checkLine(line, site{path, i + 1}, res)
		}
		exempt = marked
	}
	if c.cfg.Strict {
		for i := range res.Diagnostics {
			if res.Diagnostics[i].Severity == result.Warning {
				res.Diagnostics[i].Severity = result.Error
			}
		}
	}
	return res
}

// checkLine judges each distinct citation on line.
func (c *Checker) checkLine(line string, at site, res *result.Result) {
	line = urlRe.ReplaceAllStringFunc(line, func(u string) string { return strings.Repeat(" ", len(u)) })
	seen := make(map[string]bool)
	cite := func(realm, euid, num string) {
		key := realm + ":" + euid + "-" + num
		if !seen[key] {
			seen[key] = true
			c.judge(realm, euid, num, at, res)
		}
	}
	for _, m := range citationRe.FindAllStringSubmatchIndex(line, -1) {
		if embedded(line, m[0], m[1]) {
			continue
		}
		realm := ""
		if m[2] >= 0 {
			realm = line[m[2]:m[3]]
		}
		cite(realm, line[m[4]:m[5]], line[m[6]:m[7]])
	}
	for _, f := range c.forms {
		for _, m := range f.re.FindAllStringSubmatchIndex(line, -1) {
			euid := line[m[2*f.euidGroup]:m[2*f.euidGroup+1]]
			if f.knownEUIDs[euid] && !embedded(line, m[0], m[1]) {
				cite("", euid, line[m[2*f.numGroup]:m[2*f.numGroup+1]])
			}
		}
	}
}

// embedded reports whether line[start:end] is part of a longer hyphenated token
// such as foo-RUL-004 or RUL-004-bar.
func embedded(line string, start, end int) bool {
	return (start > 0 && line[start-1] == '-') || (end < len(line) && line[end] == '-')
}

// judge reports what is wrong with one citation, if anything. A qualifier with
// no dot that names no Realm in scope is taken for a label in prose, such as
// see:RUL-004.
func (c *Checker) judge(realm, euid, num string, at site, res *result.Result) {
	id := euid + "-" + num
	if c.ignored[id] {
		return
	}
	var rule *citedRule
	rules, inScope := c.realms[realm]
	switch {
	case inScope:
		rule = rules[id]
	case strings.Contains(realm, "."):
		// Named a Realm that is not in scope: nothing can resolve.
	default:
		realm = ""
		rule = c.rules[id]
	}
	switch {
	case rule != nil:
		if !c.allowed[rule.state] {
			rule.report(id, at, res)
		}
	case inScope || c.euids[euid] || reportableEUIDRe.MatchString(euid):
		at.add(res, result.Diagnostic{Code: "RF001", Severity: result.Error, Message: unknownMessage(id, realm, inScope)})
	}
}

// unknownMessage describes a citation that resolves to no rule.
func unknownMessage(id, realm string, inScope bool) string {
	switch {
	case realm == "":
		return fmt.Sprintf("cited rule %s was not found in the realm or its dependencies", id)
	case inScope:
		return fmt.Sprintf("cited rule %s was not found in realm %s", id, realm)
	default:
		return fmt.Sprintf("cited rule %s names realm %s, which is not a dependency", id, realm)
	}
}

// report adds the finding for a citation of the rule, if it is not live. The
// state comes first: a Retired rule usually has a superseder in force, and that
// must not turn every citation of it into an error.
func (c *citedRule) report(id string, at site, res *result.Result) {
	switch {
	case c.state == entity.StateAbandoned:
		at.add(res, result.Diagnostic{Code: "RF002", Severity: result.Error, Message: fmt.Sprintf("cited rule %s is Abandoned", id)})
	case c.state == entity.StateRetired || c.state == entity.StateTerminated:
		msg := fmt.Sprintf("cited rule %s is no longer in force (state %s)", id, c.state)
		if c.successor != "" {
			msg += fmt.Sprintf("; %s replaces it", c.successor)
		}
		at.add(res, result.Diagnostic{Code: "RF003", Severity: result.Warning, Message: msg})
	case c.successorState == entity.StatePublished:
		at.add(res, result.Diagnostic{Code: "RF005", Severity: result.Warning, Message: fmt.Sprintf("cited rule %s will be replaced by %s, which is Published", id, c.successor)})
	case c.successor != "":
		at.add(res, result.Diagnostic{Code: "RF004", Severity: result.Error, Message: fmt.Sprintf("cited rule %s is replaced by %s (state %s)", id, c.successor, c.successorState)})
	case c.state == entity.StateDraft:
		at.add(res, result.Diagnostic{Code: "RF006", Severity: result.Warning, Message: fmt.Sprintf("cited rule %s is still a Draft", id)})
	}
}
