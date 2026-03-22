// Package entity defines the shared domain types for SteerSpec entity files
// and Realm manifests. All modules in strspc-manager operate on these types.
package entity

// File represents a SteerSpec entity file conforming to entity.v1.schema.json.
type File struct {
	Schema      string  `json:"$schema"`
	Entity      Entity  `json:"entity"`
	RuleSet     RuleSet `json:"rule_set"`
	Rules       []Rule  `json:"rules"`
	SubEntities []File  `json:"sub_entities,omitempty"`
	Notes       []Note  `json:"notes"`
}

// Entity holds the identity and metadata for a SteerSpec entity.
type Entity struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Parent      string `json:"parent,omitempty"`
}

// RuleSet holds versioning and integrity metadata for an entity's rule set.
type RuleSet struct {
	Version   string  `json:"version"`
	Timestamp string  `json:"timestamp"`
	Hash      *string `json:"hash"`
}

// Rule represents a single rule within an entity.
type Rule struct {
	ID         string  `json:"id"`
	Revision   int     `json:"revision"`
	State      string  `json:"state"`
	Body       string  `json:"body"`
	AddedBy    string  `json:"added_by"`
	AddedAt    string  `json:"added_at"`
	Supersedes *string `json:"supersedes"`
}

// Note represents an annotation attached to a specific rule.
type Note struct {
	ID       string `json:"id"`
	RuleRef  string `json:"rule_ref"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	AddedBy  string `json:"added_by"`
	AddedAt  string `json:"added_at"`
	Revision int    `json:"revision"`
}

// RealmFile represents a SteerSpec Realm manifest (realm.json).
type RealmFile struct {
	Schema               string      `json:"$schema"`
	Realm                RealmMeta   `json:"realm"`
	Dependencies         []RealmDep  `json:"dependencies,omitempty"`
	RuleIdentifierFormat interface{} `json:"rule_identifier_format,omitempty"`
}

// RealmMeta holds the identity and versioning for a Realm.
type RealmMeta struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Version string `json:"version"`
}

// RealmDep declares a dependency on another Realm.
type RealmDep struct {
	RealmID string `json:"realm_id"`
	Version string `json:"version"`
}
