package rulediff

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
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

// A hand edit of a Published rule's body trips RD002 and, because the rule
// set was not bumped, RD007, RD009 and RD008. The cause comes first: the rule
// codes, then the rule set's version and timestamp, then its hash (gh #396).
func TestDiff_RuleCodesComeBeforeRuleSetCodes(t *testing.T) {
	base := makeBase()
	head := makeHead(base)
	head.RuleSet = base.RuleSet
	head.RuleSet.Hash = ptr("blake3:stale") // not recomputed after the edit
	head.Rules[1].Body = "Edited published body"

	baseData, err := json.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	headData, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}

	for name, got := range map[string][]string{
		"DiffBytes": codes(New().DiffBytes(baseData, headData)),
		"Diff":      codes(New().Diff(base, head)),
	} {
		t.Run(name, func(t *testing.T) {
			want := []string{"RD002", "RD007", "RD009"}
			if name == "DiffBytes" {
				want = append(want, "RD008")
			}
			if !slices.Equal(got, want) {
				t.Errorf("codes = %v, want %v", got, want)
			}
		})
	}
}
