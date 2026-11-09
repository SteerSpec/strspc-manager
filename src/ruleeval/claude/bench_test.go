//go:build bench

package claude_test

// TestBench compares models and efforts for the CI rule check (`strspc check`)
// on two case sets, calling the real Anthropic API:
//
//   - labelled: small diffs, each written to break, keep or not touch one
//     rule of this repository whose answer can be checked mechanically. Each
//     case is judged with all the Draft, Published and Implemented rules of
//     that rule's entity, as `check` sends them, and only the labelled rule
//     is graded. Violated is the positive class, since it is what blocks a
//     merge.
//   - prs: the diffs of the latest commits of this branch, judged against
//     every rule `check` sends (core at the pin in .strspc/config.yaml and
//     ./rules/), one request group per entity. They have no labels: they
//     give the cost of a real run and how far each config agrees with the
//     first one.
//
// Run from the repository root:
//
//	STRSPC_BENCH_DRY=1 go test -tags bench ./src/ruleeval/claude/ -run TestBench -v   # no API call
//	go test -tags bench ./src/ruleeval/claude/ -run TestBench -v -timeout 3h          # needs ANTHROPIC_API_KEY
//
// Environment:
//
//	STRSPC_BENCH_DRY      1: list the cases and estimate the input, call nothing
//	STRSPC_BENCH_OUT      output directory (default: a new one under the temp dir);
//	                      a rerun into the same directory skips the calls already made
//	STRSPC_BENCH_CONFIGS  comma-separated model/effort pairs, the first being the
//	                      baseline (default: sonnet-5-5/medium,haiku-5-5/medium,
//	                      haiku-5-5/high,haiku-5-5/xhigh)
//	STRSPC_BENCH_REPS     runs of each labelled case per config (default 3)
//	STRSPC_BENCH_PRS      commits in the prs set (default 8, 0 to skip it)
//	STRSPC_BENCH_SETS     labelled, prs or both (default both)
//	STRSPC_BENCH_WORKERS  concurrent jobs (default 4)
//
// It writes results.jsonl (one row per call), cases.md (the labelled cases,
// for review) and report.md (the comparison) into the output directory.

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
	"github.com/SteerSpec/strspc-manager/src/ruleresolve"
)

// benchConfig is one model and effort under comparison.
type benchConfig struct{ Model, Effort string }

// String names c as model/effort without the "claude-" prefix, as reports
// and STRSPC_BENCH_CONFIGS write it.
func (c benchConfig) String() string { return strings.TrimPrefix(c.Model, "claude-") + "/" + c.Effort }

// price is the first-party API price in USD per million tokens, for prompts up
// to 100K tokens: the provider keeps each request under its input budget
// (claude.DefaultMaxInputTokens). Cache writes cost 1.25x input, reads 0.1x.
var prices = map[string]struct{ In, Out float64 }{
	"claude-sonnet-5-5": {2.00, 10.00},
	"claude-haiku-5-5":  {0.10, 0.50},
	"claude-opus-5-5":   {4.00, 20.00},
}

func cost(model string, u ruleeval.Usage) float64 {
	p, ok := prices[model]
	if !ok {
		return 0
	}
	// InputTokens excludes the cached tokens, which the API reports apart.
	return (float64(u.InputTokens)*p.In + float64(u.CacheWriteTokens)*p.In*1.25 +
		float64(u.CacheReadTokens)*p.In*0.1 + float64(u.OutputTokens)*p.Out) / 1e6
}

// labelled is one hand-written case: diff breaks, keeps or doesn't touch rule.
type labelled struct {
	ID    string
	Rule  string
	Label ruleeval.Verdict
	Why   string
	Diff  string
}

// newFile renders a unified diff adding path with lines.
func newFile(path string, lines ...string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode 100644\nindex 0000000..1111111\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1,%d @@\n", path, path, path, len(lines))
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

// appendTo renders a unified diff adding lines to path after context, which
// ends at line at.
func appendTo(path string, at int, context []string, lines ...string) string {
	var b strings.Builder
	start := at - len(context) + 1
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nindex 2222222..3333333 100644\n--- a/%s\n+++ b/%s\n@@ -%d,%d +%d,%d @@\n",
		path, path, path, path, start, len(context), start, len(context)+len(lines))
	for _, l := range context {
		b.WriteString(" " + l + "\n")
	}
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	return b.String()
}

// The not_applicable cases each touch none of their rule's subject, and no
// two send the same request.
var (
	workflowChange = appendTo(".github/workflows/ci.yml", 30, []string{"      - uses: actions/checkout@v7", "      - uses: actions/setup-go@v6", "        with:"},
		"          cache: true")
	goModBump = "diff --git a/go.mod b/go.mod\nindex 4444444..5555555 100644\n--- a/go.mod\n+++ b/go.mod\n@@ -10,3 +10,3 @@ require (\n" +
		" \tgithub.com/google/go-cmp v0.7.0\n-\tgithub.com/santhosh-tekuri/jsonschema/v6 v6.0.1\n+\tgithub.com/santhosh-tekuri/jsonschema/v6 v6.0.2\n \tgithub.com/spf13/cobra v1.10.1\n"
	docsEdit = "diff --git a/docs/SPEC.md b/docs/SPEC.md\nindex 6666666..7777777 100644\n--- a/docs/SPEC.md\n+++ b/docs/SPEC.md\n@@ -341,1 +341,1 @@\n" +
		"-When an Implemented rule needs to change, the author creates a new rule with a `supersedes` field pointing to the original.\n" +
		"+When an Implemented rule must change, its author adds a new rule whose `supersedes` field names the original.\n"
	testTableChange = "diff --git a/src/entity/state_test.go b/src/entity/state_test.go\nindex 8888888..9999999 100644\n--- a/src/entity/state_test.go\n+++ b/src/entity/state_test.go\n@@ -20,3 +20,4 @@ func TestValidateTransition(t *testing.T) {\n" +
		" \t\t{\"draft to published\", StateDraft, StatePublished, false},\n \t\t{\"draft to abandoned\", StateDraft, StateAbandoned, false},\n" +
		"+\t\t{\"published to draft\", StatePublished, StateDraft, true},\n \t}\n"
)

// labelledCases are the labelled set. Each rule has a violated, a compliant
// and a not_applicable case.
var labelledCases = []labelled{
	{
		ID: "goconv004-violated", Rule: "GOCONV-004", Label: ruleeval.Violated,
		Why: "the error carries a cause but is formatted with %v",
		Diff: newFile("src/manifest/load.go", "package manifest", "", "import (", "\t\"fmt\"", "\t\"os\"", ")", "",
			"// Load reads the manifest at path.", "func Load(path string) ([]byte, error) {", "\tdata, err := os.ReadFile(path)",
			"\tif err != nil {", "\t\treturn nil, fmt.Errorf(\"reading manifest %s: %v\", path, err)", "\t}", "\treturn data, nil", "}"),
	},
	{
		ID: "goconv004-compliant", Rule: "GOCONV-004", Label: ruleeval.Compliant,
		Why: "the cause is wrapped with %w",
		Diff: newFile("src/manifest/load.go", "package manifest", "", "import (", "\t\"fmt\"", "\t\"os\"", ")", "",
			"// Load reads the manifest at path.", "func Load(path string) ([]byte, error) {", "\tdata, err := os.ReadFile(path)",
			"\tif err != nil {", "\t\treturn nil, fmt.Errorf(\"reading manifest %s: %w\", path, err)", "\t}", "\treturn data, nil", "}"),
	},
	{ID: "goconv004-na", Rule: "GOCONV-004", Label: ruleeval.NotApplicable, Why: "a CI workflow change", Diff: workflowChange},
	{
		ID: "goconv005-violated", Rule: "GOCONV-005", Label: ruleeval.Violated,
		Why: "an exported function does an HTTP request without a context.Context",
		Diff: newFile("src/remote/index.go", "package remote", "", "import (", "\t\"fmt\"", "\t\"io\"", "\t\"net/http\"", ")", "",
			"// FetchIndex downloads the release index at url.", "func FetchIndex(url string) ([]byte, error) {",
			"\tresp, err := http.Get(url)", "\tif err != nil {", "\t\treturn nil, fmt.Errorf(\"fetching index: %w\", err)", "\t}",
			"\tdefer resp.Body.Close()", "\treturn io.ReadAll(resp.Body)", "}"),
	},
	{
		ID: "goconv005-compliant", Rule: "GOCONV-005", Label: ruleeval.Compliant,
		Why: "the HTTP request takes a context.Context first",
		Diff: newFile("src/remote/index.go", "package remote", "", "import (", "\t\"context\"", "\t\"fmt\"", "\t\"io\"", "\t\"net/http\"", ")", "",
			"// FetchIndex downloads the release index at url.", "func FetchIndex(ctx context.Context, url string) ([]byte, error) {",
			"\treq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)", "\tif err != nil {",
			"\t\treturn nil, fmt.Errorf(\"building request: %w\", err)", "\t}", "\tresp, err := http.DefaultClient.Do(req)", "\tif err != nil {",
			"\t\treturn nil, fmt.Errorf(\"fetching index: %w\", err)", "\t}", "\tdefer resp.Body.Close()", "\treturn io.ReadAll(resp.Body)", "}"),
	},
	{
		ID: "goconv005-na", Rule: "GOCONV-005", Label: ruleeval.NotApplicable,
		Why: "a pure helper does no network or long-running work",
		Diff: newFile("src/entity/euid.go", "package entity", "",
			"// IsShortEUID reports whether id is an EUID with no realm qualifier.", "func IsShortEUID(id string) bool {",
			"\tfor _, r := range id {", "\t\tif r == ':' {", "\t\t\treturn false", "\t\t}", "\t}", "\treturn id != \"\"", "}"),
	},
	{
		ID: "goconv007-violated", Rule: "GOCONV-007", Label: ruleeval.Violated,
		Why: "an exported function has no doc comment",
		Diff: newFile("src/entity/slug.go", "package entity", "", "import \"strings\"", "",
			"func Slug(id string) string {", "\treturn strings.ReplaceAll(strings.ToLower(id), \".\", \"-\")", "}"),
	},
	{
		ID: "goconv007-compliant", Rule: "GOCONV-007", Label: ruleeval.Compliant,
		Why: "the exported function's comment starts with its name",
		Diff: newFile("src/entity/slug.go", "package entity", "", "import \"strings\"", "",
			"// Slug lowercases id and replaces dots with dashes.", "func Slug(id string) string {",
			"\treturn strings.ReplaceAll(strings.ToLower(id), \".\", \"-\")", "}"),
	},
	{ID: "goconv007-na", Rule: "GOCONV-007", Label: ruleeval.NotApplicable, Why: "a go.mod dependency bump", Diff: goModBump},
	{
		ID: "mod001-violated", Rule: "MOD-001", Label: ruleeval.Violated,
		Why: "a lint check prints a warning instead of adding a Diagnostic",
		Diff: newFile("src/rulelint/title.go", "package rulelint", "", "import (", "\t\"fmt\"", "", "\t\"github.com/SteerSpec/strspc-manager/src/entity\"", ")", "",
			"// checkTitle warns about an entity with an empty title.", "func checkTitle(f *entity.File) {", "\tif f.Entity.Title == \"\" {",
			"\t\tfmt.Printf(\"warning: entity %s has no title\\n\", f.Entity.ID)", "\t}", "}"),
	},
	{
		ID: "mod001-compliant", Rule: "MOD-001", Label: ruleeval.Compliant,
		Why: "the finding is added to the result as a Diagnostic",
		Diff: newFile("src/rulelint/title.go", "package rulelint", "", "import (", "\t\"github.com/SteerSpec/strspc-manager/src/entity\"",
			"\t\"github.com/SteerSpec/strspc-manager/src/result\"", ")", "",
			"// checkTitle reports an entity with an empty title.", "func checkTitle(f *entity.File, res *result.Result) {", "\tif f.Entity.Title == \"\" {",
			"\t\tres.Add(result.Diagnostic{Module: module, Code: \"RL099\", Severity: result.Warning, Message: \"entity \" + f.Entity.ID + \" has no title\"})",
			"\t}", "}"),
	},
	{ID: "mod001-na", Rule: "MOD-001", Label: ruleeval.NotApplicable, Why: "a wording change in docs/SPEC.md", Diff: docsEdit},
	{
		ID: "mod005-violated", Rule: "MOD-005", Label: ruleeval.Violated,
		Why: "a manager package imports strspc-CLI",
		Diff: newFile("src/render/config.go", "package render", "", "import (",
			"\tclicfg \"github.com/SteerSpec/strspc-CLI/src/config\"", ")", "",
			"// Theme returns the theme the CLI configuration names.", "func Theme(c *clicfg.Config) string {", "\treturn c.Render.Theme", "}"),
	},
	{
		ID: "mod005-compliant", Rule: "MOD-005", Label: ruleeval.Compliant,
		Why: "the only import is the manager's own package",
		Diff: newFile("src/render/config.go", "package render", "", "import (",
			"\t\"github.com/SteerSpec/strspc-manager/src/entity\"", ")", "",
			"// Title returns the title of the entity f.", "func Title(f *entity.File) string {", "\treturn f.Entity.Title", "}"),
	},
	{ID: "mod005-na", Rule: "MOD-005", Label: ruleeval.NotApplicable, Why: "a test table gains a case, imports unchanged", Diff: testTableChange},
	{
		ID: "mod006-violated", Rule: "MOD-006", Label: ruleeval.Violated,
		Why: "a new diagnostic code RL099 with no docs/SPEC.md change",
		Diff: newFile("src/rulelint/title.go", "package rulelint", "", "import (", "\t\"github.com/SteerSpec/strspc-manager/src/entity\"",
			"\t\"github.com/SteerSpec/strspc-manager/src/result\"", ")", "",
			"// checkTitle reports an entity with an empty title (RL099).", "func checkTitle(f *entity.File, res *result.Result) {", "\tif f.Entity.Title == \"\" {",
			"\t\tres.Add(result.Diagnostic{Module: module, Code: \"RL099\", Severity: result.Warning, Message: \"entity \" + f.Entity.ID + \" has no title\"})",
			"\t}", "}"),
	},
	{
		ID: "mod006-compliant", Rule: "MOD-006", Label: ruleeval.Compliant,
		Why: "the new code RL099 gets a row in docs/SPEC.md",
		Diff: newFile("src/rulelint/title.go", "package rulelint", "", "import (", "\t\"github.com/SteerSpec/strspc-manager/src/entity\"",
			"\t\"github.com/SteerSpec/strspc-manager/src/result\"", ")", "",
			"// checkTitle reports an entity with an empty title (RL099).", "func checkTitle(f *entity.File, res *result.Result) {", "\tif f.Entity.Title == \"\" {",
			"\t\tres.Add(result.Diagnostic{Module: module, Code: \"RL099\", Severity: result.Warning, Message: \"entity \" + f.Entity.ID + \" has no title\"})",
			"\t}", "}") +
			appendTo("docs/SPEC.md", 559, []string{"| RL041 | Error | A rule of a Standard Realm that supersedes another is past Draft and not Abandoned, while the rule it supersedes is not Terminated (STDRLM-013). |"},
				"| RL099 | Warning | An entity's `title` is empty. |"),
	},
	{
		ID: "mod006-na", Rule: "MOD-006", Label: ruleeval.NotApplicable,
		Why: "a helper that emits no diagnostic code",
		Diff: newFile("src/result/sort.go", "package result", "", "import \"slices\"", "",
			"// SortByCode orders diagnostics by code, keeping the order of equal codes.", "func SortByCode(ds []Diagnostic) {",
			"\tslices.SortStableFunc(ds, func(a, b Diagnostic) int {", "\t\tswitch {", "\t\tcase a.Code < b.Code:", "\t\t\treturn -1",
			"\t\tcase a.Code > b.Code:", "\t\t\treturn 1", "\t\t}", "\t\treturn 0", "\t})", "}"),
	},
}

// row is one call's result, written to results.jsonl as it completes.
type row struct {
	Set     string                      `json:"set"`
	Case    string                      `json:"case"`
	Config  string                      `json:"config"`
	Model   string                      `json:"model"`
	Effort  string                      `json:"effort"`
	Rep     int                         `json:"rep"`
	Rule    string                      `json:"rule,omitempty"`
	Label   ruleeval.Verdict            `json:"label,omitempty"`
	Verdict ruleeval.Verdict            `json:"verdict,omitempty"`
	Graded  map[string]ruleeval.Verdict `json:"verdicts,omitempty"`
	Usage   ruleeval.Usage              `json:"usage"`
	CostUSD float64                     `json:"cost_usd"`
	Seconds float64                     `json:"latency_s"`
	Error   string                      `json:"error,omitempty"`
	// Fingerprint identifies what was sent: the diff and the rules. A rerun
	// reuses a row only when it still matches, so a changed case or commit
	// is evaluated again.
	Fingerprint string `json:"fingerprint"`
}

func (r row) key() string { return r.Set + "|" + r.Case + "|" + r.Config + "|" + strconv.Itoa(r.Rep) }

// job is one call to make: one case, one config, one rep, with the request
// groups `check` would send for it.
type job struct {
	set, id string
	cfg     benchConfig
	rep     int
	rule    string
	label   ruleeval.Verdict
	diff    string
	groups  [][]ruleeval.RuleInput
}

// fingerprint hashes the diff and the rules j sends.
func (j job) fingerprint() string {
	h := sha256.New()
	h.Write([]byte(j.diff))
	for _, g := range j.groups {
		for _, in := range g {
			fmt.Fprintf(h, "\x00%s\x00%d\x00%s\x00%s", in.Rule.ID, in.Rule.Revision, in.Rule.State, in.Rule.Body)
			for _, n := range in.Notes {
				fmt.Fprintf(h, "\x00%s\x00%s", n.ID, n.Content)
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func (j job) key() string {
	return row{Set: j.set, Case: j.id, Config: j.cfg.String(), Rep: j.rep}.key()
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func envInt(t *testing.T, name string, def int) int {
	v, err := strconv.Atoi(env(name, strconv.Itoa(def)))
	if err != nil || v < 0 {
		t.Fatalf("%s: want a number, got %q", name, os.Getenv(name))
	}
	return v
}

func TestBench(t *testing.T) {
	dry := os.Getenv("STRSPC_BENCH_DRY") == "1"
	if !dry && os.Getenv(claude.EnvAPIKey) == "" {
		t.Skipf("%s not set (STRSPC_BENCH_DRY=1 runs without it)", claude.EnvAPIKey)
	}
	root := repoRoot(t)
	var configs []benchConfig
	for _, s := range strings.Split(env("STRSPC_BENCH_CONFIGS", "sonnet-5-5/medium,haiku-5-5/medium,haiku-5-5/high,haiku-5-5/xhigh"), ",") {
		model, effort, ok := strings.Cut(strings.TrimSpace(s), "/")
		if !ok {
			t.Fatalf("STRSPC_BENCH_CONFIGS: %q is not model/effort", s)
		}
		c := benchConfig{"claude-" + strings.TrimPrefix(model, "claude-"), effort}
		if _, ok := prices[c.Model]; !ok {
			t.Fatalf("no price for %s: add it to prices, or its cost would read as $0", c.Model)
		}
		configs = append(configs, c)
	}
	reps, prs, workers := envInt(t, "STRSPC_BENCH_REPS", 3), envInt(t, "STRSPC_BENCH_PRS", 8), max(1, envInt(t, "STRSPC_BENCH_WORKERS", 4))
	sets := env("STRSPC_BENCH_SETS", "both")
	if sets != "both" && sets != "labelled" && sets != "prs" {
		t.Fatalf("STRSPC_BENCH_SETS: want labelled, prs or both, got %q", sets)
	}
	out := os.Getenv("STRSPC_BENCH_OUT")
	if out == "" {
		out = filepath.Join(os.TempDir(), "strspc-bench-"+time.Now().Format("20060102-150405"))
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx := t.Context()
	inputs, coreSource := ruleInputs(ctx, t, root)
	byEntity := make(map[string][]ruleeval.RuleInput)
	var entities []string
	for _, in := range inputs {
		if _, ok := byEntity[in.EntityID]; !ok {
			entities = append(entities, in.EntityID)
		}
		byEntity[in.EntityID] = append(byEntity[in.EntityID], in)
	}
	entityOf := make(map[string]string)
	for _, in := range inputs {
		entityOf[in.Rule.ID] = in.EntityID
	}

	var jobs []job
	if sets == "both" || sets == "labelled" {
		writeCasesMarkdown(t, filepath.Join(out, "cases.md"), byEntity, entityOf)
		for _, cfg := range configs {
			for _, c := range labelledCases {
				ent, ok := entityOf[c.Rule]
				if !ok {
					t.Fatalf("case %s: rule %s is not a D, P or I rule of the sources", c.ID, c.Rule)
				}
				for rep := range reps {
					jobs = append(jobs, job{set: "labelled", id: c.ID, cfg: cfg, rep: rep, rule: c.Rule, label: c.Label, diff: c.Diff,
						groups: [][]ruleeval.RuleInput{byEntity[ent]}})
				}
			}
		}
	}
	if (sets == "both" || sets == "prs") && prs > 0 {
		var groups [][]ruleeval.RuleInput
		for _, e := range entities {
			groups = append(groups, byEntity[e])
		}
		for _, c := range recentCommits(t, root, prs) {
			for _, cfg := range configs {
				jobs = append(jobs, job{set: "prs", id: c.title, cfg: cfg, diff: c.diff, groups: groups})
			}
		}
	}

	t.Logf("sources: %s and ./rules/: %d rules in %d entities", coreSource, len(inputs), len(entities))
	counts := make(map[string]int)
	for _, j := range jobs {
		counts[j.set]++
	}
	t.Logf("%d calls over %d configs: %d labelled (%d cases x %d reps), %d on commits; output in %s",
		len(jobs), len(configs), counts["labelled"], len(labelledCases), reps, counts["prs"], out)
	if dry {
		estimate(t, jobs, configs)
		return
	}

	resultsPath := filepath.Join(out, "results.jsonl")
	done := readRows(t, resultsPath)
	f, err := os.OpenFile(resultsPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	// Decide what to run before the workers start, so that only they touch
	// done from here on.
	var todo []job
	for _, j := range jobs {
		if r, ok := done[j.key()]; !ok || r.Fingerprint != j.fingerprint() {
			todo = append(todo, j)
		}
	}
	t.Logf("%d calls to make, %d reused from %s", len(todo), len(jobs)-len(todo), resultsPath)

	var mu sync.Mutex
	var writeErr error
	queue := make(chan job)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for j := range queue {
				r := run(ctx, j)
				data, err := json.Marshal(r)
				mu.Lock()
				done[r.key()] = r
				if err == nil {
					_, err = f.Write(append(data, '\n'))
				}
				if err != nil && writeErr == nil {
					writeErr = fmt.Errorf("recording %s: %w", r.key(), err)
				}
				mu.Unlock()
				if r.Error != "" {
					t.Logf("%s %s %s rep %d: %s", r.Set, r.Case, r.Config, r.Rep, r.Error)
				}
			}
		})
	}
	for _, j := range todo {
		queue <- j
	}
	close(queue)
	wg.Wait()
	if writeErr != nil {
		t.Fatal(writeErr)
	}

	// Report on this run's jobs only: rows of cases or commits no longer in
	// the set, or that no longer match, stay in results.jsonl but out of it.
	var rows []row
	for _, j := range jobs {
		if r, ok := done[j.key()]; ok && r.Fingerprint == j.fingerprint() {
			rows = append(rows, r)
		}
	}
	report := writeReport(rows, configs)
	if err := os.WriteFile(filepath.Join(out, "report.md"), []byte(report), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + report)
}

// run makes the calls of one job on a provider of its own, so its usage is
// its own.
func run(ctx context.Context, j job) row {
	r := row{Set: j.set, Case: j.id, Config: j.cfg.String(), Model: j.cfg.Model, Effort: j.cfg.Effort, Rep: j.rep, Rule: j.rule, Label: j.label,
		Fingerprint: j.fingerprint()}
	p, err := claude.New(claude.Config{Model: j.cfg.Model, Effort: j.cfg.Effort})
	if err != nil {
		r.Error = err.Error()
		return r
	}
	start := time.Now()
	r.Graded = make(map[string]ruleeval.Verdict)
	var errs []string
	for _, g := range j.groups {
		resp, err := p.EvaluateBatch(ctx, ruleeval.BatchRequest{EntityID: g[0].EntityID, Inputs: g, Diff: j.diff})
		if err != nil {
			errs = append(errs, g[0].EntityID+": "+err.Error())
			continue
		}
		for _, e := range resp.Errors {
			errs = append(errs, g[0].EntityID+": "+e.Error())
		}
		for id, v := range resp.Verdicts {
			r.Graded[id] = v.Verdict
		}
	}
	r.Seconds = time.Since(start).Seconds()
	r.Usage = p.Usage()
	r.CostUSD = cost(j.cfg.Model, r.Usage)
	r.Error = strings.Join(errs, "; ")
	if j.set == "labelled" {
		r.Verdict = r.Graded[j.rule]
		if r.Verdict == "" && r.Error == "" {
			r.Error = "no verdict for " + j.rule
		}
		r.Graded = nil
	}
	return r
}

func repoRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		t.Fatalf("finding the repository root: %v", err)
	}
	return strings.TrimSpace(string(out))
}

var corePin = regexp.MustCompile(`(?m)^\s*-\s*source:\s*(github://SteerSpec/strspc-rules@\S+/rules/core)\s*$`)

// ruleInputs resolves the core source pinned in .strspc/config.yaml and
// ./rules/, both global as configured, and returns their D, P and I rules
// (ruleeval.DefaultEvaluateStates) as `check` sends them, inheritance aside.
func ruleInputs(ctx context.Context, t *testing.T, root string) ([]ruleeval.RuleInput, string) {
	t.Helper()
	cfg, err := os.ReadFile(filepath.Join(root, ".strspc", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	m := corePin.FindSubmatch(cfg)
	if m == nil {
		t.Fatal("no core source in .strspc/config.yaml")
	}
	core := string(m[1])
	res, err := ruleresolve.New([]ruleresolve.SourceEntry{
		{Source: core, Scope: ruleresolve.ScopeGlobal},
		{Source: "./rules/", Scope: ruleresolve.ScopeGlobal},
	}, ruleresolve.WithBaseDir(root), ruleresolve.WithCacheDir(t.TempDir()))
	if err != nil {
		t.Fatal(err)
	}
	files, r := res.Resolve(ctx)
	if !r.OK() {
		t.Fatalf("resolving the rule sources: %v", r.Diagnostics)
	}
	var inputs []ruleeval.RuleInput
	var walk func(f *entity.File, src ruleresolve.SourceEntry)
	walk = func(f *entity.File, src ruleresolve.SourceEntry) {
		for _, rule := range f.Rules {
			if !slices.Contains(ruleeval.DefaultEvaluateStates, rule.State) {
				continue
			}
			var notes []entity.Note
			for _, n := range f.Notes {
				if n.RuleRef == rule.ID {
					notes = append(notes, n)
				}
			}
			inputs = append(inputs, ruleeval.RuleInput{Rule: rule, Notes: notes, EntityID: f.Entity.ID, Source: src.Source, Scope: string(src.Scope)})
		}
		for i := range f.SubEntities {
			walk(&f.SubEntities[i], src)
		}
	}
	for _, rf := range files {
		walk(rf.File, rf.Origin)
	}
	return inputs, core
}

type commit struct{ title, diff string }

// recentCommits returns the diffs of the latest n commits of HEAD's first
// parent history that change more than .beads/.
func recentCommits(t *testing.T, root string, n int) []commit {
	t.Helper()
	out, err := exec.Command("git", "-C", root, "log", "--first-parent", "--format=%h %s", "-n", strconv.Itoa(n), "HEAD", "--", ".", ":(exclude).beads").Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	var commits []commit
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		sha, _, _ := strings.Cut(sc.Text(), " ")
		diff, err := exec.Command("git", "-C", root, "show", "--format=", "--no-color", sha, "--", ".", ":(exclude).beads").Output()
		if err != nil {
			t.Fatalf("git show %s: %v", sha, err)
		}
		commits = append(commits, commit{title: sc.Text(), diff: string(diff)})
	}
	return commits
}

func readRows(t *testing.T, path string) map[string]row {
	t.Helper()
	rows := make(map[string]row)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return rows
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var r row
		if line == "" || json.Unmarshal([]byte(line), &r) != nil {
			continue
		}
		// A call that failed is made again.
		if r.Error == "" {
			rows[r.key()] = r
		}
	}
	return rows
}

// estimate prints the input of every call at 2 bytes a token, the ratio the
// provider measured for Go code (chunk.go), plus a fixed system prompt, priced
// at the cache-write rate (1.25x input), the most a prompt token costs. It
// leaves out output tokens, detail requests and retries, so it is a rough
// guide, not a bound: output adds to the real cost and cache reads take from
// it.
func estimate(t *testing.T, jobs []job, configs []benchConfig) {
	const systemTokens = 1500
	perConfig := make(map[string]int)
	for _, j := range jobs {
		for _, g := range j.groups {
			n := len(j.diff) + systemTokens*2
			for _, in := range g {
				n += len(in.Rule.Body) + 120
				for _, note := range in.Notes {
					n += len(note.Content) + 40
				}
			}
			perConfig[j.cfg.String()] += n / 2
		}
	}
	for _, c := range configs {
		tok := perConfig[c.String()]
		p := prices[c.Model]
		t.Logf("%-18s ~%d input tokens, ~$%.2f at the cache-write rate, output not included", c, tok, float64(tok)*p.In*1.25/1e6)
	}
}

func writeCasesMarkdown(t *testing.T, path string, byEntity map[string][]ruleeval.RuleInput, entityOf map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("# Labelled cases\n\n| id | rule | label | why |\n|---|---|---|---|\n")
	for _, c := range labelledCases {
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", c.ID, c.Rule, c.Label, c.Why)
	}
	for _, c := range labelledCases {
		ent := entityOf[c.Rule]
		var body string
		for _, in := range byEntity[ent] {
			if in.Rule.ID == c.Rule {
				body = in.Rule.Body
			}
		}
		fmt.Fprintf(&b, "\n## %s\n\n%s (%s): %s\n\nJudged with the %d D, P and I rules of %s. Label: **%s**, %s.\n\n````diff\n%s````\n",
			c.ID, c.Rule, ent, body, len(byEntity[ent]), ent, c.Label, c.Why, c.Diff)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeReport compares the configs: on the labelled set against the labels,
// on the prs set against the first config.
func writeReport(rows []row, configs []benchConfig) string {
	slices.SortFunc(rows, func(a, b row) int { return strings.Compare(a.key(), b.key()) })
	var b strings.Builder
	b.WriteString("# strspc check benchmark\n\n")
	b.WriteString("## Labelled cases\n\nViolated is the positive class: it is what blocks a merge. A wrong block is a non-violated case judged violated; a miss is a violated case judged otherwise.\n\n")
	b.WriteString("Exact match is secondary: compliant against not_applicable on a diff that only nears the rule's subject is a judgement call the prompt leaves open, and neither blocks.\n\n")
	b.WriteString("| config | wrong blocks | specificity | misses | recall | stable cases | exact match | calls | errors | cost | mean latency |\n|---|---|---|---|---|---|---|---|---|---|---|\n")
	for _, c := range configs {
		var n, errs, exact, tp, fn, tn, fp int
		var usd, secs float64
		verdicts := make(map[string]map[ruleeval.Verdict]bool)
		for _, r := range rows {
			if r.Set != "labelled" || r.Config != c.String() {
				continue
			}
			n++
			usd += r.CostUSD
			secs += r.Seconds
			if r.Error != "" {
				errs++
				continue
			}
			if verdicts[r.Case] == nil {
				verdicts[r.Case] = make(map[ruleeval.Verdict]bool)
			}
			verdicts[r.Case][r.Verdict] = true
			if r.Verdict == r.Label {
				exact++
			}
			switch {
			case r.Label == ruleeval.Violated && r.Verdict == ruleeval.Violated:
				tp++
			case r.Label == ruleeval.Violated:
				fn++
			case r.Verdict == ruleeval.Violated:
				fp++
			default:
				tn++
			}
		}
		stable := 0
		for _, vs := range verdicts {
			if len(vs) == 1 {
				stable++
			}
		}
		if n == 0 {
			continue
		}
		scored := n - errs
		fmt.Fprintf(&b, "| %s | %d | %s | %d | %s | %d/%d | %s | %d | %d | $%.3f | %.1fs |\n", c, fp, ratio(tn, tn+fp), fn,
			ratio(tp, tp+fn), stable, len(verdicts), ratio(exact, scored), n, errs, usd, secs/float64(n))
	}

	b.WriteString("\n### Wrong answers\n\n| config | case | label | verdicts over the reps |\n|---|---|---|---|\n")
	type key struct{ cfg, id string }
	seen := make(map[key][]string)
	var order []key
	for _, r := range rows {
		if r.Set != "labelled" || r.Error != "" || r.Verdict == r.Label {
			continue
		}
		k := key{r.Config, r.Case}
		if _, ok := seen[k]; !ok {
			order = append(order, k)
		}
		seen[k] = append(seen[k], string(r.Verdict))
	}
	for _, k := range order {
		label := ""
		for _, c := range labelledCases {
			if c.ID == k.id {
				label = string(c.Label)
			}
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n", k.cfg, k.id, label, strings.Join(seen[k], ", "))
	}

	base := configs[0].String()
	b.WriteString("\n## Recent commits\n\nNo labels: each config is compared with " + base + ". Cost is per commit, the cost of one CI run on a diff that size.\n\n")
	b.WriteString("| config | commits | errors | mean cost per run | mean latency | violated verdicts | agreement with " + base + " |\n|---|---|---|---|---|---|---|\n")
	baseline := make(map[string]map[string]ruleeval.Verdict)
	for _, r := range rows {
		if r.Set == "prs" && r.Config == base && r.Error == "" {
			baseline[r.Case] = r.Graded
		}
	}
	var disagreements []string
	for _, c := range configs {
		var n, errs, violated, same, compared int
		var usd, secs float64
		for _, r := range rows {
			if r.Set != "prs" || r.Config != c.String() {
				continue
			}
			n++
			usd += r.CostUSD
			secs += r.Seconds
			if r.Error != "" {
				errs++
			}
			for _, id := range slices.Sorted(maps.Keys(r.Graded)) {
				v := r.Graded[id]
				if v == ruleeval.Violated {
					violated++
				}
				bv, ok := baseline[r.Case][id]
				if !ok || c.String() == base {
					continue
				}
				compared++
				if bv == v {
					same++
				} else if bv == ruleeval.Violated || v == ruleeval.Violated {
					disagreements = append(disagreements, fmt.Sprintf("| %s | %s | %s | %s | %s |", c, r.Case, id, bv, v))
				}
			}
		}
		if n == 0 {
			continue
		}
		agree := "baseline"
		if c.String() != base {
			agree = ratio(same, compared)
		}
		fmt.Fprintf(&b, "| %s | %d | %d | $%.3f | %.1fs | %d | %s |\n", c, n, errs, usd/float64(n), secs/float64(n), violated, agree)
	}
	if len(disagreements) > 0 {
		b.WriteString("\n### Disagreements on a violation\n\nVerdicts where one side says violated, for you to judge. Compliant against not_applicable is left out: neither blocks.\n\n")
		b.WriteString("| config | commit | rule | " + base + " | config |\n|---|---|---|---|---|\n")
		b.WriteString(strings.Join(disagreements, "\n") + "\n")
	}
	b.WriteString("\nCosts use first-party prices for prompts up to 100K tokens (cache writes 1.25x input, reads 0.1x). Inheritance is left out of the rule groups, and the provider is this branch's, not the CLI release CI runs.\n")
	return b.String()
}

func ratio(a, b int) string {
	if b == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%% (%d/%d)", 100*float64(a)/float64(b), a, b)
}

// TestBenchReportGrades checks the grading without the API: answers equal to
// the labels score 100%, and answers that never say violated catch nothing.
func TestBenchReportGrades(t *testing.T) {
	cfg := benchConfig{"claude-haiku-5-5", "high"}
	var oracle, null []row
	for _, c := range labelledCases {
		r := row{Set: "labelled", Case: c.ID, Config: cfg.String(), Rule: c.Rule, Label: c.Label}
		r.Verdict = c.Label
		oracle = append(oracle, r)
		r.Verdict = ruleeval.NotApplicable
		null = append(null, r)
	}
	if got := writeReport(oracle, []benchConfig{cfg}); !strings.Contains(got, "| haiku-5-5/high | 0 | 100% (12/12) | 0 | 100% (6/6) | 18/18 | 100% (18/18) |") {
		t.Errorf("oracle report:\n%s", got)
	}
	if got := writeReport(null, []benchConfig{cfg}); !strings.Contains(got, "| haiku-5-5/high | 0 | 100% (12/12) | 6 | 0% (0/6) |") {
		t.Errorf("null report:\n%s", got)
	}
}
