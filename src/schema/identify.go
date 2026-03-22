package schema

import (
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// ErrNotEntity is returned when a file's $schema does not reference
// any entity schema version.
var ErrNotEntity = errors.New("not an entity file")

// IsEntitySchema reports whether schema matches the entity schema
// for a specific version (e.g. "v1"). Supports both formats:
//   - Relative: ./_schema/entity.v1.schema.json
//   - URL:      https://steerspec.dev/schemas/entity/v1.json
func IsEntitySchema(schema, version string) bool {
	if schema == "" || version == "" {
		return false
	}
	// Relative style: exact base filename match.
	if path.Base(schema) == "entity."+version+".schema.json" {
		return true
	}
	// URL style: require /entity/ as a distinct path segment.
	if strings.HasSuffix(schema, "/entity/"+version+".json") {
		return true
	}
	return false
}

// IsEntitySchemaAnyVersion reports whether schema references any
// entity schema version, regardless of which version.
func IsEntitySchemaAnyVersion(schema string) bool {
	if schema == "" {
		return false
	}
	base := path.Base(schema)
	// Relative style: entity.v<N>.schema.json
	if strings.HasPrefix(base, "entity.v") && strings.HasSuffix(base, ".schema.json") {
		return true
	}
	// URL style: v<N>.json under an /entity/ path segment
	if strings.HasPrefix(base, "v") && strings.HasSuffix(base, ".json") &&
		strings.Contains(schema, "/entity/") {
		return true
	}
	return false
}

// ValidateFileSchema checks that ef and all its sub-entities declare
// the expected schema version. Returns [ErrNotEntity] if the schema
// doesn't reference an entity schema at all.
func ValidateFileSchema(ef *entity.File, version string) error {
	if !IsEntitySchema(ef.Schema, version) {
		if !IsEntitySchemaAnyVersion(ef.Schema) {
			return ErrNotEntity
		}
		return fmt.Errorf("schema version mismatch: file declares %q, expected version %q",
			ef.Schema, version)
	}
	for i := range ef.SubEntities {
		if err := ValidateFileSchema(&ef.SubEntities[i], version); err != nil {
			return fmt.Errorf("sub-entity %s: %w", ef.SubEntities[i].Entity.ID, err)
		}
	}
	return nil
}
