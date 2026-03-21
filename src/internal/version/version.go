// Package version holds build-time version metadata.
package version

var (
	// Version is the semantic version of this build.
	Version = "dev"
	// Commit is the Git commit hash of this build.
	Commit = "none"
	// Date is the timestamp when this binary was built.
	Date = "unknown"
)
