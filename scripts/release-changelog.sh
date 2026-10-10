#!/usr/bin/env bash
# Writes the changelog of one tool's next release to standard output.
#
# It lists every commit since the tool's last release that changed the tool's
# files, sorted into breaking changes, features, fixes and everything else. The
# tool's last release is its newest tag, v<version>-<tag-suffix>; a tool that
# has never been released gets its whole history.
#
# A COMMIT IS LISTED BECAUSE IT CHANGED THE TOOL'S FILES, WHATEVER ITS SCOPE.
# That is how the release workflow counts a version: every commit that changed
# the tool's files is part of the release. A dependency update, or a change
# made under another tool's scope, is in the release as much as any other, so
# it is in the changelog too, under Other Changes. Only a commit under one of
# the tool's own scopes is listed as a feature or a fix of the tool, which is
# also the rule a feature is counted by for the version.
#
# The release workflow runs this once for each tool, so every tool's notes are
# sorted by the same rules. A breaking commit is listed under Breaking Changes
# only, not a second time further down.
#
# usage: release-changelog.sh <title> <version> <tag-suffix> <scopes> <path>
#   title       the tool's name as a person reads it:        "Simple CLI"
#   version     the version being released:                  1.4.0
#   tag-suffix  what follows the version in the tool's tags: simple-cli
#   scopes      the commit scopes that are this tool's, as
#               an alternation:                              "simple-cli|cli"
#   path        the directory the tool's files are in:       tools/simple-cli

set -euo pipefail

if [ "$#" -ne 5 ]; then
  echo "usage: release-changelog.sh <title> <version> <tag-suffix> <scopes> <path>" >&2
  exit 2
fi

title=$1
version=$2
suffix=$3
scopes=$4
path=$5

# The whole name has to match, so "v*-scl-parser" is not also every
# "v*-scl-parser-cli".
previous=$(git tag -l "v*-${suffix}" --sort=-version:refname | sed -n 1p)

if [ -n "$previous" ]; then
  commits=$(git log "${previous}..HEAD" --pretty=format:"* %s (%h)" -- "$path")
else
  commits=$(git log --pretty=format:"* %s (%h)" -- "$path")
fi

mine="\((${scopes})\)"

breaking=$(grep -E "^\* [a-z]+${mine}!:" <<< "$commits" || true)
features=$(grep -E "^\* feat${mine}:" <<< "$commits" || true)
fixes=$(grep -E "^\* fix${mine}:" <<< "$commits" || true)
other=$(grep -v -E "^\* ((feat|fix)${mine}:|[a-z]+${mine}!:)" <<< "$commits" || true)

section() {
  if [ -n "$2" ]; then
    printf '### %s\n%s\n\n' "$1" "$2"
  fi
}

printf '## %s %s\n\n' "$title" "$version"
section "⚠️ Breaking Changes" "$breaking"
section "✨ Features" "$features"
section "🐛 Bug Fixes" "$fixes"
section "📝 Other Changes" "$other"
