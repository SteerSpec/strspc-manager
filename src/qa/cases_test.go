package qa_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/evalcache"
	"github.com/SteerSpec/strspc-manager/src/realmlint"
	"github.com/SteerSpec/strspc-manager/src/realmresolve"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/rulediff"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/rulelint"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
	"github.com/SteerSpec/strspc-manager/src/schema"
)

// asvsRef is a sound Standard Reference.
var asvsRef = entity.StandardRef{ID: "owasp.asvs", Relation: entity.RelationImplements}

// standardsFile is a sound entity declaring ref, with a `standard` note on its
// rule for each content.
func standardsFile(ref entity.StandardRef, contents ...string) *entity.File {
	f := &entity.File{
		Schema:  "./_schema/entity.v1.schema.json",
		Entity:  entity.Entity{ID: "STDNOTE", Title: "Standards", Standards: []entity.StandardRef{ref}},
		RuleSet: entity.RuleSet{Version: "0.1.0", Timestamp: "2026-01-01T00:00:00Z"},
		Rules: []entity.Rule{{
			ID: "STDNOTE-001", State: entity.StatePublished, Body: "The system MUST work.",
			AddedBy: "@qa", AddedAt: "2026-01-01",
		}},
	}
	for i, c := range contents {
		f.Notes = append(f.Notes, entity.Note{
			ID: fmt.Sprintf("STDNOTE-001/%02d", i+1), RuleRef: "STDNOTE-001", Type: entity.NoteTypeStandard,
			Content: c, AddedBy: "@qa", AddedAt: "2026-01-01",
		})
	}
	return f
}

// writeQAJSON writes v to path.
func writeQAJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// sound is an entity file every module accepts.
const sound = "testdata/broken/rule-sources/realm/SOURCED.json"

// soundRealm is a Realm with one entity and nothing wrong with it.
const soundRealm = "testdata/broken/rule-sources/realm"

// cleanRealm reaches every cancellation point: it has a dependency, a sub-realm,
// inheritance and notes.
const cleanRealm = "testdata/clean/realm"

// goCase is a diagnostic no file on disk can provoke: a nil input, a cancelled
// context, a failed provider, a file the test makes unreadable. want is the
// exact list of `code severity` pairs, sorted.
type goCase struct {
	name string
	want []string
	run  func(t *testing.T) *result.Result
}

// goCases covers what the scenarios under testdata/broken cannot. The
// completeness test reads the codes from want.
var goCases = []goCase{
	{
		name: "rule-lint reports a bad standards entry",
		want: []string{"RL026 error"},
		run: func(*testing.T) *result.Result {
			f := standardsFile(entity.StandardRef{ID: "owasp", Relation: entity.RelationImplements})
			return rulelint.New().LintFile(f)
		},
	},
	{
		name: "rule-lint reports a malformed standard note",
		want: []string{"RL027 error"},
		run: func(*testing.T) *result.Result {
			return rulelint.New().LintFile(standardsFile(asvsRef, "no mapping"))
		},
	},
	{
		name: "rule-lint reports a duplicate standard note",
		want: []string{"RL028 error"},
		run: func(*testing.T) *result.Result {
			return rulelint.New().LintFile(standardsFile(asvsRef, "owasp.asvs:1", "owasp.asvs:1"))
		},
	},
	{
		name: "rule-lint reports an undeclared standard",
		want: []string{"RL002 info", "RL029 error"},
		run: func(t *testing.T) *result.Result {
			dir := t.TempDir()
			f := standardsFile(entity.StandardRef{}, "owasp.asvs:1")
			f.Entity.Standards = nil
			writeQAJSON(t, filepath.Join(dir, "STDNOTE.json"), f)
			writeQAJSON(t, filepath.Join(dir, "realm.json"), entity.RealmFile{Realm: entity.RealmMeta{ID: "dev.qa.std", Title: "S", Version: "1.0.0"}})
			return rulelint.New().LintRealm(t.Context(), dir)
		},
	},
	{
		name: "realm-lint reports a bad realm standards entry",
		want: []string{"RM002 info", "RM003 error", "RM005 info", "RM018 error"},
		run: func(t *testing.T) *result.Result {
			dir := t.TempDir()
			writeQAJSON(t, filepath.Join(dir, "realm.json"), entity.RealmFile{Realm: entity.RealmMeta{
				ID: "dev.qa.std", Title: "S", Version: "1.0.0",
				Standards: []entity.StandardRef{{ID: "owasp", Relation: entity.RelationImplements}},
			}})
			return realmlint.New().Lint(t.Context(), dir)
		},
	},
	{
		name: "rule-diff reports a revision moved without a body change",
		want: []string{"RD021 error"},
		run: func(*testing.T) *result.Result {
			base := standardsFile(asvsRef)
			head := standardsFile(asvsRef)
			head.RuleSet.Version, head.RuleSet.Timestamp = "0.2.0", "2026-02-01T00:00:00Z"
			head.Rules[0].Revision = 1
			return rulediff.New().Diff(base, head)
		},
	},
	{
		name: "rule-diff reports a standard note retyped past reference",
		want: []string{"RD013 error", "RD022 error"},
		run: func(*testing.T) *result.Result {
			base := standardsFile(asvsRef, "owasp.asvs:1")
			head := standardsFile(asvsRef, "owasp.asvs:1")
			head.RuleSet.Version, head.RuleSet.Timestamp = "0.2.0", "2026-02-01T00:00:00Z"
			head.Notes[0].Type = "rationale"
			return rulediff.New().Diff(base, head)
		},
	},
	{
		name: "rule-lint reports a nil file",
		want: []string{"RL000 error"},
		run: func(*testing.T) *result.Result {
			return rulelint.New().LintFile(nil)
		},
	},
	{
		name: "rule-lint reports a missing realm directory",
		want: []string{"RL000 error"},
		run: func(t *testing.T) *result.Result {
			return rulelint.New().LintRealm(t.Context(), filepath.Join(t.TempDir(), "absent"))
		},
	},
	{
		name: "rule-lint reports an unreadable file",
		want: []string{"RL000 error"},
		run: func(t *testing.T) *result.Result {
			return rulelint.New().LintRealm(t.Context(), unreadableEntity(t))
		},
	},
	{
		name: "rule-lint without a schema fetcher skips RL002",
		want: []string{"RL002 info"},
		run: func(t *testing.T) *result.Result {
			return rulelint.New().LintBytes(t.Context(), readFixture(t, sound))
		},
	},
	{
		name: "rule-lint reports a schema it cannot fetch",
		want: []string{"RL002 error"},
		run: func(t *testing.T) *result.Result {
			l := rulelint.New(rulelint.WithSchemaFetcher(deadSchemaFetcher(t)))
			return l.LintBytes(t.Context(), readFixture(t, sound))
		},
	},
	{
		name: "realm-lint without fetcher or linter skips RM002 and RM005",
		want: []string{"RM002 info", "RM005 info"},
		run: func(t *testing.T) *result.Result {
			return realmlint.New().Lint(t.Context(), soundRealm)
		},
	},
	{
		name: "realm-lint reports a schema it cannot fetch",
		want: []string{"RM002 error", "RM005 info"},
		run: func(t *testing.T) *result.Result {
			return realmlint.New(realmlint.WithSchemaFetcher(deadSchemaFetcher(t))).Lint(t.Context(), soundRealm)
		},
	},
	{
		name: "realm-lint delegates entity files to its rule linter",
		want: []string{"RL001 error", "RL001 error", "RL002 error", "RL003 error"},
		run: func(t *testing.T) *result.Result {
			fetcher := schemaFetcher(t)
			l := realmlint.New(
				realmlint.WithSchemaFetcher(fetcher),
				realmlint.WithRuleLinter(rulelint.New(rulelint.WithSchemaFetcher(fetcher))),
			)
			return l.Lint(t.Context(), "testdata/broken/parse-errors/realm")
		},
	},
	{
		name: "realm-lint reports an unreadable file",
		want: []string{"RM002 info", "RM005 error", "RM005 info"},
		run: func(t *testing.T) *result.Result {
			return realmlint.New().Lint(t.Context(), unreadableEntity(t))
		},
	},
	{
		name: "realm-resolve stops on a cancelled context",
		want: []string{"RR000 error"},
		run: func(t *testing.T) *result.Result {
			_, res := realmresolve.New().Resolve(cancelled(t), &entity.RealmFile{}, soundRealm)
			return res
		},
	},
	{
		name: "realm-resolve stops at every cancellation point",
		want: []string{"RR000 error"},
		run: func(t *testing.T) *result.Result {
			rf, err := entity.LoadRealm(filepath.Join(cleanRealm, "realm.json"))
			if err != nil {
				t.Fatal(err)
			}
			return sweepCancellation(t, "RR000", func(ctx context.Context) *result.Result {
				_, res := realmresolve.New().Resolve(ctx, rf, cleanRealm)
				return res
			})
		},
	},
	{
		name: "realm-resolve reports a missing realm directory",
		want: []string{"RR002 error"},
		run: func(t *testing.T) *result.Result {
			_, res := realmresolve.New().Resolve(t.Context(), &entity.RealmFile{}, filepath.Join(t.TempDir(), "absent"))
			return res
		},
	},
	{
		name: "realm-resolve reports a source that leaves the origin repository",
		want: []string{"RR001 error"},
		run: func(t *testing.T) *result.Result {
			origin := remote.Ref{Owner: "acme", Repo: "rules", Ref: "v1.0.0", Path: "rules/cli"}
			rf := manifestWith(entity.RealmDep{RealmID: "dev.steerspec.qa.far", Version: "1.0.0", Source: "../../../outside"})
			_, res := realmresolve.New(realmresolve.WithOrigin(origin)).Resolve(t.Context(), rf, soundRealm)
			return res
		},
	},
	{
		name: "realm-resolve reports an unreadable entity file",
		want: []string{"RR002 error"},
		run: func(t *testing.T) *result.Result {
			_, res := realmresolve.New().Resolve(t.Context(), &entity.RealmFile{}, unreadableEntity(t))
			return res
		},
	},
	{
		name: "realm-resolve needs a fetcher for a remote source",
		want: []string{"RR012 error"},
		run: func(t *testing.T) *result.Result {
			rf := manifestWith(entity.RealmDep{RealmID: "dev.steerspec.qa.far", Version: "1.0.0", Source: "github://acme/rules@v1.0.0/core"})
			_, res := realmresolve.New().Resolve(t.Context(), rf, soundRealm)
			return res
		},
	},
	{
		name: "rule-resolve stops on a cancelled context",
		want: []string{"RSV000 error"},
		run: func(t *testing.T) *result.Result {
			r, err := ruleresolve.New([]ruleresolve.SourceEntry{{Source: soundRealm}})
			if err != nil {
				t.Fatal(err)
			}
			_, res := r.Resolve(cancelled(t))
			return res
		},
	},
	{
		name: "rule-resolve stops at every cancellation point",
		want: []string{"RSV000 error"},
		run: func(t *testing.T) *result.Result {
			r, err := ruleresolve.New([]ruleresolve.SourceEntry{{Source: cleanRealm}, {Source: "testdata/clean/deps/shared"}})
			if err != nil {
				t.Fatal(err)
			}
			return sweepCancellation(t, "RSV000", func(ctx context.Context) *result.Result {
				_, res := r.Resolve(ctx)
				return res
			})
		},
	},
	{
		name: "rule-resolve stops walking a source on a cancelled context",
		want: []string{"RSV000 error"},
		run: func(t *testing.T) *result.Result {
			_, res := (&ruleresolve.LocalSource{}).Fetch(cancelled(t), soundRealm)
			return res
		},
	},
	{
		name: "rule-resolve reports an unreadable file",
		want: []string{"RSV002 error"},
		run: func(t *testing.T) *result.Result {
			_, res := (&ruleresolve.LocalSource{}).Fetch(t.Context(), unreadableEntity(t))
			return res
		},
	},
	{
		name: "rule-resolve reports a github source that does not parse",
		want: []string{"RSV007 error"},
		run: func(t *testing.T) *result.Result {
			_, res := (&ruleresolve.GitHubSource{}).Fetch(t.Context(), "github://not-a-reference")
			return res
		},
	},
	{
		name: "rule-resolve reports a github source without a fetcher",
		want: []string{"RSV007 error"},
		run: func(t *testing.T) *result.Result {
			_, res := (&ruleresolve.GitHubSource{}).Fetch(t.Context(), "github://acme/rules@v1.0.0/core")
			return res
		},
	},
	{
		name: "rule-resolve reports a failed download",
		want: []string{"RSV007 error"},
		run: func(t *testing.T) *result.Result {
			r, err := ruleresolve.New(
				[]ruleresolve.SourceEntry{{Source: "github://acme/rules@v1.0.0/core"}},
				ruleresolve.WithRemoteFetcher(remoteFetcher(t)),
			)
			if err != nil {
				t.Fatal(err)
			}
			_, res := r.Resolve(t.Context())
			return res
		},
	},
	{
		name: "rule-diff reports nil files",
		want: []string{"RD000 error"},
		run: func(*testing.T) *result.Result {
			return rulediff.New().Diff(nil, nil)
		},
	},
	{
		name: "rule-diff reports a nil new file",
		want: []string{"RD000 error"},
		run: func(*testing.T) *result.Result {
			return rulediff.New().DiffNew(nil)
		},
	},
	{
		name: "rule-diff reports an unreadable file",
		want: []string{"RD000 error"},
		run: func(t *testing.T) *result.Result {
			return rulediff.CompareDir(t.TempDir(), unreadableEntity(t))
		},
	},
	{
		// RD005 is realm.json, which the unreadable Realm holds and head lacks.
		name: "rule-diff reports an unreadable base file",
		want: []string{"RD000 error", "RD005 error"},
		run: func(t *testing.T) *result.Result {
			head := t.TempDir()
			if err := os.WriteFile(filepath.Join(head, "SOURCED.json"), readFixture(t, sound), 0o644); err != nil {
				t.Fatal(err)
			}
			return rulediff.CompareDir(unreadableEntity(t), head)
		},
	},
	{
		name: "rule-eval reports a failing provider",
		want: []string{"RE002 warning"},
		run: func(t *testing.T) *result.Result {
			return evaluate(t, providerFunc(func() (*ruleeval.EvalResponse, error) {
				return nil, errors.New("provider down")
			}))
		},
	},
	{
		name: "rule-eval reports a provider that returns nothing",
		want: []string{"RE002 warning"},
		run: func(t *testing.T) *result.Result {
			return evaluate(t, providerFunc(func() (*ruleeval.EvalResponse, error) {
				return nil, nil // the failure under test
			}))
		},
	},
	{
		name: "rule-eval reports an unrecognized verdict",
		want: []string{"RE002 warning"},
		run: func(t *testing.T) *result.Result {
			return evaluate(t, providerFunc(func() (*ruleeval.EvalResponse, error) {
				return &ruleeval.EvalResponse{Verdict: "maybe"}, nil
			}))
		},
	},
	{
		name: "rule-eval reports a batch without the rule's verdict",
		want: []string{"RE002 warning"},
		run: func(t *testing.T) *result.Result {
			return evaluate(t, batchFunc(func() (*ruleeval.BatchResponse, error) {
				return &ruleeval.BatchResponse{}, nil
			}))
		},
	},
	{
		name: "rule-eval reports a partly failed batch",
		want: []string{"RE002 warning"},
		run: func(t *testing.T) *result.Result {
			return evaluate(t, batchFunc(func() (*ruleeval.BatchResponse, error) {
				return &ruleeval.BatchResponse{
					Verdicts: map[string]*ruleeval.EvalResponse{"QA-001": {Verdict: ruleeval.Compliant}},
					Errors:   []error{errors.New("hunk too large to send")},
				}, nil
			}))
		},
	},
	{
		name: "eval-cache recreates a corrupt file",
		want: []string{"EC001 info"},
		run: func(t *testing.T) *result.Result {
			path := filepath.Join(t.TempDir(), "cache.db")
			if err := os.WriteFile(path, []byte(strings.Repeat("not a database ", 1024)), 0o600); err != nil {
				t.Fatal(err)
			}
			store, err := evalcache.Open(t.Context(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = store.Close() })
			return store.Diagnostics()
		},
	},
}

func TestGoCases(t *testing.T) {
	for _, tt := range goCases {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, d := range tt.run(t).Diagnostics {
				got = append(got, d.Code+" "+d.Severity.String())
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// cancelled returns a context that is already cancelled.
func cancelled(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

// countdownContext is cancelled once Err has been called budget times, which
// lands a cancellation on each ctx.Err() check of a run in turn, without timing.
type countdownContext struct {
	context.Context
	budget *atomic.Int64
}

func (c countdownContext) Err() error {
	if c.budget.Add(-1) < 0 {
		return context.Canceled
	}
	return nil
}

// sweepCancellation runs fn with ever more budget until it finishes without a
// diagnostic, and requires code alone from each run it cut short. It returns
// code as one diagnostic, for goCase.want.
func sweepCancellation(t *testing.T, code string, fn func(context.Context) *result.Result) *result.Result {
	t.Helper()
	const maxChecks = 10000
	for n := range int64(maxChecks) {
		var budget atomic.Int64
		budget.Store(n)
		res := fn(countdownContext{Context: t.Context(), budget: &budget})
		if len(res.Diagnostics) == 0 {
			if n == 0 {
				t.Fatal("no cancellation reached: the run never checked its context")
			}
			return &result.Result{Diagnostics: []result.Diagnostic{{Code: code, Severity: result.Error}}}
		}
		for _, d := range res.Diagnostics {
			if d.Code != code {
				t.Errorf("cancelled after %d checks: %s", n, d)
			}
		}
	}
	t.Fatalf("the run still reports after %d checks", maxChecks)
	return nil
}

func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// manifestWith returns a manifest of soundRealm that declares dep.
func manifestWith(dep entity.RealmDep) *entity.RealmFile {
	rf := &entity.RealmFile{Dependencies: []entity.RealmDep{dep}}
	rf.Realm.ID = "dev.steerspec.qa.sources"
	rf.Realm.Version = "1.0.0"
	return rf
}

// deadSchemaFetcher has no local schemas, so each fetch ends at offlineClient.
func deadSchemaFetcher(t *testing.T) *schema.Fetcher {
	t.Helper()
	return schema.New(schema.WithCacheDir(t.TempDir()), schema.WithBaseURL(deadBaseURL), schema.WithClient(offlineClient()))
}

// unreadableEntity returns a directory holding a Realm whose one entity file
// cannot be read. It skips the test when permissions don't bind, as for root.
func unreadableEntity(t *testing.T) string {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not bind root")
	}
	dir := t.TempDir()
	for _, name := range []string{"realm.json", "_schema/entity.v1.schema.json"} {
		target := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, readFixture(t, filepath.Join(soundRealm, name)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(dir, "SOURCED.json")
	if err := os.WriteFile(target, readFixture(t, sound), 0o000); err != nil {
		t.Fatal(err)
	}
	return dir
}

// evaluate runs one Draft rule of entity QA through p.
func evaluate(t *testing.T, p ruleeval.Provider) *result.Result {
	t.Helper()
	e, err := ruleeval.New(p)
	if err != nil {
		t.Fatal(err)
	}
	inputs := []ruleeval.RuleInput{{Rule: entity.Rule{ID: "QA-001", State: entity.StateDraft}, EntityID: "QA"}}
	return e.Evaluate(t.Context(), inputs, defaultDiff)
}

// providerFunc is a Provider that answers every rule the same way.
type providerFunc func() (*ruleeval.EvalResponse, error)

func (f providerFunc) Evaluate(context.Context, ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	return f()
}

// batchFunc is a BatchProvider that answers every entity the same way.
type batchFunc func() (*ruleeval.BatchResponse, error)

func (f batchFunc) Evaluate(context.Context, ruleeval.EvalRequest) (*ruleeval.EvalResponse, error) {
	return nil, errors.New("the evaluator must batch")
}

func (f batchFunc) EvaluateBatch(context.Context, ruleeval.BatchRequest) (*ruleeval.BatchResponse, error) {
	return f()
}
