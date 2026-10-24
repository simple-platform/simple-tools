#!/usr/bin/env bash
# Writes the version being released into an Elixir project's mix.exs, in this
# checkout only.
#
# THE VERSION OF RECORD IS THE TAG, and the release workflow commits nothing
# back. So the number written in a mix.exs in the repository is not the version
# of the next release: it is whatever it was when it was last edited by hand.
# What is built and published has to carry the version the workflow computed,
# and mix reads a project's version from this one line, so the line is
# rewritten on the runner before anything is built.
#
# It refuses a file it does not recognise, rather than build a release that
# carries the wrong number: the file has to hold exactly one line of the form
#     version: "1.2.3",
#
# usage: release-set-mix-version.sh <mix.exs> <version>

set -euo pipefail

if [ "$#" -ne 2 ]; then
  echo "usage: release-set-mix-version.sh <mix.exs> <version>" >&2
  exit 2
fi

file=$1
version=$2

if ! grep -q -E '^[0-9]+\.[0-9]+\.[0-9]+$' <<< "$version"; then
  echo "'${version}' is not a version of the form 1.2.3." >&2
  exit 1
fi

line='^( *version: )"[0-9]+\.[0-9]+\.[0-9]+",$'

found=$(grep -c -E "$line" "$file" || true)
if [ "$found" != "1" ]; then
  echo "${file} holds ${found} lines of the form 'version: \"1.2.3\",' and exactly one is expected, so the version ${version} was not written into it." >&2
  exit 1
fi

sed -i.previous -E "s/${line}/\\1\"${version}\",/" "$file"
rm "${file}.previous"

echo "${file} now reads:$(grep -E "$line" "$file")"
