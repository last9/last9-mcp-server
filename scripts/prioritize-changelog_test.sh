#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp)
trap 'rm -f "$fixture"' EXIT

check_fixed_merge() {
  local incoming=$1
  local before=$2
  local expected=$3

  printf '## [Unreleased]\n\n### Fixed\n%s- branch fix (#12).\n\n## [1.0.0] - 2026-01-01\n\n### Fixed\n- released fix (#12).\n' "$before" >"$fixture"

  "$repo_root/scripts/prioritize-changelog.sh" 12 "$fixture"
  "$repo_root/scripts/prioritize-changelog.sh" 12 "$fixture"

  actual=$(awk '/^### Fixed$/ { found++ } found == 1 { print } found == 1 && /^$/ { exit }' "$fixture")
  if [[ $actual != "$expected" ]]; then
    echo "$incoming incoming Fixed entries:" >&2
    diff -u <(printf '%s\n' "$expected") <(printf '%s\n' "$actual")
    exit 1
  fi
}

check_fixed_merge 0 '' $'### Fixed\n- branch fix (#12).'
check_fixed_merge 1 $'- incoming fix (#20).\n' $'### Fixed\n- branch fix (#12).\n- incoming fix (#20).'
check_fixed_merge 2 $'- incoming fix (#21).\n- incoming fix (#20).\n' $'### Fixed\n- branch fix (#12).\n- incoming fix (#21).\n- incoming fix (#20).'
