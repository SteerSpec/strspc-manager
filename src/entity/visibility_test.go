package entity_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestEffectiveVisibility(t *testing.T) {
	tests := []struct{ declared, container, want string }{
		{"", entity.VisibilityPublic, entity.VisibilityPublic},
		{"", entity.VisibilityPrivate, entity.VisibilityPrivate},
		{entity.VisibilityProtected, entity.VisibilityPublic, entity.VisibilityProtected},
		{entity.VisibilityPublic, entity.VisibilityPrivate, entity.VisibilityPublic},
	}
	for _, tt := range tests {
		if got := entity.EffectiveVisibility(tt.declared, tt.container); got != tt.want {
			t.Errorf("EffectiveVisibility(%q, %q) = %q, want %q", tt.declared, tt.container, got, tt.want)
		}
	}
}

func TestVisibilityWider(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{entity.VisibilityPublic, entity.VisibilityProtected, true},
		{entity.VisibilityProtected, entity.VisibilityPrivate, true},
		{entity.VisibilityPublic, entity.VisibilityPrivate, true},
		{entity.VisibilityPrivate, entity.VisibilityPublic, false},
		{entity.VisibilityProtected, entity.VisibilityProtected, false},
		// An unknown value is narrower than anything: it permits nothing.
		{entity.VisibilityPrivate, "internal", true},
		{"internal", entity.VisibilityPrivate, false},
	}
	for _, tt := range tests {
		if got := entity.VisibilityWider(tt.a, tt.b); got != tt.want {
			t.Errorf("VisibilityWider(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidVisibility(t *testing.T) {
	for _, v := range []string{entity.VisibilityPrivate, entity.VisibilityProtected, entity.VisibilityPublic} {
		if !entity.ValidVisibility(v) {
			t.Errorf("ValidVisibility(%q) = false", v)
		}
	}
	for _, v := range []string{"", "internal", "Public"} {
		if entity.ValidVisibility(v) {
			t.Errorf("ValidVisibility(%q) = true", v)
		}
	}
}

func TestVisibility_OmittedWhenEmptyAndKeptWhenSet(t *testing.T) {
	data, err := json.Marshal(entity.Entity{ID: "ABC", Title: "ABC"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "visibility") {
		t.Errorf("empty visibility was written: %s", data)
	}

	var e entity.Entity
	if err := json.Unmarshal([]byte(`{"id":"ABC","title":"ABC","visibility":"private"}`), &e); err != nil {
		t.Fatal(err)
	}
	if data, err = json.Marshal(e); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"visibility":"private"`) {
		t.Errorf("visibility was dropped on rewrite: %s", data)
	}
}
