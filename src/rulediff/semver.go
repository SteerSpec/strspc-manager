package rulediff

import semver "github.com/Masterminds/semver/v3"

// isNewerSemver reports whether head is strictly greater than base
// according to Semantic Versioning precedence rules (including pre-release
// ordering and ignoring build metadata). Returns false if either version
// cannot be parsed as a valid semantic version.
func isNewerSemver(base, head string) bool {
	b, err := semver.NewVersion(base)
	if err != nil {
		return false
	}
	h, err := semver.NewVersion(head)
	if err != nil {
		return false
	}
	return h.GreaterThan(b)
}
