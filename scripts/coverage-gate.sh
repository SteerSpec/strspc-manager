#!/usr/bin/env bash
# Fails when any package's statement coverage is under the floor (GOTEST-001).
# Each package must earn its figure from its own tests, so this reads the line
# `go test` prints per package and never uses -coverpkg.
#
# Packages with nothing to cover are listed and skipped: `[no test files]`
# (only declarations, such as src/internal/version) and `[no statements]`
# (test-only packages). A package with statements but no tests reports 0.0%
# and fails.
set -euo pipefail

threshold="${COVERAGE_THRESHOLD:-85}"
output="$(mktemp)"
trap 'rm -f "$output"' EXIT

# Fail closed: a failing test run never reaches the coverage check.
if ! go test -race -coverprofile=coverage.out ./... >"$output" 2>&1; then
  cat "$output"
  echo "Tests failed; coverage was not checked" >&2
  exit 1
fi

report="$(awk -v t="$threshold" '
  function name(i) {
    for (i = 1; i <= NF; i++) if ($i ~ /\//) return $i
    return "?"
  }
  /no test files|\[no statements\]/ {
    printf "skip  %s\n", name()
    next
  }
  match($0, /coverage: [0-9.]+%/) {
    pct = substr($0, RSTART + 10, RLENGTH - 11) + 0
    checked++
    if (pct < t + 0) {
      failed++
      printf "FAIL  %5.1f%%  %s\n", pct, name()
    } else {
      printf "ok    %5.1f%%  %s\n", pct, name()
    }
  }
  END {
    if (!checked) {
      print "FAIL  no package reported coverage"
    } else if (failed) {
      printf "FAIL  %d of %d packages under %s%%\n", failed, checked, t
    } else {
      printf "ok    all %d packages at or above %s%%\n", checked, t
    }
  }
' "$output")"

echo "$report"

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "## Coverage (floor: ${threshold}% per package)"
    echo
    echo '```'
    echo "$report"
    echo '```'
    echo
    echo '<details><summary>Per-function breakdown</summary>'
    echo
    echo '```'
    go tool cover -func=coverage.out
    echo '```'
    echo
    echo '</details>'
  } >>"$GITHUB_STEP_SUMMARY"
fi

if echo "$report" | tail -n 1 | grep -q '^FAIL'; then
  exit 1
fi
