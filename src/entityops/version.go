package entityops

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/SteerSpec/strspc-manager/src/entity"
)

// maxRuleNumber is the maximum rule number supported by the 3-digit ID format.
const maxRuleNumber = 999

var (
	ruleIDRe = regexp.MustCompile(`^([A-Za-z0-9]+)-(\d{3})$`)

	// nowFunc is overridable in tests for deterministic timestamps.
	nowFunc = func() time.Time { return time.Now().UTC() }
)

// BumpPatch increments the patch component of a semver string.
func BumpPatch(version string) (string, error) {
	major, minor, patch, err := parseSemver(version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1), nil
}

// BumpMinor increments the minor component and resets patch to 0.
func BumpMinor(version string) (string, error) {
	major, minor, _, err := parseSemver(version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d.0", major, minor+1), nil
}

// NextRuleNumber scans existing rule IDs matching the entity's EUID prefix
// and returns max+1. Returns 1 if the file is nil or has no matching rules.
func NextRuleNumber(f *entity.File) int {
	if f == nil {
		return 1
	}
	max := 0
	for _, r := range f.Rules {
		m := ruleIDRe.FindStringSubmatch(r.ID)
		if m == nil || m[1] != f.Entity.ID {
			continue
		}
		n, err := strconv.Atoi(m[2])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1
}

// UpdateMeta sets the rule_set timestamp and recomputes the blake3 hash.
func UpdateMeta(f *entity.File) error {
	if f == nil {
		return fmt.Errorf("entity file is nil")
	}
	f.RuleSet.Timestamp = nowFunc().Format(time.RFC3339)

	data, err := json.Marshal(f)
	if err != nil {
		return fmt.Errorf("marshaling for hash: %w", err)
	}

	hash, err := entity.ComputeHash(data)
	if err != nil {
		return fmt.Errorf("computing hash: %w", err)
	}
	f.RuleSet.Hash = &hash
	return nil
}

// parseSemver extracts major.minor.patch from a version string.
func parseSemver(version string) (int, int, int, error) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) != 3 {
		return 0, 0, 0, fmt.Errorf("invalid semver: %q", version)
	}
	// Strip any pre-release or build metadata from patch.
	patchStr := parts[2]
	if idx := strings.IndexAny(patchStr, "-+"); idx >= 0 {
		patchStr = patchStr[:idx]
	}
	major, err := parseSemverComponent(version, "major", parts[0])
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err := parseSemverComponent(version, "minor", parts[1])
	if err != nil {
		return 0, 0, 0, err
	}
	patch, err := parseSemverComponent(version, "patch", patchStr)
	if err != nil {
		return 0, 0, 0, err
	}
	return major, minor, patch, nil
}

// parseSemverComponent validates and parses a single numeric semver component.
// It rejects leading zeros (except "0" itself) and negative/signed values.
func parseSemverComponent(version, label, s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("invalid semver %q: empty %s", version, label)
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("invalid semver %q: %s %q has leading zero", version, label, s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid semver %q: invalid %s %q: %w", version, label, s, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid semver %q: %s must be non-negative", version, label)
	}
	return n, nil
}
