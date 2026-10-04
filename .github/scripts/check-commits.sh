#!/usr/bin/env bash
# Checks that commit subjects follow Conventional Commits
# (https://www.conventionalcommits.org). Usage: check-commits.sh <range>,
# e.g. origin/main..HEAD; with no range it checks HEAD alone.
set -euo pipefail

pattern='^(feat|fix|docs|test|ci|refactor|perf|build|chore|revert)(\([a-z0-9-]+\))?!?: [^ ].*'
range="${1:-HEAD^!}"
log="$(git log --format='%H %s' "$range")"
bad=0
while IFS= read -r line; do
  [ -n "$line" ] || continue
  sha="${line%% *}"
  subject="${line#* }"
  case "$subject" in Merge\ *|Revert\ \"*) continue ;; esac
  if ! [[ "$subject" =~ $pattern ]]; then
    echo "::error::${sha:0:7} \"$subject\" is not a Conventional Commit (type(scope): summary; see CONTRIBUTING.md)"
    bad=1
  fi
done <<< "$log"
exit "$bad"
