package render

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
	"github.com/SteerSpec/strspc-manager/src/inherit"
)

var update = flag.Bool("update", false, "update golden files")

func testRenderGolden(t *testing.T, name string) {
	t.Helper()

	jsonPath := filepath.Join("testdata", name+".json")

	ef, err := entity.Load(jsonPath)
	if err != nil {
		t.Fatalf("loading %s: %v", jsonPath, err)
	}

	r := NewMarkdownRenderer()
	var buf bytes.Buffer
	if err := r.Render(&buf, ef); err != nil {
		t.Fatalf("rendering %s: %v", name, err)
	}
	assertGolden(t, name, buf.String())
}

// assertGolden compares got with testdata/<name>.md, or rewrites it with
// -update.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	goldenPath := filepath.Join("testdata", name+".md")

	if *update {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		t.Logf("updated %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v (run with -update to generate)", goldenPath, err)
	}

	if got != string(want) {
		t.Errorf("output mismatch for %s.\nGot:\n%s\nWant:\n%s", name, got, string(want))
	}
}

func TestRenderBasic(t *testing.T) {
	testRenderGolden(t, "basic")
}

func TestRenderNested(t *testing.T) {
	testRenderGolden(t, "nested")
}

func TestRenderEmpty(t *testing.T) {
	testRenderGolden(t, "empty")
}

func TestRenderDeterministic(t *testing.T) {
	ef, err := entity.Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	r := NewMarkdownRenderer()
	var buf1, buf2 bytes.Buffer
	if err := r.Render(&buf1, ef); err != nil {
		t.Fatalf("render 1: %v", err)
	}
	if err := r.Render(&buf2, ef); err != nil {
		t.Fatalf("render 2: %v", err)
	}

	if buf1.String() != buf2.String() {
		t.Error("rendering is not deterministic: two renders produced different output")
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	_, err := New("html")
	if err == nil {
		t.Fatal("expected error for unsupported format, got nil")
	}
}

func TestRenderCustomTemplate(t *testing.T) {
	customTmpl := filepath.Join(t.TempDir(), "custom.md.tmpl")
	err := os.WriteFile(customTmpl, []byte(`# {{.Entity.ID}}: {{.Entity.Title}}
`), 0o644)
	if err != nil {
		t.Fatalf("writing custom template: %v", err)
	}

	ef, err := entity.Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}

	r := NewMarkdownRenderer(WithTemplate(customTmpl))
	var buf bytes.Buffer
	if err := r.Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}

	got := buf.String()
	want := "# TST: Test Entity\n"
	if got != want {
		t.Errorf("custom template output = %q, want %q", got, want)
	}
}

func TestNewMarkdownFormat(t *testing.T) {
	r, err := New(FormatMarkdown)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r == nil {
		t.Fatal("renderer is nil")
	}
}

func TestRenderWithRuleIDFormatter(t *testing.T) {
	ef, err := entity.Load("testdata/basic.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	f, err := NewRuleIDFormatter(&entity.RuleIdentifierFormat{Brackets: "(,)", EUIDPosition: "closing"})
	if err != nil {
		t.Fatalf("NewRuleIDFormatter: %v", err)
	}

	var buf bytes.Buffer
	if err := NewMarkdownRenderer(WithRuleIDFormatter(f)).Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	got := buf.String()
	if !strings.Contains(got, "(001-TST.0/D)") {
		t.Errorf("output does not contain (001-TST.0/D):\n%s", got)
	}
	if strings.Contains(got, "[TST-001.0/D]") {
		t.Errorf("output still contains the default form:\n%s", got)
	}
}

func TestRenderInherited(t *testing.T) {
	ef, err := entity.Load("testdata/inherited.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	acct := inherit.Hop{RealmID: "dev.bank.core", Entity: &entity.File{
		Entity: entity.Entity{ID: "ACCT", Title: "Account"},
		Rules: []entity.Rule{
			{ID: "ACCT-001", State: "I", Body: "Every account MUST have an owner."},
			{ID: "ACCT-002", State: "P", Body: "Withdrawals MUST NOT exceed the balance."},
			{ID: "ACCT-003", State: "D", Body: "Draft rules are not inherited."},
		},
	}}
	sav := inherit.Hop{RealmID: "dev.bank", Entity: ef}
	chains := map[string]Chain{
		"SAV":   {Entity: sav, Ancestors: []inherit.Hop{acct}},
		"SAVJR": {Entity: inherit.Hop{RealmID: "dev.bank", Entity: &ef.SubEntities[0]}, Ancestors: []inherit.Hop{sav, acct}},
	}

	var buf bytes.Buffer
	if err := NewMarkdownRenderer(WithInheritance(chains)).Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	got := buf.String()
	// SAV overrides ACCT-002 with its Draft SAV-001. SAVJR inherits SAV's
	// effective rule set, which holds only Published and Implemented rules,
	// so it gets ACCT-001 and ACCT-002, not overridden for it.
	for _, want := range []string{
		"**Inherits:** dev.bank.core:ACCT",
		"- **dev.bank.core:ACCT-002** Withdrawals MUST NOT exceed the balance. (P, from ACCT in dev.bank.core) *Overridden by SAV-001.*",
		"- **dev.bank.core:ACCT-001** Every account MUST have an owner. (I, from ACCT in dev.bank.core)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
	if strings.Contains(got, "ACCT-003") {
		t.Error("Draft ancestor rules must not be listed as inherited")
	}
	assertGolden(t, "inherited", got)
}

func TestRenderInherited_WithoutChainsUnchanged(t *testing.T) {
	ef, err := entity.Load("testdata/inherited.json")
	if err != nil {
		t.Fatalf("loading: %v", err)
	}
	var buf bytes.Buffer
	if err := NewMarkdownRenderer().Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if strings.Contains(buf.String(), "Inherited rules") {
		t.Error("no Inherited rules section without WithInheritance")
	}
}

func TestRenderInherited_EmptyChainKeepsSection(t *testing.T) {
	// A resolved chain whose ancestors hold only Draft rules still gets the
	// section, saying nothing is inherited yet.
	ef := &entity.File{Entity: entity.Entity{ID: "SAV", Title: "Savings", Inherits: "ACCT"}, RuleSet: entity.RuleSet{Version: "0.1.0"}}
	acct := inherit.Hop{RealmID: "dev.bank", Entity: &entity.File{
		Entity: entity.Entity{ID: "ACCT"},
		Rules:  []entity.Rule{{ID: "ACCT-001", State: "D", Body: "Draft."}},
	}}
	chains := map[string]Chain{"SAV": {Entity: inherit.Hop{RealmID: "dev.bank", Entity: ef}, Ancestors: []inherit.Hop{acct}}}
	var buf bytes.Buffer
	if err := NewMarkdownRenderer(WithInheritance(chains)).Render(&buf, ef); err != nil {
		t.Fatalf("rendering: %v", err)
	}
	if !strings.Contains(buf.String(), "## Inherited rules\n\nNo Published or Implemented rules are inherited yet.\n") {
		t.Errorf("empty chain section missing:\n%s", buf.String())
	}
}
