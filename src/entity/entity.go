// Package entity defines the shared domain types for SteerSpec entity files
// and Realm manifests. All modules in strspc-manager operate on these types.
package entity

// File represents a SteerSpec entity file conforming to entity.v1.schema.json.
type File struct {
	// Schema is the $schema URI the file declares; it must name the entity.v1
	// schema.
	Schema string `json:"$schema"`
	// Entity holds the identity and metadata of the entity the file describes.
	Entity Entity `json:"entity"`
	// RuleSet carries the version, timestamp and integrity hash of the rule
	// set.
	RuleSet RuleSet `json:"rule_set"`
	// Rules lists the rules owned directly by this entity, in file order.
	Rules []Rule `json:"rules"`
	// SubEntities holds nested entity files whose Parent is this entity.
	SubEntities []File `json:"sub_entities,omitempty"`
	// Notes lists the annotations attached to rules of this entity.
	Notes []Note `json:"notes"`
}

// Entity holds the identity and metadata for a SteerSpec entity.
type Entity struct {
	// ID is the unique entity identifier, 3 to 18 characters long.
	ID string `json:"id"`
	// Title is the human-readable name of the entity.
	Title string `json:"title"`
	// Description is an optional free-text summary of the entity's purpose.
	Description string `json:"description,omitempty"`
	// Parent is the ID of the containing entity; empty for a top-level entity.
	Parent string `json:"parent,omitempty"`
}

// RuleSet holds versioning and integrity metadata for an entity's rule set.
type RuleSet struct {
	// Version is the semantic version of the rule set.
	Version string `json:"version"`
	// Timestamp is the RFC 3339 UTC time of the last change to the rule set.
	Timestamp string `json:"timestamp"`
	// Hash is the blake3:<hex> hash of the canonical file content, or nil when
	// not yet computed.
	Hash *string `json:"hash"`
}

// Rule represents a single rule within an entity.
type Rule struct {
	// ID is the rule identifier in <PREFIX>-<NNN> form.
	ID string `json:"id"`
	// Revision is the edit counter of the rule; it starts at 0 and grows by one
	// per edit.
	Revision int `json:"revision"`
	// State is the lifecycle state code of the rule, one of the State*
	// constants.
	State string `json:"state"`
	// Body is the normative text of the rule.
	Body string `json:"body"`
	// AddedBy names the author who added the rule.
	AddedBy string `json:"added_by"`
	// AddedAt is the YYYY-MM-DD date the rule was added.
	AddedAt string `json:"added_at"`
	// Supersedes is the ID of the rule this rule replaces, or nil when it
	// replaces none.
	Supersedes *string `json:"supersedes"`
}

// Note represents an annotation attached to a specific rule.
type Note struct {
	// ID is the note identifier in <rule_id>/<NN> form.
	ID string `json:"id"`
	// RuleRef is the ID of the rule the note is attached to.
	RuleRef string `json:"rule_ref"`
	// Type is the note kind, such as rationale, example, counter_example,
	// applies_to or clarification.
	Type string `json:"type"`
	// Content is the free-text body of the note.
	Content string `json:"content"`
	// AddedBy names the author who added the note.
	AddedBy string `json:"added_by"`
	// AddedAt is the YYYY-MM-DD date the note was added.
	AddedAt string `json:"added_at"`
	// Revision is the edit counter of the note; it is 0 when the note is
	// added and grows by one per change to its content or type.
	Revision int `json:"revision"`
}

// RealmFile represents a SteerSpec Realm manifest (realm.json).
type RealmFile struct {
	// Schema is the $schema URI the manifest declares; it must name the
	// realm.v1 schema.
	Schema string `json:"$schema"`
	// Realm holds the identity and version of the Realm the manifest describes.
	Realm RealmMeta `json:"realm"`
	// Dependencies lists the other Realms this Realm depends on, each pinned to
	// a version.
	Dependencies []RealmDep `json:"dependencies,omitempty"`
	// SubRealms lists the subdirectory names of child Realms, if any.
	SubRealms []string `json:"sub_realms,omitempty"`
	// RuleIdentifierFormat overrides the RLIFRMT rule identifier rendering
	// defaults (RLM-002, RLMMNFST-004); nil keeps them, or for a sub-realm
	// means the parent's format applies (RLM-015).
	RuleIdentifierFormat *RuleIdentifierFormat `json:"rule_identifier_format,omitempty"`
}

// RuleIdentifierFormat is a Realm's rule_identifier_format setting. It only
// changes how a rule identifier is rendered, never the stored "<EUID>-<NNN>"
// form (RLIFRMT-008). An empty field falls back to its RLIFRMT default.
type RuleIdentifierFormat struct {
	// Brackets is the opening and closing pair: "(,)", "{,}" or "[,]"
	// (RLIFRMT-002).
	Brackets string `json:"brackets,omitempty"`
	// EUIDPosition places the Entity Unique Identifier at the "opening" or
	// "closing" part of the identifier; the Rule Set Unique Number takes the
	// other one (RLIFRMT-003, RLIFRMT-004).
	EUIDPosition string `json:"euid_position,omitempty"`
	// IDSplitter separates the EUID and the number: "-" or "_" (RLIFRMT-005).
	IDSplitter string `json:"id_splitter,omitempty"`
	// RevisionSplitter precedes the revision: "." or "#" (RLIFRMT-006).
	RevisionSplitter string `json:"revision_splitter,omitempty"`
	// StateSplitter precedes the state: "/", "\" or "|" (RLIFRMT-007).
	StateSplitter string `json:"state_splitter,omitempty"`
}

// RealmMeta holds the identity and versioning for a Realm.
type RealmMeta struct {
	// ID is the Realm identifier.
	ID string `json:"id"`
	// Title is the human-readable name of the Realm.
	Title string `json:"title"`
	// Version is the semantic version of the Realm.
	Version string `json:"version"`
}

// RealmDep declares a dependency on another Realm.
type RealmDep struct {
	// RealmID is the identifier the dependency's own manifest must declare.
	RealmID string `json:"realm_id"`
	// Version is the exact version the dependency's own manifest must declare.
	Version string `json:"version"`
	// Source is where the dependency is fetched from: a path relative to the
	// manifest directory or a github:// reference.
	Source string `json:"source,omitempty"`
}
