#!/usr/bin/env bash
# growth-metrics.sh — read-only snapshot of the adoption metrics tracked in
# docs/growth-metrics.md.
#
# Pulls everything GitHub exposes via its REST API (release-asset download
# counts per platform, issues opened by accounts other than the repo owner,
# forks) and prints one dated block: a markdown table row to paste into the
# monthly readings table in docs/growth-metrics.md, plus the detail behind it.
#
# Metrics with no API — currently just the Godot Asset Library install count —
# are printed as MANUAL lines naming where to read them, never omitted.
#
# Every call is a read-only GET. This script never pushes, never opens or edits
# an issue or release, and never mutates anything remote.
#
# Usage:
#   ./scripts/growth-metrics.sh
#   REPO=owner/name ./scripts/growth-metrics.sh
#
# Requires `gh` (https://cli.github.com). Unauthenticated `gh` can still read a
# public repo but hits a much lower rate limit; any metric whose call fails is
# reported as UNAVAILABLE rather than as a zero, so a throttled run never looks
# like a month of no adoption.

set -euo pipefail

DEFAULT_REPO="mrf/godot-stagehand"
ASSET_LIBRARY_SEARCH="https://godotengine.org/asset-library/asset?filter=stagehand"

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  # Everything after the shebang up to the first non-comment line, uncommented.
  awk 'NR > 1 { if (!/^#/) exit; sub(/^# ?/, ""); print }' "${BASH_SOURCE[0]}"
  exit 0
fi

if ! command -v gh >/dev/null 2>&1; then
  echo "ERROR: gh not found. Install the GitHub CLI: https://cli.github.com" >&2
  exit 1
fi

# Resolve the repo: explicit REPO wins, then the checkout's own remote, then the
# hardcoded default (so the script still works from outside a clone).
REPO="${REPO:-}"
if [[ -z "$REPO" ]]; then
  REPO="$(gh repo view --json nameWithOwner -q .nameWithOwner 2>/dev/null || true)"
fi
if [[ -z "$REPO" ]]; then
  REPO="$DEFAULT_REPO"
fi
OWNER="${REPO%%/*}"

TODAY="$(date -u +%Y-%m-%d)"
MONTH="$(date -u +%Y-%m)"

# api <path> [extra gh args...] — read-only GET. Prints nothing and returns 1 on
# failure so each metric can degrade to UNAVAILABLE independently.
api() {
  gh api --method GET "$@" 2>/dev/null
}

warn() {
  echo "  ! $*" >&2
}

if ! auth_login="$(gh api user --jq .login 2>/dev/null)"; then
  warn "gh is not authenticated — falling back to unauthenticated reads (60 req/hr)."
  auth_login="(none)"
fi

remaining="$(api rate_limit --jq '.resources.core.remaining' || true)"
if [[ -z "$remaining" ]]; then
  remaining="unknown"
fi

# ---------------------------------------------------------------------------
# Release-asset downloads, summed across every release, grouped by platform.
# Cumulative on purpose: the number only ever goes up, so month-over-month
# subtraction gives that month's downloads.
# ---------------------------------------------------------------------------
assets_raw=""
assets_ok=1
# shellcheck disable=SC2016  # $tag is a jq variable; the shell must not expand it.
if ! assets_raw="$(api "repos/$REPO/releases?per_page=100" --paginate \
  --jq '.[] | .tag_name as $tag | .assets[] | [$tag, .name, .download_count] | @tsv')"; then
  assets_ok=0
  warn "release list unavailable for $REPO"
fi

platform_summary=""
downloads_total=0
if [[ "$assets_ok" -eq 1 && -n "$assets_raw" ]]; then
  platform_summary="$(printf '%s\n' "$assets_raw" | awk -F'\t' '
    {
      platform = $2
      sub(/^godot-stagehand-/, "", platform)
      sub(/\.exe$/, "", platform)
      total[platform] += $3
      grand += $3
    }
    END {
      n = 0
      for (p in total) { keys[++n] = p }
      # Insertion sort: keeps output order stable across runs so a diff of two
      # months lines up row for row.
      for (i = 2; i <= n; i++) {
        k = keys[i]
        for (j = i - 1; j >= 1 && keys[j] > k; j--) keys[j + 1] = keys[j]
        keys[j + 1] = k
      }
      for (i = 1; i <= n; i++) printf "%s\t%d\n", keys[i], total[keys[i]]
      printf "TOTAL\t%d\n", grand
    }
  ')"
  downloads_total="$(printf '%s\n' "$platform_summary" | awk -F'\t' '$1 == "TOTAL" { print $2 }')"
elif [[ "$assets_ok" -eq 0 ]]; then
  downloads_total="UNAVAILABLE"
fi

# ---------------------------------------------------------------------------
# Issues opened by someone other than the repo owner. Uses the issues endpoint
# rather than the search API: no separate search rate limit, and it hands back
# the actual titles. PRs come back from the same endpoint, so drop anything
# carrying a `pull_request` key; drop bot accounts too (Dependabot files PRs,
# not issues, but a future bot might).
# ---------------------------------------------------------------------------
issues_raw=""
issues_ok=1
if ! issues_raw="$(api "repos/$REPO/issues?state=all&per_page=100" --paginate \
  --jq ".[] | select(.pull_request == null) | select(.user.type != \"Bot\") | select(.user.login != \"$OWNER\") | [.number, .state, .user.login, .created_at, .title] | @tsv")"; then
  issues_ok=0
  warn "issue list unavailable for $REPO"
fi

external_issues=0
if [[ "$issues_ok" -eq 1 && -n "$issues_raw" ]]; then
  external_issues="$(printf '%s\n' "$issues_raw" | awk 'END { print NR }')"
fi

# ---------------------------------------------------------------------------
# Forks. Stars are read too, but only so the doc's "stars are vanity" claim has
# a number next to it — the table does not have a stars column.
# ---------------------------------------------------------------------------
forks="UNAVAILABLE"
stars="UNAVAILABLE"
if repo_json="$(api "repos/$REPO" --jq '[.forks_count, .stargazers_count] | @tsv')"; then
  forks="$(printf '%s' "$repo_json" | cut -f1)"
  stars="$(printf '%s' "$repo_json" | cut -f2)"
else
  warn "repo metadata unavailable for $REPO"
fi

fork_detail=""
if fork_raw="$(api "repos/$REPO/forks?per_page=100" --paginate \
  --jq '.[] | [.full_name, .created_at, (.pushed_at // "never")] | @tsv')"; then
  fork_detail="$fork_raw"
fi

# ---------------------------------------------------------------------------
# Output
# ---------------------------------------------------------------------------
# dl_cell <platform> — that platform's cumulative download count for the table.
# UNAVAILABLE if the release read failed, 0 if the platform has no asset yet
# (so a dropped or renamed build shows as 0 rather than an empty cell).
dl_cell() {
  local value
  if [[ "$assets_ok" -eq 0 ]]; then
    echo "UNAVAILABLE"
    return
  fi
  value="$(printf '%s\n' "$platform_summary" | awk -F'\t' -v p="$1" '$1 == p { print $2 }')"
  echo "${value:-0}"
}

echo "# growth metrics — $TODAY (repo: $REPO, gh account: $auth_login, core rate limit left: $remaining)"
echo

echo "## Release-asset downloads (cumulative, all releases)"
if [[ "$assets_ok" -eq 0 ]]; then
  echo "UNAVAILABLE — could not read releases."
elif [[ -z "$assets_raw" ]]; then
  echo "No releases with assets published yet."
else
  printf '%s\n' "$platform_summary" | awk -F'\t' '{ printf "  %-28s %s\n", $1, $2 }'
  echo
  echo "  per release/asset:"
  printf '%s\n' "$assets_raw" | awk -F'\t' '{ printf "    %-10s %-34s %s\n", $1, $2, $3 }'
fi
echo

echo "## Godot Asset Library installs"
echo "MANUAL — the Asset Library exposes no install count this script can read."
echo "  Open the listing and record the number shown there:"
echo "    $ASSET_LIBRARY_SEARCH"
echo "  If the listing is still pending review there is no number yet — record 'pending'."
echo

echo "## Issues opened by non-$OWNER accounts"
if [[ "$issues_ok" -eq 0 ]]; then
  echo "UNAVAILABLE — could not read issues."
elif [[ "$external_issues" -eq 0 ]]; then
  echo "  0 — nobody outside $OWNER has filed an issue yet."
else
  echo "  $external_issues total (open + closed):"
  printf '%s\n' "$issues_raw" | awk -F'\t' '{ printf "    #%-5s %-7s %-18s %s  %s\n", $1, $2, $3, $4, $5 }'
fi
echo

echo "## Forks"
echo "  count: $forks"
if [[ -n "$fork_detail" ]]; then
  printf '%s\n' "$fork_detail" | awk -F'\t' '{ printf "    %-40s created %s  last push %s\n", $1, $2, $3 }'
fi
echo "  (stars, for reference only — not tracked in the table: $stars)"
echo

echo "## Paste this row into docs/growth-metrics.md"
echo
echo "| Month | linux-amd64 | darwin-amd64 | darwin-arm64 | windows-amd64 | Total DL | AL installs | Non-owner issues | Forks |"
echo "| ----- | ----------- | ------------ | ------------ | ------------- | -------- | ----------- | ---------------- | ----- |"
printf '| %s | %s | %s | %s | %s | %s | %s | %s | %s |\n' \
  "$MONTH" \
  "$(dl_cell linux-amd64)" \
  "$(dl_cell darwin-amd64)" \
  "$(dl_cell darwin-arm64)" \
  "$(dl_cell windows-amd64)" \
  "$downloads_total" \
  "FILL-IN" \
  "$(if [[ "$issues_ok" -eq 0 ]]; then echo UNAVAILABLE; else echo "$external_issues"; fi)" \
  "$forks"
