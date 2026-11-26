#!/usr/bin/env bash

set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
fixture=$(mktemp)
trap 'rm -f "$fixture"' EXIT

cat >"$fixture" <<'EOF'
## [Unreleased]

### Added
- incoming added one (#20).
- incoming added two (#19).
- branch added (#12).

### Fixed
- incoming fixed (#21).
- branch fixed (#12).

### Changed
- incoming changed one (#23).
- incoming changed two (#22).
- branch changed (#12).

## [1.0.0] - 2026-01-01

### Added
- released entry (#12).
EOF

"$repo_root/scripts/prioritize-changelog.sh" 12 "$fixture"
"$repo_root/scripts/prioritize-changelog.sh" 12 "$fixture"

expected=$(cat <<'EOF'
## [Unreleased]

### Added
- branch added (#12).
- incoming added one (#20).
- incoming added two (#19).

### Fixed
- branch fixed (#12).
- incoming fixed (#21).

### Changed
- branch changed (#12).
- incoming changed one (#23).
- incoming changed two (#22).

## [1.0.0] - 2026-01-01

### Added
- released entry (#12).
EOF
)

if [[ $(cat "$fixture") != "$expected" ]]; then
  diff -u <(printf '%s\n' "$expected") "$fixture"
  exit 1
fi
