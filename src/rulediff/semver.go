package rulediff

import (
	"strconv"
	"strings"
)

// isNewerSemver reports whether head is strictly greater than base.
// Versions with pre-release or build metadata suffixes are treated as invalid
// (parseSemver returns nil), so isNewerSemver returns false for them.
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
// Versions with pre-release (-) or build metadata (+) suffixes are rejected.
func parseSemver(v string) []int {
	if strings.ContainsAny(v, "-+") {
		return nil
	}
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
