package ruleeval_test

import (
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
)

func TestNew_NilProviderWithoutStaticOnly(t *testing.T) {
	_, err := ruleeval.New(nil)
	if err == nil {
		t.Fatal("expected error when provider is nil and StaticOnly is false")
	}
}

func TestNew_NilProviderWithStaticOnly(t *testing.T) {
	e, err := ruleeval.New(nil, ruleeval.WithStaticOnly(true))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if e == nil {
		t.Fatal("expected non-nil Evaluator")
	}
}
