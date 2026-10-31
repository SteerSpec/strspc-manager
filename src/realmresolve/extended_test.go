package realmresolve

import (
	"context"
	"reflect"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/inherit"
)

func TestResolveExtended(t *testing.T) {
	out, res := New().Resolve(context.Background(), loadTestRealm(t, "extended/root"), absTestdata(t, "extended/root"))
	if len(res.Errors()) != 0 {
		t.Fatalf("unexpected errors: %v", res.Errors())
	}

	// ROOT-001 is written once despite the repeat. ROOT-002 uses the short
	// form. MID-001 is inherited, and its target resolves through dev.mid:
	// dev.core is no dependency of dev.root. ROOT-004's target is Abandoned
	// and ROOT-005's note is not an extends note, so neither is inlined.
	want := map[string][]inherit.Extended{
		"dev.root:ROOT-001": {{ID: "dev.mid:MID-001", Revision: 2, Body: "Mid rule."}},
		"dev.root:ROOT-002": {{ID: "ROOT-003", Revision: 0, Body: "Rule ROOT-003."}},
		"dev.mid:MID-001":   {{ID: "dev.core:BASE-001", Revision: 3, Body: "Base rule."}},
	}
	if !reflect.DeepEqual(out.Extended, want) {
		t.Errorf("Extended = %v, want %v", out.Extended, want)
	}
}

func TestResolveExtended_NoExtendsNotes(t *testing.T) {
	out, _ := New().Resolve(context.Background(), loadTestRealm(t, "note-refs-solo"), absTestdata(t, "note-refs-solo"))
	if len(out.Extended) != 0 {
		t.Errorf("Extended = %v, want none", out.Extended)
	}
}
