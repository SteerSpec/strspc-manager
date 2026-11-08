package claude_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/prompt"
)

// memStore is an in-memory ruleeval.BlobStore that fails when err is set.
type memStore struct {
	mu    sync.Mutex
	m     map[string][]byte
	puts  int // values stored
	calls int // PutBlobs calls
	err   error
}

func newMemStore() *memStore { return &memStore{m: map[string][]byte{}} }

// GetBlob returns the value under key, or err when the store fails.
func (s *memStore) GetBlob(key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return nil, false, s.err
	}
	v, ok := s.m[key]
	return v, ok, nil
}

// PutBlobs stores values and counts them, or fails with err.
func (s *memStore) PutBlobs(values map[string][]byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	for k, v := range values {
		s.m[k] = append([]byte(nil), v...)
		s.puts++
	}
	s.calls++
	return nil
}

// cacheFixture is the audit of testFileRule over command and test files,
// one file per part, that the cache tests rerun.
type cacheFixture struct {
	inputs                               []ruleeval.RuleInput
	lintCmd, syncCmd, lintTest, syncTest ruleeval.File
	cfg                                  func(*claude.Config)
}

func newCacheFixture() cacheFixture {
	f := cacheFixture{
		inputs:   []ruleeval.RuleInput{rule("CMD-002", testFileRule)},
		lintCmd:  ruleeval.File{Path: "src/cmd/lint.go", Content: commandGo("lint")},
		syncCmd:  ruleeval.File{Path: "src/cmd/sync.go", Content: commandGo("sync")},
		lintTest: ruleeval.File{Path: "src/cmd/lint_test.go", Content: commandTestGo("lint")},
		syncTest: ruleeval.File{Path: "src/cmd/sync_test.go", Content: commandTestGo("sync")},
	}
	room := 0
	for _, file := range []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest, f.syncTest} {
		lines := strings.Split(strings.TrimSuffix(file.Content, "\n"), "\n")
		// Room for one file of a few more lines than the fixtures.
		room = max(room, len(prompt.FileSlice(file.Path, 1, len(lines), lines))+200)
	}
	f.cfg = partsBudget(f.inputs, room)
	return f
}

// cacheRun is one audit of a cache test: the response and the bodies the
// server saw.
type cacheRun struct {
	resp   *ruleeval.BatchResponse
	bodies []string
}

func (r cacheRun) extractions() []string {
	var out []string
	for _, b := range r.bodies {
		if isExtractRequest(b) {
			out = append(out, b)
		}
	}
	return out
}

func (r cacheRun) judgements() int {
	n := 0
	for _, b := range r.bodies {
		if isJudgeRequest(b) {
			n++
		}
	}
	return n
}

// audit runs f's audit of files with store against a server that extracts
// commandFacts, plus extra findings for the first file shown, and judges
// with testFileJudge.
func (f cacheFixture) audit(t *testing.T, store ruleeval.BlobStore, files []ruleeval.File, extra func(body string) []prompt.Finding) cacheRun {
	t.Helper()
	var (
		mu  sync.Mutex
		run cacheRun
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		mu.Lock()
		run.bodies = append(run.bodies, body)
		mu.Unlock()
		if isJudgeRequest(body) {
			writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-002": testFileJudge(body, files)}), nil))
			return
		}
		fs := commandFacts(body, files)
		if extra != nil {
			fs = append(fs, extra(body)...)
		}
		writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-002": fs}), nil))
	}))
	defer srv.Close()
	cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, Model: testModel}
	f.cfg(&cfg)
	p, err := claude.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run.resp, err = p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: f.inputs, Files: files, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func verdictOf(t *testing.T, r cacheRun) ruleeval.Verdict {
	t.Helper()
	v := r.resp.Verdicts["CMD-002"]
	if v == nil {
		t.Fatalf("no verdict, errors = %v", r.resp.Errors)
	}
	return v.Verdict
}

// TestAuditCache_RerunSendsOnlyWhatChanged checks a rerun reads only the
// files that changed, and judges again only when the findings did.
func TestAuditCache_RerunSendsOnlyWhatChanged(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}

	first := f.audit(t, store, files, nil)
	if verdictOf(t, first) != ruleeval.Violated || len(first.extractions()) != 3 || first.judgements() != 1 {
		t.Fatalf("first run: %d extractions, %d judgements", len(first.extractions()), first.judgements())
	}
	if store.calls != 1 || store.puts != 4 {
		t.Errorf("the first run should store 3 files and 1 judgement at once: %d calls, %d values", store.calls, store.puts)
	}

	same := f.audit(t, store, files, nil)
	if verdictOf(t, same) != ruleeval.Violated || len(same.bodies) != 0 {
		t.Fatalf("an unchanged set should send nothing, sent %d", len(same.bodies))
	}
	if store.calls != 1 {
		t.Errorf("an unchanged set should store nothing, nor erase what was kept: %d calls", store.calls)
	}

	// Editing a comment leaves the findings as they were: the file is read
	// again, the judgement is kept.
	edited := f.lintCmd
	edited.Content = strings.Replace(edited.Content, "lint note 01", "lint note 1!", 1)
	files[0] = edited
	reword := f.audit(t, store, files, nil)
	if verdictOf(t, reword) != ruleeval.Violated || len(reword.extractions()) != 1 || !shows(reword.extractions()[0], f.lintCmd.Path) || reword.judgements() != 0 {
		t.Fatalf("a reworded comment: %d extractions, %d judgements", len(reword.extractions()), reword.judgements())
	}

	// A line added above the command moves its finding: that file is read
	// again and the rule judged again.
	edited.Content = "// Added.\n" + edited.Content
	files[0] = edited
	moved := f.audit(t, store, files, nil)
	if verdictOf(t, moved) != ruleeval.Violated || len(moved.extractions()) != 1 || !shows(moved.extractions()[0], f.lintCmd.Path) || moved.judgements() != 1 {
		t.Fatalf("a moved finding: %d extractions, %d judgements", len(moved.extractions()), moved.judgements())
	}

	// The missing test added: only it is read, and the rule now holds.
	files = append(files, f.syncTest)
	added := f.audit(t, store, files, nil)
	if verdictOf(t, added) != ruleeval.Compliant || len(added.extractions()) != 1 || !shows(added.extractions()[0], f.syncTest.Path) || added.judgements() != 1 {
		t.Fatalf("an added test: %d extractions, %d judgements, verdict %v", len(added.extractions()), added.judgements(), verdictOf(t, added))
	}
}

// TestAuditCache_MatchesWithoutStore checks the findings a judgement gets
// from the store are those it gets without one, so that its request is the
// same.
func TestAuditCache_MatchesWithoutStore(t *testing.T) {
	f := newCacheFixture()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	judgeBody := func(r cacheRun) string {
		for _, b := range r.bodies {
			if isJudgeRequest(b) {
				return b
			}
		}
		t.Fatal("no judgement")
		return ""
	}
	plain := judgeBody(f.audit(t, nil, files, nil))

	store := newMemStore()
	f.audit(t, store, files, nil)
	// Only sync.go changes, in a comment; the others come from the store.
	edited := f.syncCmd
	edited.Content = strings.Replace(edited.Content, "sync note 01", "sync note 1!", 1)
	// Forget the judgement so that the rule is judged again.
	for k := range store.m {
		if strings.Contains(k, "/judge/") {
			delete(store.m, k)
		}
	}
	mixed := f.audit(t, store, []ruleeval.File{f.lintCmd, edited, f.lintTest}, nil)
	if len(mixed.extractions()) != 1 {
		t.Fatalf("extractions = %d, want sync.go only", len(mixed.extractions()))
	}
	if got := judgeBody(mixed); got != plain {
		t.Errorf("the judgement differs with findings from the store:\n%s\nvs\n%s", got, plain)
	}
}

// TestAuditCache_LooseFindingIsJudgedNotKept checks a finding naming a
// file its part did not show still reaches the judgement, but keeps
// nothing for that part, which is read again on the next run.
func TestAuditCache_LooseFindingIsJudgedNotKept(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	loose := func(body string) []prompt.Finding {
		if !shows(body, f.lintCmd.Path) {
			return nil
		}
		return []prompt.Finding{{Kind: prompt.FindingFact, File: "cmd/lint.go", Line: 1, Excerpt: "package cmd", Note: "loose note"}}
	}

	first := f.audit(t, store, files, loose)
	var judge string
	for _, b := range first.bodies {
		if isJudgeRequest(b) {
			judge = b
		}
	}
	if !strings.Contains(judge, "loose note") {
		t.Fatal("the loose finding should be judged")
	}

	second := f.audit(t, store, files, loose)
	if len(second.extractions()) != 1 || !shows(second.extractions()[0], f.lintCmd.Path) || second.judgements() != 0 {
		t.Fatalf("second run: %d extractions, %d judgements; want lint.go read again, the judgement kept",
			len(second.extractions()), second.judgements())
	}
}

// TestAuditCache_UnreadFileIsNotKept checks a file with a line no part
// shows is read on every run, its findings never kept.
func TestAuditCache_UnreadFileIsNotKept(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	minified := ruleeval.File{Path: "web/app.min.js", Content: "var a = 1;\n" + strings.Repeat("x", 20000) + "\n"}
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest, minified}

	first := f.audit(t, store, files, nil)
	if len(first.resp.Errors) != 1 || !strings.Contains(first.resp.Errors[0].Error(), "line 2 of web/app.min.js") {
		t.Fatalf("errors = %v, want the unread line", first.resp.Errors)
	}
	second := f.audit(t, store, files, nil)
	if len(second.extractions()) != 1 || !shows(second.extractions()[0], minified.Path) || second.judgements() != 0 {
		t.Fatalf("second run: %d extractions, %d judgements; want the minified file read again", len(second.extractions()), second.judgements())
	}
	if len(second.resp.Errors) != 1 {
		t.Errorf("the rerun should report the unread line again, got %v", second.resp.Errors)
	}
}

// TestAuditCache_FailedRuleIsNotKept checks a rule a part did not answer
// for keeps nothing, so that the next run reads every file for it again.
func TestAuditCache_FailedRuleIsNotKept(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		if shows(string(raw), f.syncCmd.Path) {
			writeJSON(w, 200, message("end_turn", `{}`, nil))
			return
		}
		writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-002": commandFacts(string(raw), files)}), nil))
	}))
	defer srv.Close()
	cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, Model: testModel, Concurrency: 1}
	f.cfg(&cfg)
	p, err := claude.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: f.inputs, Files: files, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Verdicts["CMD-002"] != nil || store.puts != 0 {
		t.Fatalf("a failed rule: verdict %+v, %d values kept; want none", resp.Verdicts["CMD-002"], store.puts)
	}
}

// TestAuditCache_FailedRequestKeepsCompleteGroups checks a request that
// fails the audit still keeps what the groups that were done found, so the
// rerun reads only for the rest.
func TestAuditCache_FailedRequestKeepsCompleteGroups(t *testing.T) {
	f := newCacheFixture()
	inputs := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule), f.inputs[0]}
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	store := newMemStore()
	run := func(broken bool) []string {
		var bodies []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			body := string(raw)
			bodies = append(bodies, body)
			switch {
			case isJudgeRequest(body):
				writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{
					"CMD-001": {Verdict: "compliant", Explanation: "Long is set.", Confidence: 0.9}, "CMD-002": testFileJudge(body, files),
				}), nil))
			case broken && asks(body, "CMD-002"):
				writeJSON(w, 200, message("end_turn", "not json", nil))
			case asks(body, "CMD-002"):
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-002": commandFacts(body, files)}), nil))
			default:
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
			}
		}))
		defer srv.Close()
		// One rule per group, one request at a time: CMD-001's group is
		// done before CMD-002's fails.
		cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, Model: testModel, MaxRulesPerRequest: 1, Concurrency: 1}
		partsBudget(inputs, 2600)(&cfg)
		p, err := claude.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Files: files, Store: store})
		if broken != errors.Is(err, claude.ErrMalformed) {
			t.Fatalf("broken=%v: err = %v", broken, err)
		}
		return bodies
	}

	run(true)
	if store.puts != len(files) {
		t.Fatalf("values kept = %d, want CMD-001's findings of every file", store.puts)
	}
	for _, b := range run(false) {
		if isExtractRequest(b) && asks(b, "CMD-001") {
			t.Errorf("the rerun should not read for CMD-001 again: %.200s", b)
		}
	}
}

// TestAuditCache_FailedJudgementKeepsTheOthers checks a judgement request
// that fails the audit leaves the judgements that came back kept.
func TestAuditCache_FailedJudgementKeepsTheOthers(t *testing.T) {
	f := newCacheFixture()
	inputs := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule), f.inputs[0]}
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	store := newMemStore()
	run := func(broken bool) []string {
		var bodies []string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			body := string(raw)
			bodies = append(bodies, body)
			switch {
			case isJudgeRequest(body) && asks(body, "CMD-002") && broken:
				writeJSON(w, 200, message("end_turn", "not json", nil))
			case isJudgeRequest(body) && asks(body, "CMD-002"):
				writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-002": testFileJudge(body, files)}), nil))
			case isJudgeRequest(body):
				writeJSON(w, 200, message("end_turn", judgeText(map[string]judgement{"CMD-001": {Verdict: "compliant", Explanation: "Long is set.", Confidence: 0.9}}), nil))
			case asks(body, "CMD-002"):
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-002": commandFacts(body, files)}), nil))
			default:
				writeJSON(w, 200, message("end_turn", findingsText(map[string][]prompt.Finding{"CMD-001": nil}), nil))
			}
		}))
		defer srv.Close()
		// One rule per judgement, one request at a time: CMD-001's comes
		// back before CMD-002's fails.
		cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, Model: testModel, MaxRulesPerRequest: 1, Concurrency: 1}
		partsBudget(inputs, 2600)(&cfg)
		p, err := claude.New(cfg)
		if err != nil {
			t.Fatal(err)
		}
		_, err = p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Files: files, Store: store})
		if broken != errors.Is(err, claude.ErrMalformed) {
			t.Fatalf("broken=%v: err = %v", broken, err)
		}
		return bodies
	}

	run(true)
	for _, b := range run(false) {
		if isJudgeRequest(b) && asks(b, "CMD-001") {
			t.Error("the rerun should not judge CMD-001 again")
		}
		if isExtractRequest(b) {
			t.Errorf("the rerun should read nothing: %.200s", b)
		}
	}
}

// TestAuditCache_FailingStoreReadsEmpty checks a store that fails reads as
// an empty one and does not fail the audit.
func TestAuditCache_FailingStoreReadsEmpty(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	store.err = errors.New("disk full")
	run := f.audit(t, store, []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}, nil)
	if verdictOf(t, run) != ruleeval.Violated || len(run.resp.Errors) != 0 || len(run.extractions()) != 3 {
		t.Fatalf("a failing store: %d extractions, errors %v", len(run.extractions()), run.resp.Errors)
	}
}

// TestAuditCache_BadValuesRead checks a value the store holds that does not
// decode, or a judgement of unknown verdict, reads as a miss.
func TestAuditCache_BadValuesRead(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	f.audit(t, store, files, nil)
	for k := range store.m {
		if strings.Contains(k, "/judge/") {
			store.m[k] = []byte(`{"verdict":"maybe"}`)
		} else {
			store.m[k] = []byte(`null`)
		}
	}
	run := f.audit(t, store, files, nil)
	if len(run.extractions()) != 3 || run.judgements() != 1 || verdictOf(t, run) != ruleeval.Violated {
		t.Fatalf("bad values: %d extractions, %d judgements", len(run.extractions()), run.judgements())
	}
}

// emptyJudgement is the judgement of strspc-manager#338: every field
// present, nothing in them.
var emptyJudgement = judgement{Verdict: "violated"}

// auditJudgedBy runs an audit of CMD-001 and CMD-002 over files with store,
// one judgement for both rules, against a server that extracts
// commandFacts for CMD-002 and nothing for CMD-001, and judges with judge.
func (f cacheFixture) auditJudgedBy(t *testing.T, store ruleeval.BlobStore, files []ruleeval.File, judge func(body string) map[string]judgement) cacheRun {
	t.Helper()
	inputs := []ruleeval.RuleInput{rule("CMD-001", cobraLongRule), f.inputs[0]}
	var (
		mu  sync.Mutex
		run cacheRun
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		mu.Lock()
		run.bodies = append(run.bodies, body)
		mu.Unlock()
		if isJudgeRequest(body) {
			writeJSON(w, 200, message("end_turn", judgeText(judge(body)), nil))
			return
		}
		fs := map[string][]prompt.Finding{}
		if asks(body, "CMD-001") {
			fs["CMD-001"] = nil
		}
		if asks(body, "CMD-002") {
			fs["CMD-002"] = commandFacts(body, files)
		}
		writeJSON(w, 200, message("end_turn", findingsText(fs), nil))
	}))
	defer srv.Close()
	cfg := claude.Config{APIKey: "k", BaseURL: srv.URL, Model: testModel}
	partsBudget(inputs, 2600)(&cfg)
	p, err := claude.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	run.resp, err = p.EvaluateBatch(context.Background(), ruleeval.BatchRequest{EntityID: "CMD", Inputs: inputs, Files: files, Store: store})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

// judgeRequests returns the judge requests of r.
func (r cacheRun) judgeRequests() []string {
	var out []string
	for _, b := range r.bodies {
		if isJudgeRequest(b) {
			out = append(out, b)
		}
	}
	return out
}

// TestAuditCache_EmptyJudgementIsAskedAgainAlone checks a rule whose
// judgement comes back empty is judged again on its own, once, and that
// what that second judgement says is kept (strspc-manager#338).
func TestAuditCache_EmptyJudgementIsAskedAgainAlone(t *testing.T) {
	f := newCacheFixture()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	store := newMemStore()
	judge := func(body string) map[string]judgement {
		if asks(body, "CMD-001") {
			return map[string]judgement{
				"CMD-001": {Verdict: "compliant", Explanation: "Long is set.", Confidence: 0.9},
				"CMD-002": emptyJudgement,
			}
		}
		return map[string]judgement{"CMD-002": testFileJudge(body, files)}
	}
	run := f.auditJudgedBy(t, store, files, judge)
	if len(run.resp.Errors) != 0 {
		t.Fatalf("errors = %v, want none", run.resp.Errors)
	}
	for id, want := range map[string]ruleeval.Verdict{"CMD-001": ruleeval.Compliant, "CMD-002": ruleeval.Violated} {
		if v := run.resp.Verdicts[id]; v == nil || v.Verdict != want || v.Explanation == "" {
			t.Errorf("%s = %+v, want %s with its explanation", id, v, want)
		}
	}
	reqs := run.judgeRequests()
	if len(reqs) != 2 || asks(reqs[1], "CMD-001") || !asks(reqs[1], "CMD-002") {
		t.Fatalf("want the group judged, then CMD-002 alone: %d judge requests", len(reqs))
	}

	rerun := f.auditJudgedBy(t, store, files, judge)
	if len(rerun.bodies) != 0 {
		t.Errorf("the rerun should be served from the store, sent %d requests", len(rerun.bodies))
	}
}

// TestAuditCache_EmptyJudgementTwiceIsNotJudged checks a rule whose
// judgement comes back empty again when asked alone gets no verdict, an
// error, and nothing kept, so a rerun asks again (strspc-manager#338).
func TestAuditCache_EmptyJudgementTwiceIsNotJudged(t *testing.T) {
	f := newCacheFixture()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	store := newMemStore()
	judge := func(body string) map[string]judgement {
		m := map[string]judgement{"CMD-002": emptyJudgement}
		if asks(body, "CMD-001") {
			m["CMD-001"] = judgement{Verdict: "compliant", Explanation: "Long is set.", Confidence: 0.9}
		}
		return m
	}
	for i, wantExtractions := range []int{3, 0} {
		run := f.auditJudgedBy(t, store, files, judge)
		if v := run.resp.Verdicts["CMD-002"]; v != nil {
			t.Errorf("run %d: CMD-002 = %+v, want no verdict", i, v)
		}
		if v := run.resp.Verdicts["CMD-001"]; v == nil || v.Verdict != ruleeval.Compliant {
			t.Errorf("run %d: CMD-001 = %+v, want compliant", i, v)
		}
		if len(run.resp.Errors) != 1 || !strings.Contains(run.resp.Errors[0].Error(), "rule CMD-002 came back twice with no explanation") {
			t.Errorf("run %d: errors = %v, want CMD-002 reported not judged", i, run.resp.Errors)
		}
		// The first run judges both rules, then CMD-002 alone; the rerun
		// has CMD-001 kept and asks for CMD-002 twice more.
		wantJudgements := 2
		if len(run.extractions()) != wantExtractions || run.judgements() != wantJudgements {
			t.Errorf("run %d: %d extractions, %d judgements, want %d and %d",
				i, len(run.extractions()), run.judgements(), wantExtractions, wantJudgements)
		}
	}
}

// TestAuditCache_StoredEmptyJudgementIsAMiss checks an empty judgement an
// earlier version kept is judged again rather than served.
func TestAuditCache_StoredEmptyJudgementIsAMiss(t *testing.T) {
	f := newCacheFixture()
	store := newMemStore()
	files := []ruleeval.File{f.lintCmd, f.syncCmd, f.lintTest}
	f.audit(t, store, files, nil)
	n := 0
	for k := range store.m {
		if strings.Contains(k, "/judge/") {
			store.m[k] = []byte(`{"verdict":"violated","explanation":"","confidence":0,"evidence":[]}`)
			n++
		}
	}
	if n != 1 {
		t.Fatalf("store holds %d judgements, want 1", n)
	}
	run := f.audit(t, store, files, nil)
	if len(run.extractions()) != 0 || run.judgements() != 1 {
		t.Fatalf("%d extractions, %d judgements, want 0 and 1", len(run.extractions()), run.judgements())
	}
	if v := run.resp.Verdicts["CMD-002"]; v == nil || v.Verdict != ruleeval.Violated || v.Explanation == "" {
		t.Fatalf("CMD-002 = %+v, want violated with its explanation", v)
	}
}
