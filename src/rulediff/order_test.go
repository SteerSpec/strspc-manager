package rulediff

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Finding 21: the same inputs give the same diagnostics in the same order.
func TestDiagnosticOrderIsStable(t *testing.T) {
	var rules, subs []string
	for i := 1; i <= 12; i++ {
		rules = append(rules, fmt.Sprintf(`{"id":"ENT-%03d","revision":0,"state":"D","body":"x","added_by":"github:a","added_at":"2026-01-01"}`, i))
		subs = append(subs, fmt.Sprintf(`{"entity":{"id":"S%02d"},"rules":[]}`, i))
	}
	base := `{"entity":{"id":"ENT"},"rule_set":{"version":"1.0.0"},"rules":[` + strings.Join(rules, ",") + `],"sub_entities":[` + strings.Join(subs, ",") + `]}`
	// Head drops every sub-entity and moves every rule straight to I: one
	// diagnostic each, in an order a map would shuffle.
	head := strings.ReplaceAll(`{"entity":{"id":"ENT"},"rule_set":{"version":"1.0.1"},"rules":[`+strings.Join(rules, ",")+`]}`, `"state":"D"`, `"state":"I"`)

	baseDir, headDir := t.TempDir(), t.TempDir()
	for i := 1; i <= 12; i++ {
		name := fmt.Sprintf("F%02d.json", i)
		if err := os.WriteFile(filepath.Join(baseDir, name), []byte(base), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(headDir, name), []byte(head), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	first := CompareDir(baseDir, headDir).Diagnostics
	if len(first) < 24 {
		t.Fatalf("only %d diagnostics", len(first))
	}
	for range 20 {
		if got := CompareDir(baseDir, headDir).Diagnostics; !reflect.DeepEqual(got, first) {
			t.Fatal("diagnostic order changed between runs")
		}
	}
}
