#!/usr/bin/env bash
# hostcompat-checkout.sh — check out one pinned host project for the
# host-project compatibility suite.
#
# Reads testdata/hostcompat/manifest.json, clones the project's `repo` into
# .hostcompat/<id> at its pinned `commit`, and verifies HEAD == commit. On
# success the checkout path is the only line on stdout. If the checkout already
# exists (a restored cache) it is reused when HEAD is the pin and refused
# otherwise — a directory this script did not just create is never reset.
#
# If `commit` is null the pin has not been resolved: `ref` is resolved with
# `git ls-remote`, the SHA is printed on stdout for pasting into the manifest,
# and nothing is cloned. A pin nobody reviewed is not something to run.
#
# Exit codes (a stable contract — CI branches on them):
#   0   checked out at the pin (or already was)
#   2   usage or manifest error: bad arguments, unknown id, malformed commit
#   3   pin unresolved: `commit` is null; resolved SHA printed on stdout
#   75  infrastructure: clone/fetch/ls-remote failed, the pinned commit could
#       not be fetched, or HEAD != pin. Retry; NEVER report as a compat failure
#       (docs/design/host-project-compat-suite.md, Amendment 4). 75 is EX_TEMPFAIL
#       and nothing else in this script, git (128) or jq exits with it.
#
# Environment (both are for tests; the defaults are what CI uses):
#   HOSTCOMPAT_MANIFEST   manifest path (default testdata/hostcompat/manifest.json)
#   HOSTCOMPAT_DEST_DIR   checkout root (default .hostcompat)
#
# Usage: scripts/hostcompat-checkout.sh <id>

set -euo pipefail

readonly EXIT_USAGE=2
readonly EXIT_UNPINNED=3
readonly EXIT_INFRA=75

# Never prompt for credentials: a private or missing repo must fail fast, not
# hang a CI runner waiting on a terminal.
export GIT_TERMINAL_PROMPT=0

die() {
    local code="$1"
    shift
    echo "hostcompat-checkout: $*" >&2
    exit "$code"
}

if [ $# -ne 1 ]; then
    die "$EXIT_USAGE" "usage: $0 <id>"
fi
id="$1"
if ! [[ "$id" =~ ^[a-z0-9][a-z0-9-]*$ ]]; then
    die "$EXIT_USAGE" "id '$id' must match [a-z0-9][a-z0-9-]* (it becomes a directory name)"
fi
command -v jq >/dev/null 2>&1 || die "$EXIT_USAGE" "jq is required"
command -v git >/dev/null 2>&1 || die "$EXIT_USAGE" "git is required"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${HOSTCOMPAT_MANIFEST:-$repo_root/testdata/hostcompat/manifest.json}"
dest_root="${HOSTCOMPAT_DEST_DIR:-$repo_root/.hostcompat}"
[ -f "$manifest" ] || die "$EXIT_USAGE" "manifest not found: $manifest"

# `// empty` maps both a missing key and an explicit null to "".
field() {
    jq -r --arg id "$id" '.projects[] | select(.id == $id) | .'"$1"' // empty' "$manifest"
}
if [ "$(jq -r --arg id "$id" '[.projects[] | select(.id == $id)] | length' "$manifest")" != "1" ]; then
    die "$EXIT_USAGE" "no single project with id '$id' in $manifest"
fi
repo="$(field repo)"
ref="$(field ref)"
commit="$(field commit)"
[ -n "$repo" ] || die "$EXIT_USAGE" "project '$id' has no repo"

# resolve_ref prints the commit SHA that $ref points at. An annotated tag's own
# object id is not a commit, so the peeled (^{}) line wins over the tag line,
# and tags win over branches.
resolve_ref() {
    local listing
    if ! listing="$(git ls-remote "$repo" "refs/tags/$ref^{}" "refs/tags/$ref" "refs/heads/$ref")"; then
        die "$EXIT_INFRA" "git ls-remote $repo failed"
    fi
    local pattern sha
    for pattern in "refs/tags/$ref^{}" "refs/tags/$ref" "refs/heads/$ref"; do
        sha="$(printf '%s\n' "$listing" | awk -v want="$pattern" '$2 == want { print $1; exit }')"
        if [ -n "$sha" ]; then
            printf '%s\n' "$sha"
            return 0
        fi
    done
    die "$EXIT_INFRA" "ref '$ref' not found in $repo"
}

if [ -z "$commit" ]; then
    [ -n "$ref" ] || die "$EXIT_USAGE" "project '$id' has neither commit nor ref"
    resolve_ref
    echo "hostcompat-checkout: '$id' has no pinned commit; paste the SHA above into the manifest" >&2
    exit "$EXIT_UNPINNED"
fi
if ! [[ "$commit" =~ ^[0-9a-f]{40}$ ]]; then
    die "$EXIT_USAGE" "project '$id' commit '$commit' is not a 40-hex SHA"
fi

dest="$dest_root/$id"

# verify_head fails as infrastructure unless the checkout at $1 is at the pin.
verify_head() {
    local dir="$1" head
    if ! head="$(git -C "$dir" rev-parse HEAD 2>/dev/null)"; then
        die "$EXIT_INFRA" "$dir is not a usable git checkout; remove it and retry"
    fi
    if [ "$head" != "$commit" ]; then
        die "$EXIT_INFRA" "SHA mismatch for '$id': checkout HEAD is $head, manifest pins $commit; remove $dir and retry"
    fi
}

if [ -e "$dest" ]; then
    verify_head "$dest"
    printf '%s\n' "$dest"
    exit 0
fi

# Build in a scratch dir and rename, so an interrupted clone never leaves a
# half-made $dest for the next run to mistake for a cache hit.
mkdir -p "$dest_root"
scratch="$(mktemp -d "$dest_root/.checkout-$id.XXXXXX")"
trap 'rm -rf "$scratch"' EXIT

git -C "$scratch" init -q || die "$EXIT_INFRA" "git init failed in $scratch"
git -C "$scratch" remote add origin "$repo" || die "$EXIT_INFRA" "git remote add failed"
# Fetch the single pinned commit; fall back to the full history for servers
# that refuse to serve an arbitrary SHA.
if ! git -C "$scratch" fetch -q --depth 1 origin "$commit" 2>/dev/null; then
    git -C "$scratch" fetch -q origin 2>/dev/null || die "$EXIT_INFRA" "could not fetch $repo"
fi
git -C "$scratch" -c advice.detachedHead=false checkout -q --detach "$commit" 2>/dev/null ||
    die "$EXIT_INFRA" "commit $commit is not available in $repo"

verify_head "$scratch"
mv "$scratch" "$dest"
trap - EXIT
printf '%s\n' "$dest"
