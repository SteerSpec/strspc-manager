// Package providers constructs ruleeval.Provider implementations by name.
// It exists so that ruleeval itself never imports a concrete provider
// (which would create an import cycle through ruleeval/prompt).
package providers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/ruleeval"
	"github.com/SteerSpec/strspc-manager/src/ruleeval/claude"
)

// Sentinel errors returned by New; test with errors.Is.
var (
	// ErrStatic signals that the configuration asks for no AI provider at
	// all; the caller should run the evaluator in static-only mode.
	ErrStatic = errors.New("providers: static-only evaluation requested")
	// ErrProviderNotImplemented signals a provider name that is recognised
	// but not shipped yet (openai, ollama).
	ErrProviderNotImplemented = errors.New("providers: provider not implemented")
	// ErrUnknownProvider signals a provider name that is not recognised.
	ErrUnknownProvider = errors.New("providers: unknown provider")
)

// Options carries the evaluator settings from .strspc/config.yaml.
type Options struct {
	// APIKey defaults to the provider's environment variable.
	APIKey string
	// Model takes the provider default when empty.
	Model string
	// Endpoint is a custom base URL; empty for the provider default.
	Endpoint string
	// Effort takes the provider default when empty.
	Effort string
	// Concurrency takes the provider default when zero.
	Concurrency int

	// HTTPClient overrides the transport (tests).
	HTTPClient *http.Client
}

// New returns a Provider for name. Names are matched case-insensitively.
//
//	"", "null", "static", "static-only" → ErrStatic
//	"claude"                            → claude.New
//	"openai", "ollama"                  → ErrProviderNotImplemented
//	anything else                       → ErrUnknownProvider
func New(name string, o Options) (ruleeval.Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "null", "static", "static-only":
		return nil, ErrStatic
	case "claude":
		return claude.New(claude.Config{
			APIKey:      o.APIKey,
			Model:       o.Model,
			BaseURL:     o.Endpoint,
			Effort:      o.Effort,
			Concurrency: o.Concurrency,
			HTTPClient:  o.HTTPClient,
		})
	case "openai", "ollama":
		return nil, fmt.Errorf("%w: %q (tracked in SteerSpec/strspc-manager#67)", ErrProviderNotImplemented, name)
	default:
		return nil, fmt.Errorf("%w: %q", ErrUnknownProvider, name)
	}
}
