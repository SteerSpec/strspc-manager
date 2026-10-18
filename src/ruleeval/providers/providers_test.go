package providers_test

import (
	"errors"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/providers"
)

func TestNew_Static(t *testing.T) {
	for _, name := range []string{"", "null", "static", "static-only", " Static-Only "} {
		p, err := providers.New(name, providers.Options{})
		if !errors.Is(err, providers.ErrStatic) || p != nil {
			t.Errorf("New(%q) = %v, %v; want ErrStatic", name, p, err)
		}
	}
}

func TestNew_NotImplemented(t *testing.T) {
	for _, name := range []string{"openai", "ollama", "OpenAI"} {
		_, err := providers.New(name, providers.Options{})
		if !errors.Is(err, providers.ErrProviderNotImplemented) {
			t.Errorf("New(%q) = %v; want ErrProviderNotImplemented", name, err)
		}
	}
}

func TestNew_Unknown(t *testing.T) {
	_, err := providers.New("gemini", providers.Options{})
	if !errors.Is(err, providers.ErrUnknownProvider) {
		t.Errorf("New(gemini) = %v; want ErrUnknownProvider", err)
	}
}

func TestNew_Claude(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := providers.New("claude", providers.Options{})
	if !errors.Is(err, ruleeval.ErrUnavailable) {
		t.Fatalf("claude without key = %v; want ErrUnavailable", err)
	}

	p, err := providers.New("claude", providers.Options{APIKey: "test-key", Model: "claude-sonnet-5-5", Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := p.(ruleeval.Describer); !ok || d.Model() != "claude-sonnet-5-5" {
		t.Errorf("claude provider should describe its model, got %T", p)
	}
	if _, ok := p.(ruleeval.BatchProvider); !ok {
		t.Error("claude provider should support batching")
	}

	if _, err := providers.New("claude", providers.Options{APIKey: "k", Effort: "ultra"}); err == nil {
		t.Error("invalid effort should be rejected")
	}
}
