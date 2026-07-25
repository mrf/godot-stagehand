#!/usr/bin/env bash
# sync-addon-copies.sh — Re-sync the checked-in addon fixtures from canonical.
#
# addons/stagehand is the only authoritative source (it's what assets.go
# embeds into the release binary). testdata/test_project and
# examples/minimal-game each carry their own checked-in copy so that a plain
# `git clone` + "open in Godot" workflow keeps working with no build step.
# Run this script after any change under addons/stagehand so those copies
# stay byte-for-byte identical; TestFixtureAddonCopiesMatchCanonical
# (addon_copy_drift_test.go) fails the build if they drift.
#
# Usage:
#   ./scripts/sync-addon-copies.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

SRC="$REPO_ROOT/addons/stagehand"
TARGETS=(
  "$REPO_ROOT/testdata/test_project/addons/stagehand"
  "$REPO_ROOT/examples/minimal-game/addons/stagehand"
)

if [[ ! -d "$SRC" ]]; then
  echo "ERROR: canonical addon not found at $SRC" >&2
  exit 1
fi

for dst in "${TARGETS[@]}"; do
  rm -rf "$dst"
  mkdir -p "$(dirname "$dst")"
  cp -R "$SRC" "$dst"
  echo "Synced $SRC -> $dst"
done
