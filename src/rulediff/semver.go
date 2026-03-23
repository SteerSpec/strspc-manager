package rulediff

import (
	"strconv"
	"strings"
)

// isNewerSemver reports whether head is strictly greater than base.
// Pre-release and build metadata suffixes are stripped before comparison.
// Returns false if either version cannot be parsed.
func isNewerSemver(base, head string) bool {
	b := parseSemver(base)
	h := parseSemver(head)
	if b == nil || h == nil {
		return false
	}
	for i := range b {
		if h[i] > b[i] {
			return true
		}
		if h[i] < b[i] {
			return false
		}
	}
	return false // equal is not strictly greater
}

// parseSemver returns [major, minor, patch] as ints, or nil on failure.
func parseSemver(v string) []int {
	v, _, _ = strings.Cut(v, "-") // strip pre-release (e.g. "1.0.0-beta")
	v, _, _ = strings.Cut(v, "+") // strip build metadata (e.g. "1.0.0+build")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return nums
}
