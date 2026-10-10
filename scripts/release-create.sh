#!/usr/bin/env bash
# Creates one tool's release on GitHub: its tag, on the commit that was built,
# and the release that carries its notes and its files.
#
# The release workflow runs this from each tool's `publish` job, after that
# job's review has been approved.
#
# usage: release-create.sh <tag> <title> <notes-file> [file...]
#   tag         the tag to create:               v1.4.0-simple-cli
#   title       the release's title:             "Simple CLI 1.4.0"
#   notes-file  the changelog, as Markdown:      changelog.md
#   file        each file the release carries; a release may carry none
#
# It reads three things the workflow sets: GH_TOKEN, GITHUB_REPOSITORY and
# GITHUB_SHA.

set -euo pipefail

if [ "$#" -lt 3 ]; then
  echo "usage: release-create.sh <tag> <title> <notes-file> [file...]" >&2
  exit 2
fi

tag=$1
title=$2
notes=$3
shift 3

say() {
  echo "$1"
  if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
    echo "$1" >> "$GITHUB_STEP_SUMMARY"
  fi
}

# THE TAG IS CREATED WITH THE DEVOPS TOKEN, not the token a workflow is given.
# This repository lets only its administrators and its release account create
# a tag that starts with `v`, which is what stops a version being claimed by
# anyone who can push a branch. The workflow's own token is neither, so the
# release is created as the release account. The token is a secret of the
# environment the `publish` job waits on, so it is handed over only once the
# release has been approved.
if [ -z "${GH_TOKEN:-}" ]; then
  echo "This job's environment has no SIMPLE_DEVOPS_TOKEN secret, so the tag ${tag} cannot be created. Add the secret to that environment and run this job again." >&2
  exit 1
fi

# A RUN CAN BE ASKED TO PUBLISH A VERSION THAT IS ALREADY OUT.
#
# Every push to `main` computes the version a tool's unreleased commits have
# earned, so two pushes ahead of one release compute the same number. A run
# that was approved after another had published it would then fail on a tag
# that exists, with nothing wrong behind it. A job that created the release and
# then failed further on is run again the same way. So the tag is asked for
# first, and a version that has one is not released again.
#
# The tag is asked for by its whole name. `v1.0.2-scl-parser` is not found
# because `v1.0.2-scl-parser-cli` exists.
#
# A GitHub that cannot be reached reads as "not released". The release is then
# attempted, and GitHub itself is the judge of it.
if gh api "repos/${GITHUB_REPOSITORY}/git/ref/tags/${tag}" > /dev/null 2>&1; then
  say "The tag ${tag} exists. No release is created again."
  exit 0
fi

# THE TAG NAMES THE COMMIT THIS RUN BUILT. A release waits on a review, and
# `main` moves while it waits. Naming the commit puts the tag on the tree the
# release was built from, so the tag, the files and the changelog describe one
# commit, and the next version is computed over exactly what came after it.
gh release create "$tag" \
  --repo "$GITHUB_REPOSITORY" \
  --target "$GITHUB_SHA" \
  --title "$title" \
  --notes-file "$notes" \
  "$@"

say "Released ${title} as ${tag}."
