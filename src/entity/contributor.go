package entity

// Contributor is an entry of a Realm's contributor list (CTRB-004).
type Contributor struct {
	// ID is the contributor identifier `<idp>:<subject>` (CTRB-001), such as
	// `github:pleberre`.
	ID string `json:"id"`
	// Email is the contributor's email address (CTRB-007). Empty means none.
	Email string `json:"email,omitempty"`
	// GitHubID is the numeric GitHub user ID of a `github` contributor
	// (CTRB-008). Zero means none; the schema requires at least 1.
	GitHubID int64 `json:"github_id,omitempty"`
}
