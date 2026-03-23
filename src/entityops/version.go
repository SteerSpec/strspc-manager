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

var (
	ruleIDRe = regexp.MustCompile(`^([A-Za-z0-9]+)-(\d{3,})$`)

	// nowFunc is overridable in tests for deterministic timestamps.
	nowFunc = func() time.Time { return time.Now().UTC() }
)

// BumpPatch increments the patch component of a semver string.
func BumpPatch(version string) string {
	major, minor, patch, err := parseSemver(version)
	if err != nil {
		return version
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch+1)
}

// BumpMinor increments the minor component and resets patch to 0.
func BumpMinor(version string) string {
	major, minor, _, err := parseSemver(version)
	if err != nil {
		return version
	}
	return fmt.Sprintf("%d.%d.0", major, minor+1)
}

// NextRuleNumber scans existing rule IDs and returns max+1 (or 1 if empty).
func NextRuleNumber(f *entity.File) int {
	max := 0
	for _, r := range f.Rules {
		m := ruleIDRe.FindStringSubmatch(r.ID)
		if m == nil {
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
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, 0, err
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, 0, err
	}
	patch, err := strconv.Atoi(patchStr)
	if err != nil {
		return 0, 0, 0, err
	}
	return major, minor, patch, nil
}
