package entity

import (
	"strings"
	"testing"
)

func TestComputeHash(t *testing.T) {
	minimal := []byte(`{
		"entity": {"id": "TST", "title": "Test"},
		"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
		"rules": [], "notes": []
	}`)

	t.Run("valid format", func(t *testing.T) {
		h, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.HasPrefix(h, "blake3:") {
			t.Fatalf("expected blake3: prefix, got %q", h)
		}
		if len(h) != len("blake3:")+64 {
			t.Fatalf("expected 64 hex chars after prefix, got %d", len(h)-len("blake3:"))
		}
	})

	t.Run("stable output", func(t *testing.T) {
		h1, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(minimal)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("unstable hash: %q != %q", h1, h2)
		}
	})

	t.Run("hash field excluded", func(t *testing.T) {
		withNull := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": []
		}`)
		withHash := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": "blake3:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			"rules": [], "notes": []
		}`)
		h1, err := ComputeHash(withNull)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(withHash)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("hash should be excluded: %q != %q", h1, h2)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		_, err := ComputeHash([]byte(`not json`))
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("sub-entity hash nulled", func(t *testing.T) {
		withSub := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": [],
			"sub_entities": [{
				"entity": {"id": "SUB", "title": "Sub"},
				"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
				"rules": [], "notes": []
			}]
		}`)
		withSubHash := []byte(`{
			"entity": {"id": "TST", "title": "Test"},
			"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": null},
			"rules": [], "notes": [],
			"sub_entities": [{
				"entity": {"id": "SUB", "title": "Sub"},
				"rule_set": {"version": "0.1.0", "timestamp": "2026-01-01T00:00:00Z", "hash": "blake3:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
				"rules": [], "notes": []
			}]
		}`)
		h1, err := ComputeHash(withSub)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		h2, err := ComputeHash(withSubHash)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("sub-entity hash should be nulled: %q != %q", h1, h2)
		}
	})
}
