#!/usr/bin/env bash
# Detects the hidden-code shape used in the July 2026 supply-chain incident.
#
# Modes:
#   --staged          blobs in the Git index (pre-commit)
#   --pre-push        commits named on the standard pre-push stdin
#   --commit REF      the complete tree at REF (post-pull/checkout and CI)
#   --tree            tracked files in the current working tree (manual check)

set -uo pipefail

MODE="${1:---staged}"
REF="${2:-HEAD}"
RUN=100
hits=0

source_file() {
  case "$1" in
    */node_modules/*) return 1 ;;
    *.js|*.mjs|*.cjs|*.ts|*.tsx|*.jsx) return 0 ;;
    *) return 1 ;;
  esac
}

has_incident_shape() {
  awk -v n="$RUN" '
    index($0, sprintf("%*s", n, "")) > 0 { found=1; exit }
    /global\.o=[\047\042][^\047\042]+[\047\042];var _\$_/ { found=1; exit }
    END { exit !found }
  '
}

report_hit() {
  if [ "$hits" -eq 0 ]; then
    printf '\n  BLOCKED: source contains the July 2026 hidden-loader shape\n\n'
  fi
  printf '    %s\n' "$1"
  hits=$((hits + 1))
}

scan_staged() {
  local path
  while IFS= read -r -d '' path; do
    source_file "$path" || continue
    git cat-file -e ":$path" 2>/dev/null || continue
    if git show ":$path" | has_incident_shape; then
      report_hit "index:$path"
    fi
  done < <(git diff --cached --name-only --diff-filter=ACMR -z)
}

scan_tree() {
  local path
  while IFS= read -r -d '' path; do
    source_file "$path" || continue
    [ -f "$path" ] || continue
    if has_incident_shape < "$path"; then
      report_hit "working-tree:$path"
    fi
  done < <(git ls-files -z)
}

scan_commit() {
  local commit="$1"
  local label="${2:-$1}"
  local path

  if ! git cat-file -e "$commit^{tree}" 2>/dev/null; then
    printf 'supply-chain guard: cannot resolve tree-ish %s\n' "$commit" >&2
    return 2
  fi

  while IFS= read -r -d '' path; do
    source_file "$path" || continue
    if git show "$commit:$path" | has_incident_shape; then
      report_hit "$label:$path"
    fi
  done < <(git ls-tree -r --name-only -z "$commit")
}

scan_pre_push() {
  local local_ref local_oid remote_ref remote_oid
  local zero=0000000000000000000000000000000000000000

  while read -r local_ref local_oid remote_ref remote_oid; do
    [ "$local_oid" = "$zero" ] && continue
    scan_commit "$local_oid" "$local_ref"
  done
}

case "$MODE" in
  --staged) scan_staged ;;
  --pre-push) scan_pre_push ;;
  --commit) scan_commit "$REF" "$REF" ;;
  --tree) scan_tree ;;
  *)
    printf 'usage: %s [--staged|--pre-push|--commit REF|--tree]\n' "$0" >&2
    exit 2
    ;;
esac

if [ "$hits" -gt 0 ]; then
  cat <<'MSG'

  Do not execute, build, install, or deploy this ref. Restore affected files
  from a known-clean commit and contact the incident owner.

MSG
  exit 1
fi

exit 0
