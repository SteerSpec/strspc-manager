package claude

import (
	"encoding/json"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// Key prefixes of what an audit in parts keeps in a BlobStore.
const (
	extractKeyPrefix = "claude/audit/extract/"
	judgeKeyPrefix   = "claude/audit/judge/"
)

// auditCache is what an audit in parts keeps in, and takes from, a
// BlobStore: the findings one file holds for one rule, keyed on the rule,
// the file's path and content, the model and the audit prompt version; and
// the judgement of one rule, keyed on the rule and on exactly what it was
// judged on, the audit scope and the rule's findings. A rerun thus reads
// only the files a rule has no findings of, and judges only the rules whose
// findings or scope changed.
//
// A finding is kept with the file it names, matched exactly against the
// paths its part showed. When a finding names no file of its part, it is
// still judged, but nothing is kept for that rule from any file of the
// part, since the model's path cannot say which file it came from. Nothing
// is kept of a file with content no part shows, nor for a rule whose
// findings could not be collected from every part. A rule whose findings
// are too large to judge keeps them: they are complete, and a rerun reports
// the same error without reading every file again.
//
// Without a store it keeps nothing: every group of rules reads every file.
// A failing store reads as empty.
type auditCache struct {
	store   ruleeval.BlobStore
	model   string
	version string
	files   []ruleeval.File
	unread  map[string]bool
	// read holds, per rule id, the paths it reads this run; cached and
	// fresh hold, per rule id and path, the findings taken from the store
	// and those read this run.
	read   map[string]map[string]bool
	cached map[string]map[string][]prompt.Finding
	fresh  map[string]map[string][]prompt.Finding
	// loose holds, per rule id, the findings naming no file of their part,
	// in part order; tainted the paths of those parts.
	loose   map[string][]prompt.Finding
	tainted map[string]map[string]bool
	// pending holds the values to store, written at once by flush.
	pending map[string][]byte
}

func (p *Provider) newAuditCache(store ruleeval.BlobStore, files []ruleeval.File, unread []prompt.Unread) *auditCache {
	c := &auditCache{
		store: store, model: p.cfg.Model, version: p.AuditPromptVersion(), files: files,
		unread: map[string]bool{}, read: map[string]map[string]bool{},
		cached: map[string]map[string][]prompt.Finding{}, fresh: map[string]map[string][]prompt.Finding{},
		loose: map[string][]prompt.Finding{}, tainted: map[string]map[string]bool{},
		pending: map[string][]byte{},
	}
	for _, u := range unread {
		c.unread[u.File] = true
	}
	return c
}

// toExtract returns, in order, the files group must read: those some rule
// of the group has no kept findings of. It takes the kept findings of the
// others.
func (c *auditCache) toExtract(group []ruleeval.RuleInput) []ruleeval.File {
	var need []ruleeval.File
	for _, f := range c.files {
		if !c.take(group, f) {
			need = append(need, f)
		}
	}
	for _, in := range group {
		read := map[string]bool{}
		for _, f := range need {
			read[f.Path] = true
		}
		c.read[in.Rule.ID] = read
	}
	return need
}

// take takes the kept findings of f for every rule of group, and reports
// whether there were.
func (c *auditCache) take(group []ruleeval.RuleInput, f ruleeval.File) bool {
	if c.store == nil || c.unread[f.Path] {
		return false
	}
	for _, in := range group {
		var fs []prompt.Finding
		if !c.get(c.extractKey(in, f), &fs) || fs == nil {
			return false
		}
		setFindings(c.cached, in.Rule.ID, f.Path, fs)
	}
	return true
}

// record files the findings a part showing the paths shown yielded.
func (c *auditCache) record(found map[string][]prompt.Finding, shown []string) {
	inPart := map[string]bool{}
	for _, s := range shown {
		inPart[s] = true
	}
	for _, id := range sortedKeys(found) {
		for _, f := range found[id] {
			if inPart[f.File] {
				setFindings(c.fresh, id, f.File, append(c.fresh[id][f.File], f))
				continue
			}
			c.loose[id] = append(c.loose[id], f)
			if c.tainted[id] == nil {
				c.tainted[id] = map[string]bool{}
			}
			for _, s := range shown {
				c.tainted[id][s] = true
			}
		}
	}
}

// keep stores the findings that the rules of group, but those failed, got
// from the files they read.
func (c *auditCache) keep(group []ruleeval.RuleInput, files []ruleeval.File, failed map[string]bool) {
	if c.store == nil {
		return
	}
	for _, in := range group {
		id := in.Rule.ID
		if failed[id] {
			continue
		}
		for _, f := range files {
			if c.unread[f.Path] || c.tainted[id][f.Path] {
				continue
			}
			fs := c.fresh[id][f.Path]
			if fs == nil {
				fs = []prompt.Finding{}
			}
			c.put(c.extractKey(in, f), fs)
		}
	}
}

// findings returns the findings of each input, file by file in the order
// of the set, then those naming no file of their part. The order does not
// depend on what was kept, so neither does the judgement's key.
func (c *auditCache) findings(inputs []ruleeval.RuleInput) map[string][]prompt.Finding {
	out := map[string][]prompt.Finding{}
	for _, in := range inputs {
		id := in.Rule.ID
		var fs []prompt.Finding
		seen := map[string]bool{}
		for _, f := range c.files {
			// A path listed twice holds its findings once.
			if seen[f.Path] {
				continue
			}
			seen[f.Path] = true
			if c.read[id][f.Path] {
				fs = append(fs, c.fresh[id][f.Path]...)
			} else {
				fs = append(fs, c.cached[id][f.Path]...)
			}
		}
		out[id] = append(fs, c.loose[id]...)
	}
	return out
}

// judgement returns the kept judgement of in on scope and its findings.
func (c *auditCache) judgement(in ruleeval.RuleInput, scope string, findings map[string][]prompt.Finding) (wireDetail, bool) {
	var d wireDetail
	if c.store == nil || !c.get(c.judgeKey(in, scope, findings), &d) {
		return wireDetail{}, false
	}
	switch ruleeval.Verdict(d.Verdict) {
	case ruleeval.Compliant, ruleeval.Violated, ruleeval.NotApplicable:
		return d, true
	default:
		return wireDetail{}, false
	}
}

// keepJudgement stores the judgement d of in on scope and its findings.
func (c *auditCache) keepJudgement(in ruleeval.RuleInput, scope string, findings map[string][]prompt.Finding, d wireDetail) {
	if c.store != nil {
		c.put(c.judgeKey(in, scope, findings), d)
	}
}

// extractKey keys the findings of f for in.
func (c *auditCache) extractKey(in ruleeval.RuleInput, f ruleeval.File) string {
	return extractKeyPrefix + ruleeval.CacheKey(in, ruleeval.FilesHash([]ruleeval.File{f}), c.model, c.version)
}

// judgeKey keys the judgement of in on what its request would carry for
// it: the scope and its findings, as rendered.
func (c *auditCache) judgeKey(in ruleeval.RuleInput, scope string, findings map[string][]prompt.Finding) string {
	on := scope + prompt.RuleFindings(in.Rule.ID, findings[in.Rule.ID])
	return judgeKeyPrefix + ruleeval.CacheKey(in, ruleeval.DiffHash(on), c.model, c.version)
}

// get decodes the value under key into v; a miss, a failing store or an
// undecodable value report false.
func (c *auditCache) get(key string, v any) bool {
	raw, ok, err := c.store.GetBlob(key)
	if err != nil || !ok {
		return false
	}
	return json.Unmarshal(raw, v) == nil
}

// put queues v to be stored under key by flush.
func (c *auditCache) put(key string, v any) {
	if raw, err := json.Marshal(v); err == nil {
		c.pending[key] = raw
	}
}

// flush stores every queued value at once. A failing store is not the
// audit's failure: the next run reads again what was not kept.
func (c *auditCache) flush() {
	if c.store != nil && len(c.pending) > 0 {
		_ = c.store.PutBlobs(c.pending)
		c.pending = map[string][]byte{}
	}
}

func setFindings(m map[string]map[string][]prompt.Finding, id, path string, fs []prompt.Finding) {
	if m[id] == nil {
		m[id] = map[string][]prompt.Finding{}
	}
	m[id][path] = fs
}
