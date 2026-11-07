package ruleeval_test

import (
	"context"
	"errors"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/result"
	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

// auditProvider is a batchProvider that audits, at the given prompt
// version.
type auditProvider struct {
	*batchProvider
	version string
}

// AuditPromptVersion returns the configured version; empty means it cannot audit.
func (a auditProvider) AuditPromptVersion() string { return a.version }

// auditMock is a per-rule mockProvider that audits.
type auditMock struct{ *mockProvider }

// AuditPromptVersion returns a fixed audit prompt version.
func (auditMock) AuditPromptVersion() string { return "audit-test" }

var auditFiles = []ruleeval.File{{Path: "src/cmd/lint.go", Content: "package cmd\n"}, {Path: "src/cmd/sync.go", Content: "package cmd\n"}}

func auditInputs() []ruleeval.RuleInput {
	return []ruleeval.RuleInput{{Rule: makeRule("CMD-001", "I"), EntityID: "CMD"}}
}

// TestAudit_Unsupported checks that Audit refuses a provider that does not
// audit, or forwards one that cannot, and sends nothing.
func TestAudit_Unsupported(t *testing.T) {
	for name, p := range map[string]ruleeval.Provider{
		"no Auditor":    &batchProvider{},
		"empty version": auditProvider{batchProvider: &batchProvider{}},
	} {
		ev, err := ruleeval.New(p)
		if err != nil {
			t.Fatal(err)
		}
		res, err := ev.Audit(context.Background(), auditInputs(), auditFiles)
		if !errors.Is(err, ruleeval.ErrAuditUnsupported) || res != nil {
			t.Errorf("%s: Audit = %v, %v; want ErrAuditUnsupported", name, res, err)
		}
	}
}

// TestAudit_StaticOnly checks a static-only evaluator reports RE003 and
// needs no provider.
func TestAudit_StaticOnly(t *testing.T) {
	ev, err := ruleeval.New(nil, ruleeval.WithStaticOnly(true))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ev.Audit(context.Background(), auditInputs(), auditFiles)
	if err != nil || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != ruleeval.CodeStaticOnly {
		t.Fatalf("Audit = %+v, %v; want one RE003", res, err)
	}
}

// TestAudit_EmptyFileSet checks an empty file set reports RE004 and sends
// nothing.
func TestAudit_EmptyFileSet(t *testing.T) {
	p := auditProvider{batchProvider: &batchProvider{}, version: "audit-test"}
	ev, err := ruleeval.New(p)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ev.Audit(context.Background(), auditInputs(), nil)
	if err != nil || len(res.Diagnostics) != 1 || res.Diagnostics[0].Code != ruleeval.CodeNoDiff || len(p.batches) != 0 {
		t.Fatalf("Audit = %+v, %v (%d requests); want one RE004 and no request", res, err, len(p.batches))
	}
}

// TestAudit_ForwardsFiles checks a batch request carries the files and no
// diff, and that a violation reports RE001 at its first evidence location.
func TestAudit_ForwardsFiles(t *testing.T) {
	p := auditProvider{batchProvider: &batchProvider{responses: map[string]*ruleeval.EvalResponse{
		"CMD-001": {Verdict: ruleeval.Violated, Explanation: "no Long", Evidence: []ruleeval.Location{{File: "src/cmd/lint.go", Line: 5}}},
	}}, version: "audit-test"}
	ev, err := ruleeval.New(p, ruleeval.WithFailOn([]string{"I"}))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ev.Audit(context.Background(), auditInputs(), auditFiles)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.batches) != 1 || p.batches[0].Diff != "" || len(p.batches[0].Files) != 2 || p.batches[0].Files[1].Path != "src/cmd/sync.go" {
		t.Fatalf("batches = %+v, want one request carrying the two files", p.batches)
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %+v", res.Diagnostics)
	}
	d := res.Diagnostics[0]
	if d.Code != ruleeval.CodeViolated || d.Severity != result.Error || d.Path != "src/cmd/lint.go" || d.Line != 5 {
		t.Errorf("diagnostic = %+v, want an RE001 error at src/cmd/lint.go:5", d)
	}
}

// TestAudit_ForwardsFilesPerRule checks a per-rule request carries the
// files too.
func TestAudit_ForwardsFilesPerRule(t *testing.T) {
	p := auditMock{&mockProvider{}}
	ev, err := ruleeval.New(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ev.Audit(context.Background(), auditInputs(), auditFiles); err != nil {
		t.Fatal(err)
	}
	if len(p.calls) != 1 || p.calls[0].Diff != "" || len(p.calls[0].Files) != 2 {
		t.Errorf("calls = %+v, want one request carrying the files", p.calls)
	}
}

// TestAudit_CachesApart checks an audit verdict is cached under the file
// set and the audit prompt version: it never serves a diff verdict, nor a
// verdict on other files or on the same files in another order.
func TestAudit_CachesApart(t *testing.T) {
	cache := newMemCache()
	p := auditProvider{batchProvider: &batchProvider{responses: map[string]*ruleeval.EvalResponse{"CMD-001": {Verdict: ruleeval.Compliant}}}, version: "audit-test"}
	ev, err := ruleeval.New(p, ruleeval.WithCache(cache))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	ev.Evaluate(ctx, auditInputs(), "diff --git a/x b/x\n")
	audit := func(files []ruleeval.File) {
		t.Helper()
		if _, err := ev.Audit(ctx, auditInputs(), files); err != nil {
			t.Fatal(err)
		}
	}
	audit(auditFiles)
	audit(auditFiles)
	if len(p.batches) != 2 {
		t.Fatalf("a diff verdict must not serve an audit, and a rerun must hit: %d requests", len(p.batches))
	}
	audit([]ruleeval.File{auditFiles[1], auditFiles[0]})
	audit([]ruleeval.File{auditFiles[0], {Path: "src/cmd/sync.go", Content: "package other\n"}})
	if len(p.batches) != 4 || len(cache.entries) != 4 {
		t.Errorf("reordered or edited files must miss: %d requests, %d entries", len(p.batches), len(cache.entries))
	}
}

// TestFilesHash checks the hash covers path, content and order, and keeps
// path and content apart.
func TestFilesHash(t *testing.T) {
	base := ruleeval.FilesHash(auditFiles)
	for name, files := range map[string][]ruleeval.File{
		"reordered": {auditFiles[1], auditFiles[0]},
		"renamed":   {{Path: "src/cmd/lint2.go", Content: "package cmd\n"}, auditFiles[1]},
		"edited":    {{Path: "src/cmd/lint.go", Content: "package cmd\n\n"}, auditFiles[1]},
		"shifted":   {{Path: "src/cmd/lint.gopackage cmd\n", Content: ""}, auditFiles[1]},
		"dropped":   auditFiles[:1],
	} {
		if ruleeval.FilesHash(files) == base {
			t.Errorf("%s: FilesHash did not change", name)
		}
	}
	if ruleeval.FilesHash(auditFiles) != base {
		t.Error("FilesHash is not deterministic")
	}
}
