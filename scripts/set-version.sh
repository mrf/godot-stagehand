#!/usr/bin/env bash
# set-version.sh — propagate the authoritative Stagehand version to every mirror.
#
# internal/version/version.go holds the authoritative constant. This script
# rewrites it and every mirror that must agree with it:
#
#   internal/version/version.go               const Version
#   addons/stagehand/plugin.cfg               version=
#   addons/stagehand/stagehand_version.gd     const VERSION
#
# The addon fixtures under testdata/ and examples/ are then re-synced from
# canonical by scripts/sync-addon-copies.sh, per docs/addon-sync-contract.md —
# this script never edits them directly, so they cannot half-update.
#
# Agreement is enforced by `go test ./internal/version/`, so a hand-edit that
# misses a mirror fails CI rather than shipping. See docs/versioning.md.
#
# Usage: ./scripts/set-version.sh 0.3.0

set -euo pipefail

if [ $# -ne 1 ]; then
    echo "Usage: $0 <version>   (e.g. $0 0.3.0)" >&2
    exit 2
fi

VERSION="$1"
if ! [[ "$VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
    echo "Error: version must be MAJOR.MINOR.PATCH, got '$VERSION'" >&2
    exit 2
fi

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

replace_in_place() {
    local file="$1" expression="$2"
    sed -i.bak -E "$expression" "$file"
    rm -f "$file.bak"
    echo "Updated $file to $VERSION"
}

replace_in_place internal/version/version.go \
    "s/^const Version = \"[^\"]*\"/const Version = \"$VERSION\"/"
replace_in_place addons/stagehand/plugin.cfg \
    "s/^version=\"[^\"]*\"/version=\"$VERSION\"/"
replace_in_place addons/stagehand/stagehand_version.gd \
    "s/^const VERSION: String = \"[^\"]*\"/const VERSION: String = \"$VERSION\"/"

# Invoked through bash rather than as ./scripts/… because the checked-in file
# mode is not executable.
bash ./scripts/sync-addon-copies.sh

echo
echo "Now run: go test ./internal/version/"
