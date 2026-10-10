#!/usr/bin/env bash
# Writes the changelog of one tool's next release to standard output.
#
# It lists the commits since the tool's last release that changed the tool's
# files, sorted into breaking changes, features, fixes and everything else. The
# tool's last release is its newest tag, v<version>-<tag-suffix>; a tool that
# has never been released gets its whole history.
#
# A COMMIT MADE FOR ANOTHER TOOL IS LEFT OUT, EVEN WHEN IT CHANGED THIS TOOL'S
# FILES. A commit names the tool it was made for in its scope, and a tool's
# changelog is where a reader looks for what was done to that tool. A commit
# made for the Simple CLI that also touched three files of the Contextualizer
# would read, in the Contextualizer's notes, as a change to the Contextualizer,
# and its subject describes something else.
#
# What is listed, then, is a commit that changed the tool's files and either
#   * carries one of the tool's own scopes, or
#   * carries no tool's scope at all: a dependency update, say, or a commit
#     with no scope.
# Only a commit under one of the tool's own scopes is listed as a feature or a
# fix of the tool. The rest are Other Changes. A breaking commit is listed
# under Breaking Changes only, not a second time further down.
#
# THE VERSION DOES NOT FOLLOW THIS RULE. The release workflow counts every
# commit that changed a tool's files, so a commit made for another tool still
# makes this tool's next release due. That release's changelog does not mention
# it, and can be empty.
#
# The release workflow runs this once for each tool, so every tool's notes are
# sorted by the same rules.
#
# usage: release-changelog.sh <title> <version> <tag-suffix> <scopes> <path> <every-scope>
#   title        the tool's name as a person reads it:        "Simple CLI"
#   version      the version being released:                  1.4.0
#   tag-suffix   what follows the version in the tool's tags: simple-cli
#   scopes       the commit scopes that are this tool's, as
#                an alternation:                              "simple-cli|cli"
#   path         the directory the tool's files are in:       tools/simple-cli
#   every-scope  the scopes of every tool released from this
#                repository, this one's among them, as an
#                alternation:                "simple-cli|cli|contextualizer"

set -euo pipefail

if [ "$#" -ne 6 ]; then
  echo "usage: release-changelog.sh <title> <version> <tag-suffix> <scopes> <path> <every-scope>" >&2
  exit 2
fi

title=$1
version=$2
suffix=$3
scopes=$4
path=$5
every=$6

# With no list of the tools' scopes nothing could be told apart, and every
# commit would be listed again as if this rule were not here.
if [ -z "$every" ]; then
  echo "release-changelog.sh was given no scopes for <every-scope>, so it cannot tell a commit made for another tool from one made for this one." >&2
  exit 2
fi

# The whole name has to match, so "v*-scl-parser" is not also every
# "v*-scl-parser-cli".
previous=$(git tag -l "v*-${suffix}" --sort=-version:refname | sed -n 1p)

if [ -n "$previous" ]; then
  commits=$(git log "${previous}..HEAD" --pretty=format:"* %s (%h)" -- "$path")
else
  commits=$(git log --pretty=format:"* %s (%h)" -- "$path")
fi

# Another tool's scopes are every tool's, less this tool's own.
others=""
IFS='|' read -r -a known <<< "$every"
for scope in "${known[@]}"; do
  case "|${scopes}|" in
    *"|${scope}|"*) ;;
    *) others="${others:+${others}|}${scope}" ;;
  esac
done

if [ -n "$others" ]; then
  commits=$(grep -v -E "^\* [a-z]+\((${others})\)!?:" <<< "$commits" || true)
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
