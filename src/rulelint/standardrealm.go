package rulelint

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// CheckStandardRealm runs RL033 to RL041 over the Realm at dir and its
// sub-realms: the checks on rules that depend on whether their Realm is a
// Standard Realm (RUL-017, STDRLM-005, STDRLM-008 to STDRLM-010, STDRLM-012,
// STDRLM-013, STDRLM-016, STDRLM-017). Each Realm is judged by the kind its own
// manifest declares: a sub-realm without one is a code Realm (RLMMNFST-009).
// rf is used as given and dir/realm.json is not read again. LintRealm calls it,
// and realm-lint when it lints entity files itself; a source with no manifest
// has no Realm, so neither reaches it for one. A sub-realm whose manifest can't
// be loaded is skipped: realm-lint reports it (RM009).
func CheckStandardRealm(dir string, rf *entity.RealmFile, res *result.Result) {
	var opts []entity.WalkOption
	if len(rf.SubRealms) > 0 {
		opts = append(opts, entity.WithExcludeDirs(rf.SubRealms))
	}
	checkStandardRealmDir(dir, &rf.Realm, res, opts...)
	for _, name := range rf.SubRealms {
		subDir := filepath.Join(dir, name)
		sub, err := entity.LoadRealm(filepath.Join(subDir, "realm.json"))
		if err != nil {
			continue // realm-lint reports it (RM009).
		}
		checkStandardRealmDir(subDir, &sub.Realm, res)
	}
}

// standardRule is a rule of one Realm with where it was found, for the checks
// that span the Realm's entities.
type standardRule struct {
	rule *entity.Rule
	path string
}

// checkStandardRealmDir runs the checks on the entity files of one Realm
// directory, whose manifest declares meta.
func checkStandardRealmDir(dir string, meta *entity.RealmMeta, res *result.Result, opts ...entity.WalkOption) {
	standard := meta.Kind == entity.KindStandard
	var live []standardRule
	// A file that can't be read or parsed is reported by the first pass.
	_ = entity.WalkEntityFiles(dir, func(fpath string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			return nil
		}
		walkEntities(ef, func(e *entity.File) {
			if !standard {
				checkCodeRealmRules(e, fpath, res)
				return
			}
			checkStandardRules(e, meta, fpath, res)
			checkTranscriptionFixes(e, fpath, res)
			for i := range e.Rules {
				if entity.IsLiveStandardRule(e.Rules[i]) {
					live = append(live, standardRule{rule: &e.Rules[i], path: fpath})
				}
			}
		})
		return nil
	}, opts...)
	if standard {
		checkClauseProfiles(live, res)
	}
}

// walkEntities calls fn on ef and on each of its sub-entities, depth first.
func walkEntities(ef *entity.File, fn func(*entity.File)) {
	fn(ef)
	for i := range ef.SubEntities {
		walkEntities(&ef.SubEntities[i], fn)
	}
}

// checkCodeRealmRules runs RL034: a rule outside a Standard Realm has no
// `clause` and no `profiles` (RUL-017).
func checkCodeRealmRules(ef *entity.File, path string, res *result.Result) {
	for _, r := range ef.Rules {
		if r.Clause == "" && len(r.Profiles) == 0 {
			continue
		}
		res.Add(result.Diagnostic{
			Module:   module,
			Code:     "RL034",
			Severity: result.Error,
			Message:  fmt.Sprintf("rule %s has a clause or profiles, but its Realm is not a Standard Realm (RUL-017)", r.ID),
			Path:     path,
		})
	}
}

// checkStandardRules runs the checks on each rule of a Standard Realm on its
// own: RL033 (STDRLM-005), RL035 (STDRLM-008) and RL037 (STDRLM-010).
func checkStandardRules(ef *entity.File, meta *entity.RealmMeta, path string, res *result.Result) {
	for _, r := range ef.Rules {
		if r.Clause == "" && entity.IsLiveStandardRule(r) {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL033",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s is live in a Standard Realm but has no clause (STDRLM-005)", r.ID),
				Path:     path,
			})
		}
		for _, p := range r.Profiles {
			if !slices.Contains(meta.Profiles, p) {
				res.Add(result.Diagnostic{
					Module:   module,
					Code:     "RL035",
					Severity: result.Error,
					Message:  fmt.Sprintf("rule %s: profile %q is not declared in the profiles of its Realm (STDRLM-008)", r.ID, p),
					Path:     path,
				})
			}
		}
		if r.State == entity.StateImplemented {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL037",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s of a Standard Realm is Implemented (STDRLM-010)", r.ID),
				Path:     path,
			})
		}
	}
}

// checkTranscriptionFixes runs the checks on the rules of a Standard Realm
// entity that supersede another: RL038 (STDRLM-012), RL039 (STDRLM-016), RL040
// (STDRLM-017) and RL041 (STDRLM-013). A rule supersedes only a rule of its own
// entity (RUL-012), and a target that isn't there is left to RL012 and RL025.
// An Abandoned fix is exempt: it can't change, and it no longer counts.
func checkTranscriptionFixes(ef *entity.File, path string, res *result.Result) {
	byID := make(map[string]*entity.Rule, len(ef.Rules))
	for i := range ef.Rules {
		byID[ef.Rules[i].ID] = &ef.Rules[i]
	}
	fixes := make(map[string][]string)
	var targets []string
	for _, r := range ef.Rules {
		if r.Supersedes == nil || r.State == entity.StateAbandoned {
			continue
		}
		target := byID[*r.Supersedes]
		if target == nil {
			continue
		}
		if len(fixes[target.ID]) == 0 {
			targets = append(targets, target.ID)
		}
		fixes[target.ID] = append(fixes[target.ID], r.ID)
		if r.Clause != target.Clause {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL038",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s supersedes %s but has clause %q, not %q (STDRLM-012)", r.ID, target.ID, r.Clause, target.Clause),
				Path:     path,
			})
		}
		// Only the static form of STDRLM-017: while the fix is Draft its
		// target must be live, and an Abandoned rule never is again. Whether
		// the target was live when a later fix was drafted is left to
		// rule-diff.
		if target.State == entity.StateAbandoned || (r.State == entity.StateDraft && !entity.IsLiveStandardRule(*target)) {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL040",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s supersedes %s, which is not live (STDRLM-017)", r.ID, target.ID),
				Path:     path,
			})
		}
		// Once the fix is past Draft, the rule it supersedes is Terminated
		// (STDRLM-013).
		if r.State != entity.StateDraft && target.State != entity.StateTerminated {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL041",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s supersedes %s and is in state %s, but %s is in state %s, not Terminated (STDRLM-013)", r.ID, target.ID, r.State, target.ID, target.State),
				Path:     path,
			})
		}
	}
	for _, id := range targets {
		if len(fixes[id]) > 1 {
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL039",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s is superseded by %s; at most one rule that is not Abandoned may supersede it (STDRLM-016)", id, strings.Join(fixes[id], ", ")),
				Path:     path,
			})
		}
	}
}

// checkClauseProfiles runs RL036: the live rules of a Standard Realm that
// carry one clause declare the same profiles (STDRLM-009). Profiles are unique
// (RUL-016), so they are compared as sets. The set most of the clause's rules
// declare is taken as the clause's, the first one found on a tie, and each rule
// that declares another is reported at its own file.
func checkClauseProfiles(live []standardRule, res *result.Result) {
	var clauses []string
	byClause := make(map[string][]standardRule)
	for _, sr := range live {
		c := sr.rule.Clause
		if c == "" {
			continue // RL033.
		}
		if _, ok := byClause[c]; !ok {
			clauses = append(clauses, c)
		}
		byClause[c] = append(byClause[c], sr)
	}
	for _, c := range clauses {
		rules := byClause[c]
		count := make(map[string]int)
		for _, sr := range rules {
			count[profileKey(sr.rule.Profiles)]++
		}
		if len(count) < 2 {
			continue
		}
		var want standardRule
		for _, sr := range rules {
			if k := profileKey(sr.rule.Profiles); want.rule == nil || count[k] > count[profileKey(want.rule.Profiles)] {
				want = sr
			}
		}
		wantKey := profileKey(want.rule.Profiles)
		for _, sr := range rules {
			if profileKey(sr.rule.Profiles) == wantKey {
				continue
			}
			res.Add(result.Diagnostic{
				Module:   module,
				Code:     "RL036",
				Severity: result.Error,
				Message:  fmt.Sprintf("rule %s carries clause %q with profiles %v, but the clause's rules, such as %s, declare %v (STDRLM-009)", sr.rule.ID, c, sr.rule.Profiles, want.rule.ID, want.rule.Profiles),
				Path:     sr.path,
			})
		}
	}
}

// profileKey returns the profile names as one string, the same for any order.
func profileKey(profiles []string) string {
	sorted := slices.Clone(profiles)
	slices.Sort(sorted)
	return strings.Join(sorted, "\x00")
}
