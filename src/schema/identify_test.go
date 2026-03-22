package schema

import (
	"errors"
	"testing"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

func TestIsEntitySchema(t *testing.T) {
	tests := []struct {
		schema  string
		version string
		want    bool
	}{
		// Relative format matches.
		{"./_schema/entity.v1.schema.json", "v1", true},
		{"entity.v1.schema.json", "v1", true},
		{"../schemas/entity.v2.schema.json", "v2", true},

		// URL format matches.
		{"https://steerspec.dev/schemas/entity/v1.json", "v1", true},
		{"https://example.com/entity/v2.json", "v2", true},

		// Wrong version.
		{"entity.v2.schema.json", "v1", false},
		{"https://steerspec.dev/schemas/entity/v2.json", "v1", false},

		// Realm schema (not entity).
		{"realm/v1.json", "v1", false},
		{"realm.v1.schema.json", "v1", false},

		// Empty.
		{"", "v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.schema+"_"+tt.version, func(t *testing.T) {
			if got := IsEntitySchema(tt.schema, tt.version); got != tt.want {
				t.Errorf("IsEntitySchema(%q, %q) = %v, want %v",
					tt.schema, tt.version, got, tt.want)
			}
		})
	}
}

func TestIsEntitySchemaAnyVersion(t *testing.T) {
	tests := []struct {
		schema string
		want   bool
	}{
		// Relative format.
		{"entity.v1.schema.json", true},
		{"entity.v2.schema.json", true},
		{"./_schema/entity.v1.schema.json", true},

		// URL format.
		{"https://steerspec.dev/schemas/entity/v1.json", true},
		{"https://steerspec.dev/schemas/entity/v99.json", true},

		// Not entity.
		{"realm.v1.schema.json", false},
		{"https://steerspec.dev/schemas/realm/v1.json", false},
		{"random.json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.schema, func(t *testing.T) {
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
		if want := "sub-entity BAD"; !containsStr(err.Error(), want) {
			t.Errorf("error %q should contain %q", err, want)
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

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
