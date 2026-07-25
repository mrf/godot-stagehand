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
# .uid sidecars are the one exception to "byte-for-byte": canonical
# addons/stagehand has no project.godot of its own, so Godot never assigns it
# UIDs, but each copy lives inside a real project and carries its own
# editor-assigned .uid files, committed per docs/addon-sync-contract.md. A
# naive `rm -rf && cp -R` would delete those on every sync and force Godot to
# reassign fresh UIDs next time the project is opened, churning every
# ext_resource that references the addon. This script preserves any .uid
# sidecar already present at the destination for a path that still exists
# after the sync; a newly added addon script has no .uid to preserve, so open
# the project in the editor once after syncing to mint one, then commit it.
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
  uid_holding="$(mktemp -d)"
  if [[ -d "$dst" ]]; then
    (cd "$dst" && find . -name '*.uid' -exec cp --parents {} "$uid_holding" \;)
  fi

  rm -rf "$dst"
  mkdir -p "$(dirname "$dst")"
  cp -R "$SRC" "$dst"

  (cd "$uid_holding" && find . -name '*.uid' -print0) | while IFS= read -r -d '' uid_rel; do
    src_rel="${uid_rel%.uid}"
    if [[ -f "$dst/$src_rel" ]]; then
      mkdir -p "$dst/$(dirname "$uid_rel")"
      cp "$uid_holding/$uid_rel" "$dst/$uid_rel"
    fi
  done
  rm -rf "$uid_holding"

  echo "Synced $SRC -> $dst"
done
