// Package qa_test runs deliberately broken Realms through every module of the
// engine and proves that every diagnostic code fires through a public API.
// testdata/README.md describes the fixtures.
package qa_test

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/rulerefs"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

var update = flag.Bool("update", false, "stamp fixture hashes and rewrite expected.json")

const (
	cleanDir   = "testdata/clean"
	brokenDir  = "testdata/broken"
	schemaRoot = "../../rules"

	// deadBaseURL is the API root of every fetcher. It is never dialled:
	// offlineClient answers first, so a test cannot reach the network.
	deadBaseURL = "http://qa.invalid"

	// defaultDiff is the diff of a scenario that declares no eval runs.
	defaultDiff = "diff --git a/app.go b/app.go\n+// an unrelated change\n"
)

// finding is one diagnostic as expected.json records it. Path is relative to
// the scenario, with the source line appended when the diagnostic has one.
type finding struct {
	Module   string `json:"module"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path"`
}

func (f finding) String() string {
	return fmt.Sprintf("%s %s %s %q", f.Module, f.Code, f.Severity, f.Path)
}

// compareFindings orders findings so that a run compares equal whatever map
// order the modules iterated in. Duplicates are kept.
func compareFindings(a, b finding) int {
	return strings.Compare(a.key(), b.key())
}

func (f finding) key() string {
	return f.Module + "\x00" + f.Code + "\x00" + f.Severity + "\x00" + f.Path
}

// scenario is scenario.json: what a fixture needs that its files cannot say.
type scenario struct {
	// Claims lists the codes the scenario exists to fire. It is written by
	// hand and -update never touches it, so a code that stops firing fails
	// the test even when expected.json is regenerated.
	Claims []string `json:"claims"`
	// BadHash lists the files, relative to the scenario, whose rule_set.hash is
	// wrong on purpose or which cannot be parsed; -update leaves them alone.
	BadHash []string `json:"bad_hash,omitempty"`
	// RuleSources lists the rule-resolve sources relative to the scenario,
	// `realm` when empty.
	RuleSources []string `json:"rule_sources,omitempty"`
	// Eval lists the rule-eval runs. When empty, one run with defaultDiff.
	Eval []evalRun `json:"eval,omitempty"`
}

// evalRun is one rule-eval run over the rules rule-resolve found.
type evalRun struct {
	// Diff is the diff text; empty asks for the empty-diff outcome.
	Diff string `json:"diff"`
	// Violated lists the rules the fake provider reports as violated.
	Violated []string `json:"violated,omitempty"`
	// FailOn lists the states whose violations are errors.
	FailOn []string `json:"fail_on,omitempty"`
	// StaticOnly runs the evaluator without a provider.
	StaticOnly bool `json:"static_only,omitempty"`
}

func loadScenario(t *testing.T, dir string) scenario {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "scenario.json"))
	if err != nil {
		t.Fatalf("reading scenario.json: %v", err)
	}
	var sc scenario
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sc); err != nil {
		t.Fatalf("parsing %s/scenario.json: %v", dir, err)
	}
	return sc
}

// brokenScenarios lists the directories under testdata/broken.
func brokenScenarios(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(brokenDir)
	if err != nil {
		t.Fatalf("reading %s: %v", brokenDir, err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// offlineClient answers every request with a 404 from memory. Fetchers built
// on it behave as if every remote were missing, and never open a socket.
func offlineClient() *http.Client {
	return &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNotFound, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
	})}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// schemaFetcher reads the repository's own schemas and cannot go further.
func schemaFetcher(t *testing.T) *schema.Fetcher {
	t.Helper()
	return schema.New(
		schema.WithLocalDir(schemaRoot),
		schema.WithCacheDir(t.TempDir()),
		schema.WithBaseURL(deadBaseURL),
		schema.WithClient(offlineClient()),
	)
}

// remoteFetcher is the stub behind every github:// source: each download 404s.
func remoteFetcher(t *testing.T) *remote.Fetcher {
	t.Helper()
	return &remote.Fetcher{Client: offlineClient(), BaseURL: deadBaseURL, CacheDir: t.TempDir()}
}

// localDependency is the realm-lint dependency fetcher: a relative source is a
// directory, and anything else cannot be fetched offline.
func localDependency(_ context.Context, realmDir string, dep entity.RealmDep) (string, error) {
	if dep.Source == "" || filepath.IsAbs(dep.Source) || strings.Contains(dep.Source, "://") {
		return "", fmt.Errorf("no local directory for source %q", dep.Source)
	}
	return filepath.Join(realmDir, dep.Source), nil
}

// fakeProvider judges every rule without an LLM.
type fakeProvider struct {
	violated []string
}

func (p fakeProvider) Evaluate(_ context.Context, req ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	if !slices.Contains(p.violated, req.Rule.ID) {
		return &ruleeval.EvalResponse{Verdict: ruleeval.NotApplicable}, nil
	}
	return &ruleeval.EvalResponse{
		Verdict:     ruleeval.Violated,
		Explanation: "the fake provider says so",
		Evidence:    []ruleeval.Location{{File: "app.go", Line: 1}},
	}, nil
}

// modules runs one fixture through the engine.
type modules struct {
	t      *testing.T
	ctx    context.Context
	root   string // absolute fixture directory
	cache  string // absolute remote cache directory
	strict bool
	sc     scenario
	remote *remote.Fetcher
	schema *schema.Fetcher
	got    []finding
}

// runFixture runs the fixture at dir through every module it has input for and
// returns the sorted findings.
func runFixture(t *testing.T, dir string, sc scenario, strict bool) []finding {
	t.Helper()
	root, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("resolving %s: %v", dir, err)
	}
	fetcher := remoteFetcher(t)
	m := &modules{t: t, ctx: context.Background(), root: root, cache: fetcher.CacheDir, strict: strict, sc: sc, remote: fetcher, schema: schemaFetcher(t)}
	if isDir(m.path("realm")) {
		m.ruleLint()
		m.realmLint()
		m.realmResolve()
		m.ruleEval(m.ruleResolve())
		m.ruleRefs()
	}
	if isDir(m.path("head")) {
		m.ruleDiff()
	}
	slices.SortFunc(m.got, compareFindings)
	return m.got
}

func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

func (m *modules) path(elem ...string) string {
	return filepath.Join(append([]string{m.root}, elem...)...)
}

// record adds res, its paths made independent of the machine.
func (m *modules) record(res *result.Result) {
	for _, d := range res.Diagnostics {
		path := m.relative(d.Path)
		if d.Line > 0 {
			path = fmt.Sprintf("%s:%d", path, d.Line)
		}
		m.got = append(m.got, finding{Module: d.Module, Code: d.Code, Severity: d.Severity.String(), Path: path})
	}
}

// relative rewrites the absolute directories inside p, which can be a path
// followed by a JSON pointer.
func (m *modules) relative(p string) string {
	p = strings.ReplaceAll(p, m.root+string(filepath.Separator), "")
	p = strings.ReplaceAll(p, m.root, ".")
	p = strings.ReplaceAll(p, m.cache, "<cache>")
	return filepath.ToSlash(p)
}

func (m *modules) ruleLint() {
	l := rulelint.New(rulelint.WithSchemaFetcher(m.schema), rulelint.WithStrict(m.strict))
	m.record(l.LintRealm(m.ctx, m.path("realm")))
}

func (m *modules) realmLint() {
	l := realmlint.New(
		realmlint.WithSchemaFetcher(m.schema),
		realmlint.WithDependencyFetcher(localDependency),
		realmlint.WithStrict(m.strict),
	)
	m.record(l.Lint(m.ctx, m.path("realm")))
}

// realmResolve needs a parsed manifest, which is realm-lint's to report.
func (m *modules) realmResolve() {
	rf, err := entity.LoadRealm(m.path("realm", "realm.json"))
	if err != nil {
		return
	}
	_, res := realmresolve.New(realmresolve.WithRemoteFetcher(m.remote)).Resolve(m.ctx, rf, m.path("realm"))
	m.record(res)
}

func (m *modules) ruleResolve() []*ruleresolve.ResolvedFile {
	sources := m.sc.RuleSources
	if len(sources) == 0 {
		sources = []string{"realm"}
	}
	entries := make([]ruleresolve.SourceEntry, 0, len(sources))
	for _, s := range sources {
		entries = append(entries, ruleresolve.SourceEntry{Source: s, Scope: ruleresolve.ScopeGlobal})
	}
	r, err := ruleresolve.New(entries, ruleresolve.WithBaseDir(m.root), ruleresolve.WithRemoteFetcher(m.remote))
	if err != nil {
		m.t.Fatalf("rule-resolve: %v", err)
	}
	files, res := r.Resolve(m.ctx)
	m.record(res)
	return files
}

func (m *modules) ruleEval(files []*ruleresolve.ResolvedFile) {
	var inputs []ruleeval.RuleInput
	for _, f := range files {
		inputs = append(inputs, ruleeval.RuleInputsFromFile(f.File)...)
	}
	runs := m.sc.Eval
	if len(runs) == 0 {
		runs = []evalRun{{Diff: defaultDiff}}
	}
	for _, run := range runs {
		var provider ruleeval.Provider
		if !run.StaticOnly {
			provider = fakeProvider{violated: run.Violated}
		}
		e, err := ruleeval.New(provider, ruleeval.WithStaticOnly(run.StaticOnly), ruleeval.WithFailOn(run.FailOn))
		if err != nil {
			m.t.Fatalf("rule-eval: %v", err)
		}
		m.record(e.Evaluate(m.ctx, inputs, run.Diff))
	}
}

// ruleRefs checks the files under cited/ against the Realm, each of its
// sub-realms and each of its local dependencies. A scenario without cited/ has
// no input for it.
func (m *modules) ruleRefs() {
	if !isDir(m.path("cited")) {
		return
	}
	checker, err := rulerefs.New(m.refSources(), rulerefs.WithStrict(m.strict))
	if err != nil {
		m.t.Fatalf("rule-refs: %v", err)
	}
	entries, err := os.ReadDir(m.path("cited"))
	if err != nil {
		m.t.Fatalf("reading cited/: %v", err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(m.path("cited", e.Name()))
		if err != nil {
			m.t.Fatalf("reading %s: %v", e.Name(), err)
		}
		m.record(checker.CheckText(m.path("cited", e.Name()), data))
	}
}

func (m *modules) refSources() []rulerefs.Source {
	rf, err := entity.LoadRealm(m.path("realm", "realm.json"))
	if err != nil {
		m.t.Fatalf("rule-refs needs a realm.json: %v", err)
	}
	sources := []rulerefs.Source{m.refSource(rf.Realm.ID, rf.RuleIdentifierFormat, m.path("realm"), rf.SubRealms)}
	for _, sub := range rf.SubRealms {
		sources = append(sources, m.refSource(rf.Realm.ID+"."+sub, rf.RuleIdentifierFormat, m.path("realm", sub), nil))
	}
	for _, dep := range rf.Dependencies {
		dir := filepath.Join(m.path("realm"), dep.Source)
		sources = append(sources, m.refSource(dep.RealmID, nil, dir, nil))
	}
	return sources
}

func (m *modules) refSource(id string, format *entity.RuleIdentifierFormat, dir string, exclude []string) rulerefs.Source {
	src := rulerefs.Source{RealmID: id, Format: format}
	err := entity.WalkEntityFiles(dir, func(path string, _ []byte, ef *entity.File, parseErr error) error {
		if parseErr != nil {
			return fmt.Errorf("%s: %w", path, parseErr)
		}
		src.Entities = append(src.Entities, ef)
		return nil
	}, entity.WithExcludeDirs(exclude))
	if err != nil {
		m.t.Fatalf("rule-refs sources: %v", err)
	}
	return src
}

func (m *modules) ruleDiff() {
	m.record(rulediff.CompareDir(m.path("base"), m.path("head"), rulediff.WithStrict(m.strict)))
}

// readExpected loads the expected.json of the scenario at dir.
func readExpected(dir string) ([]finding, error) {
	data, err := os.ReadFile(filepath.Join(dir, "expected.json"))
	if err != nil {
		return nil, err
	}
	var want []finding
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&want); err != nil {
		return nil, fmt.Errorf("parsing %s/expected.json: %w", dir, err)
	}
	return want, nil
}

func writeExpected(dir string, got []finding) error {
	if got == nil {
		got = []finding{}
	}
	data, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding expected.json: %w", err)
	}
	return os.WriteFile(filepath.Join(dir, "expected.json"), append(data, '\n'), 0o644)
}

// assertFindings fails with the findings each side has that the other lacks.
func assertFindings(t *testing.T, want, got []finding) {
	t.Helper()
	if slices.Equal(want, got) {
		return
	}
	var b strings.Builder
	for _, f := range subtract(want, got) {
		fmt.Fprintf(&b, "\n  missing:    %s", f)
	}
	for _, f := range subtract(got, want) {
		fmt.Fprintf(&b, "\n  unexpected: %s", f)
	}
	t.Errorf("diagnostics differ:%s", b.String())
}

// subtract returns the findings of a that b does not match one for one.
func subtract(a, b []finding) []finding {
	rest := slices.Clone(b)
	var out []finding
	for _, f := range a {
		if i := slices.Index(rest, f); i >= 0 {
			rest = slices.Delete(rest, i, i+1)
		} else {
			out = append(out, f)
		}
	}
	return out
}

var errNoExpected = errors.New("no expected.json: run go test ./src/qa -update and review the result")
