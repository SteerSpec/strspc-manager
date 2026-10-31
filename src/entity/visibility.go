package entity

// Visibility values of an entity, from narrowest to widest (ENT-011, ENT-015).
const (
	// VisibilityPrivate limits inheritance to the Realm tree and overrides to
	// the Realm holding the entity (ENT-014).
	VisibilityPrivate = "private"
	// VisibilityProtected limits overrides to the Realm tree (ENT-013).
	VisibilityProtected = "protected"
	// VisibilityPublic permits inheritance and overrides from any Realm
	// (ENT-012). It is the default of an entity that no other contains.
	VisibilityPublic = "public"
)

// ValidVisibility reports whether v is one of the Visibility* constants.
func ValidVisibility(v string) bool {
	return visibilityRank(v) >= 0
}

// EffectiveVisibility returns declared, or container, the effective visibility
// of the containing entity, when declared is empty (ENT-011). For an entity
// that no other contains, pass VisibilityPublic.
func EffectiveVisibility(declared, container string) string {
	if declared == "" {
		return container
	}
	return declared
}

// VisibilityWider reports whether a is wider than b (ENT-015). An unknown value
// permits nothing, so it counts as narrower than any known one.
func VisibilityWider(a, b string) bool {
	return visibilityRank(a) > visibilityRank(b)
}

// visibilityRank orders the visibility values; it is -1 for an unknown one.
func visibilityRank(v string) int {
	switch v {
	case VisibilityPrivate:
		return 0
	case VisibilityProtected:
		return 1
	case VisibilityPublic:
		return 2
	default:
		return -1
	}
}
