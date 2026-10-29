package realmresolve

import (
	"context"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/inherit"
	"github.com/SteerSpec/strspc-manager/src/remote"
	"github.com/SteerSpec/strspc-manager/src/result"
)

// diagsFor returns the diagnostics with code whose message mentions substr.
func diagsFor(res *result.Result, code, substr string) []result.Diagnostic {
	var out []result.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == code && strings.Contains(d.Message, substr) {
			out = append(out, d)
		}
	}
	return out
}

func TestResolveInheritance_TransitiveChain(t *testing.T) {
	// dev.x depends on dev.y only; dev.y depends on dev.z. SAV's chain
	// reaches dev.z through dev.y's own dependency, loaded on demand.
	got, res := New().Resolve(context.Background(), loadTestRealm(t, "inherit-chain/x"), absTestdata(t, "inherit-chain/x"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", res.Diagnostics)
	}
	if len(got.Inheritance) != 1 {
		t.Fatalf("expected 1 inheritance chain, got %d", len(got.Inheritance))
	}
	inh := got.Inheritance[0]
	if inh.Entity.RealmID != "dev.x" || inh.Entity.Entity.Entity.ID != "SAV" {
		t.Errorf("entity = %s:%s, want dev.x:SAV", inh.Entity.RealmID, inh.Entity.Entity.Entity.ID)
	}
	var hops []string
	for _, h := range inh.Ancestors {
		hops = append(hops, h.RealmID+":"+h.Entity.Entity.ID)
	}
	if strings.Join(hops, ",") != "dev.y:ACCT,dev.z:BASE" {
		t.Errorf("ancestors = %v, want [dev.y:ACCT dev.z:BASE]", hops)
	}

	// The effective set: BASE-001 replaced by ACCT-001, which SAV-001
	// replaces in turn; BASE-002 replaced by SAV-002.
	var ids []string
	for _, r := range inherit.Effective(inh.Entity, inh.Ancestors, nil) {
		ids = append(ids, r.QualifiedID())
	}
	if strings.Join(ids, ",") != "dev.x:SAV-001,dev.x:SAV-002" {
		t.Errorf("effective = %v", ids)
	}
}

func TestResolveInheritance_Errors(t *testing.T) {
	_, res := New().Resolve(context.Background(), loadTestRealm(t, "inherit-errors/root"), absTestdata(t, "inherit-errors/root"))

	tests := []struct {
		code, substr string
		sev          result.Severity
	}{
		{"RR014", "entity CYCA: inheritance chain", result.Error},
		{"RR014", "entity CYCB: inheritance chain", result.Error},
		{"RR014", "entity LONGA: inheritance chain", result.Error},
		{"RR013", `entity UNQ: inherits "LIBENT" cannot be resolved: no entity LIBENT in realm dev.root; it exists in dependency dev.lib`, result.Error},
		{"RR013", "realm dev.other is not a dependency of realm dev.root", result.Error},
		{"RR013", "no entity NOPE in realm dev.lib", result.Error},
		{"RR016", `rule OVR-001 overrides "dev.lib:LIBENT-002"`, result.Warning},
		{"RR015", `rule OVR-002 overrides "dev.lib:LIBENT-009": dev.lib:LIBENT-009 is not a rule of any inherited entity`, result.Error},
		{"RR015", "rule NOINH-001", result.Error},
		{"RR015", "already replaced by dev.lib:LIBKID-001 in LIBKID", result.Error},
		{"RR013", "entity GONEA (through dev.lib:GONEKID)", result.Error},
		{"RR013", "entity GONEB (through dev.lib:GONEKID)", result.Error},
		// dev.gone is missing; the root and dev.lib both declare it, and it
		// is loaded once for the root and both chains.
		{"RR002", `dependency "dev.gone"`, result.Error},
	}
	for _, tt := range tests {
		got := diagsFor(res, tt.code, tt.substr)
		if len(got) != 1 {
			t.Errorf("%s %q: got %d matches; diagnostics: %v", tt.code, tt.substr, len(got), res.Diagnostics)
			continue
		}
		if got[0].Severity != tt.sev {
			t.Errorf("%s %q: severity %s, want %s", tt.code, tt.substr, got[0].Severity, tt.sev)
		}
	}
	// LONGB is exactly three hops from LONGE; OVR-003's target is inherited;
	// SUPER-001 is superseded, so its dangling override is not live.
	for _, ok := range []string{"rule OVR-003", "entity LONGB:", "rule SUPER-001"} {
		for _, d := range res.Diagnostics {
			if strings.HasPrefix(d.Message, ok) {
				t.Errorf("unexpected diagnostic for %s: %s", ok, d)
			}
		}
	}
	// Exactly the expected set: 3 RR014, 5 RR013, 3 RR015, 1 RR016, 1 RR002.
	counts := map[string]int{}
	for _, d := range res.Diagnostics {
		counts[d.Code]++
	}
	if counts["RR013"] != 5 || counts["RR014"] != 3 || counts["RR015"] != 3 || counts["RR016"] != 1 || counts["RR002"] != 1 || len(res.Diagnostics) != 13 {
		t.Errorf("diagnostic counts = %v", counts)
	}
}

func TestResolveInheritance_SubRealm(t *testing.T) {
	// auth is a sub-realm of dev.s: its entities inherit parent-realm
	// entities by short or qualified reference, and a sub-realm qualifier
	// on an override normalises onto the realm tree.
	got, res := New().Resolve(context.Background(), loadTestRealm(t, "inherit-sub"), absTestdata(t, "inherit-sub"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", res.Diagnostics)
	}
	if len(got.Inheritance) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(got.Inheritance))
	}
	for _, inh := range got.Inheritance {
		if inh.Entity.RealmID != "dev.s" {
			t.Errorf("%s namespace = %s, want dev.s", inh.Entity.Entity.Entity.ID, inh.Entity.RealmID)
		}
	}
}

func TestResolveInheritance_NoOpWithoutInheritance(t *testing.T) {
	got, res := New().Resolve(context.Background(), loadTestRealm(t, "parent"), absTestdata(t, "parent"))
	if !res.OK() || got.Inheritance != nil {
		t.Errorf("expected no inheritance work: %v, %v", got.Inheritance, res.Diagnostics)
	}
}

func TestResolveInheritance_PrefixNamedDependencyAndItsSubRealm(t *testing.T) {
	// dev.bank.core is a dependency of dev.bank, not a sub-realm of it, and
	// dev.bank.core.auth is a sub-realm the dependency's manifest declares.
	got, res := New().Resolve(context.Background(), loadTestRealm(t, "inherit-prefix/bank"), absTestdata(t, "inherit-prefix/bank"))
	if len(res.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got: %v", res.Diagnostics)
	}
	byEntity := map[string]string{}
	for _, inh := range got.Inheritance {
		a := inh.Ancestors[0]
		byEntity[inh.Entity.Entity.Entity.ID] = a.RealmID + ":" + a.Entity.Entity.ID
	}
	if byEntity["ACCT"] != "dev.bank.core:CORE" || byEntity["LOGIN"] != "dev.bank.core:CAUTH" {
		t.Errorf("chains = %v", byEntity)
	}
}

func TestOriginWithin_RepositoryRoot(t *testing.T) {
	o := originWithin(&remote.Ref{Owner: "o", Repo: "r", Ref: "v1"}, "/cache/x", "/cache/x")
	if o == nil || o.Path != "" {
		t.Errorf("origin at the repository root = %+v, want empty path", o)
	}
	o = originWithin(&remote.Ref{Owner: "o", Repo: "r", Ref: "v1", Path: "rules"}, "/cache/x", "/cache/x/core")
	if o == nil || o.Path != "rules/core" {
		t.Errorf("origin of a subdirectory = %+v, want rules/core", o)
	}
	if originWithin(&remote.Ref{Path: "rules"}, "/cache/x", "/cache/y") != nil {
		t.Error("a directory outside the subtree has no origin")
	}
}
