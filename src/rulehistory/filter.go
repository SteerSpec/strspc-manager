package rulehistory

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

// Filter narrows a History. Empty fields do not filter.
type Filter struct {
	// Entity keeps the events of one entity, given by its EUID: the top-level
	// entity of the file or one of its sub-entities.
	Entity string
	// RuleID keeps the events of one rule, and the added event of a rule that
	// supersedes it.
	RuleID string
	// Since keeps the changes that produced a rule set version greater than
	// it, like `git log v1..`.
	Since string
	// Until keeps the changes that produced a rule set version up to and
	// including it. Since and Until together give the range (Since, Until].
	Until string
}

// Filter returns the events of h that f keeps. A Since or Until that is not
// a semantic version is an error. When either is set, an event whose rule
// set version is not a semantic version is dropped, with one warning per
// such version.
func (h *History) Filter(f Filter) (*History, error) {
	since, err := parseBound("since", f.Since)
	if err != nil {
		return nil, err
	}
	until, err := parseBound("until", f.Until)
	if err != nil {
		return nil, err
	}
	out := &History{Events: []Event{}, Warnings: append([]Warning(nil), h.Warnings...)}
	warned := map[string]bool{}
	for _, ev := range h.Events {
		if f.Entity != "" && !inEntity(ev.Entity, f.Entity) {
			continue
		}
		if f.RuleID != "" && ev.RuleID != f.RuleID && (ev.Kind != Added || ev.Supersedes != f.RuleID) {
			continue
		}
		if since != nil || until != nil {
			v, err := semver.NewVersion(ev.RuleSetVersion)
			if err != nil {
				if !warned[ev.RuleSetVersion] {
					warned[ev.RuleSetVersion] = true
					out.Warnings = append(out.Warnings, Warning{Message: fmt.Sprintf("rule set version %q is not a semantic version; its changes are left out of the version range", ev.RuleSetVersion)})
				}
				continue
			}
			if since != nil && !v.GreaterThan(since) {
				continue
			}
			if until != nil && v.GreaterThan(until) {
				continue
			}
		}
		out.Events = append(out.Events, ev)
	}
	return out, nil
}

// ParseRange splits "V1..V2" into its bounds. Either side may be empty.
func ParseRange(s string) (since, until string, err error) {
	since, until, ok := strings.Cut(s, "..")
	if !ok {
		return "", "", fmt.Errorf("version range %q is not of the form V1..V2", s)
	}
	return since, until, nil
}

func parseBound(name, s string) (*semver.Version, error) {
	if s == "" {
		return nil, nil
	}
	v, err := semver.NewVersion(s)
	if err != nil {
		return nil, fmt.Errorf("%s version %q is not a semantic version: %w", name, s, err)
	}
	return v, nil
}

// inEntity reports whether the entity path p ("ENT > SUB") ends at euid.
func inEntity(p, euid string) bool {
	return p == euid || strings.HasSuffix(p, " > "+euid)
}
