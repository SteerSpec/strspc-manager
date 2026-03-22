package schema

import (
	"errors"
	"strings"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestIsEntitySchema(t *testing.T) {
	tests := []struct {
		name    string
		schema  string
		version string
		want    bool
	}{
		// Relative format matches.
		{"relative_v1", "./_schema/entity.v1.schema.json", "v1", true},
		{"relative_bare", "entity.v1.schema.json", "v1", true},
		{"relative_v2", "../schemas/entity.v2.schema.json", "v2", true},

		// URL format matches.
		{"url_v1", "https://steerspec.dev/schemas/entity/v1.json", "v1", true},
		{"url_v2", "https://example.com/entity/v2.json", "v2", true},

		// Wrong version.
		{"wrong_version_relative", "entity.v2.schema.json", "v1", false},
		{"wrong_version_url", "https://steerspec.dev/schemas/entity/v2.json", "v1", false},

		// Realm schema (not entity).
		{"realm_url", "realm/v1.json", "v1", false},
		{"realm_relative", "realm.v1.schema.json", "v1", false},

		// Empty.
		{"empty_schema", "", "v1", false},
		{"empty_version", "entity.v1.schema.json", "", false},

		// False positives prevented by stricter matching.
		{"prefix_false_positive", "myentity.v1.schema.json", "v1", false},
		{"url_no_boundary", "notentity/v1.json", "v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEntitySchema(tt.schema, tt.version); got != tt.want {
				t.Errorf("IsEntitySchema(%q, %q) = %v, want %v",
					tt.schema, tt.version, got, tt.want)
			}
		})
	}
}

func TestIsEntitySchemaAnyVersion(t *testing.T) {
	tests := []struct {
		name   string
		schema string
		want   bool
	}{
		// Relative format.
		{"relative_v1", "entity.v1.schema.json", true},
		{"relative_v2", "entity.v2.schema.json", true},
		{"relative_path", "./_schema/entity.v1.schema.json", true},

		// URL format.
		{"url_v1", "https://steerspec.dev/schemas/entity/v1.json", true},
		{"url_v99", "https://steerspec.dev/schemas/entity/v99.json", true},

		// Not entity.
		{"realm_relative", "realm.v1.schema.json", false},
		{"realm_url", "https://steerspec.dev/schemas/realm/v1.json", false},
		{"random", "random.json", false},
		{"empty", "", false},

		// False positives prevented by stricter matching.
		{"nested_entity_path", "https://example.com/entity/archive/v1.json", false},
		{"empty_version_relative", "entity.v.schema.json", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsEntitySchemaAnyVersion(tt.schema); got != tt.want {
				t.Errorf("IsEntitySchemaAnyVersion(%q) = %v, want %v",
					tt.schema, got, tt.want)
			}
		})
	}
}

func TestValidateFileSchema(t *testing.T) {
	t.Run("valid entity", func(t *testing.T) {
		ef := &entity.File{
			Schema: "https://steerspec.dev/schemas/entity/v1.json",
			Entity: entity.Entity{ID: "TST"},
		}
		if err := ValidateFileSchema(ef, "v1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("valid with sub-entities", func(t *testing.T) {
		ef := &entity.File{
			Schema: "https://steerspec.dev/schemas/entity/v1.json",
			Entity: entity.Entity{ID: "TST"},
			SubEntities: []entity.File{
				{
					Schema: "./_schema/entity.v1.schema.json",
					Entity: entity.Entity{ID: "SUB"},
				},
			},
		}
		if err := ValidateFileSchema(ef, "v1"); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("not an entity", func(t *testing.T) {
		ef := &entity.File{
			Schema: "realm.v1.schema.json",
			Entity: entity.Entity{ID: "RLM"},
		}
		err := ValidateFileSchema(ef, "v1")
		if !errors.Is(err, ErrNotEntity) {
			t.Errorf("expected ErrNotEntity, got: %v", err)
		}
	})

	t.Run("version mismatch", func(t *testing.T) {
		ef := &entity.File{
			Schema: "entity.v2.schema.json",
			Entity: entity.Entity{ID: "TST"},
		}
		err := ValidateFileSchema(ef, "v1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if errors.Is(err, ErrNotEntity) {
			t.Error("should not be ErrNotEntity for version mismatch")
		}
	})

	t.Run("sub-entity version mismatch", func(t *testing.T) {
		ef := &entity.File{
			Schema: "entity.v1.schema.json",
			Entity: entity.Entity{ID: "TST"},
			SubEntities: []entity.File{
				{
					Schema: "entity.v2.schema.json",
					Entity: entity.Entity{ID: "BAD"},
				},
			},
		}
		err := ValidateFileSchema(ef, "v1")
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if want := "sub-entity BAD"; !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
		}
	})

	t.Run("nil file", func(t *testing.T) {
		err := ValidateFileSchema(nil, "v1")
		if !errors.Is(err, ErrNotEntity) {
			t.Errorf("expected ErrNotEntity for nil input, got: %v", err)
		}
	})

	t.Run("empty version", func(t *testing.T) {
		ef := &entity.File{
			Schema: "entity.v1.schema.json",
			Entity: entity.Entity{ID: "TST"},
		}
		err := ValidateFileSchema(ef, "")
		if err == nil {
			t.Fatal("expected error for empty version, got nil")
		}
	})

	t.Run("sub-entity not entity wraps ErrNotEntity", func(t *testing.T) {
		ef := &entity.File{
			Schema: "entity.v1.schema.json",
			Entity: entity.Entity{ID: "TST"},
			SubEntities: []entity.File{
				{
					Schema: "random.json",
					Entity: entity.Entity{ID: "BAD"},
				},
			},
		}
		err := ValidateFileSchema(ef, "v1")
		if !errors.Is(err, ErrNotEntity) {
			t.Errorf("expected wrapped ErrNotEntity, got: %v", err)
		}
	})
}
