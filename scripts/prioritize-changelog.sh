#!/usr/bin/env bash

set -euo pipefail

if [[ $# -lt 1 || $# -gt 2 || ! $1 =~ ^[0-9]+$ ]]; then
  echo "usage: $0 <pr-number> [changelog]" >&2
  exit 2
fi

pr_number=$1
changelog=${2:-CHANGELOG.md}
temporary=$(mktemp "${changelog}.XXXXXX")
trap 'rm -f "$temporary"' EXIT

awk -v pr="$pr_number" '
  BEGIN { unreleased = 0; section = "" }
  /^## \[Unreleased\]$/ { unreleased = 1 }
  unreleased && /^## \[/ && !/^## \[Unreleased\]$/ { unreleased = 0; section = "" }
  unreleased && /^### (Added|Fixed|Changed)$/ { section = substr($0, 5) }
  unreleased && section != "" && $0 ~ "\\(#" pr "\\)\\.$" {
    count[section]++
    entries[section, count[section]] = $0
    next
  }
  { lines[++line_count] = $0 }
  END {
    unreleased = 0
    for (line = 1; line <= line_count; line++) {
      current = lines[line]
      print current
      if (current == "## [Unreleased]") {
        unreleased = 1
      } else if (unreleased && current ~ /^## \[/) {
        unreleased = 0
      } else if (unreleased && current ~ /^### (Added|Fixed|Changed)$/) {
        section = substr(current, 5)
        for (entry = 1; entry <= count[section]; entry++) {
          print entries[section, entry]
        }
      }
    }
  }
' "$changelog" >"$temporary"

cp "$temporary" "$changelog"
rm -f "$temporary"
trap - EXIT
