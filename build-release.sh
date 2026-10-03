#!/bin/bash

# Script to build Stagehand binaries for multiple platforms
# Usage: ./build-release.sh [version] [--verify-only]
#
# When a version is supplied it is treated as the release tag and VERIFIED
# against the version compiled into the sources — it is not written into them.
# Bumping the version is a deliberate, committed act (scripts/set-version.sh),
# so a tag that disagrees with the tree is a mistake, not something to paper
# over at build time. See docs/versioning.md.

set -euo pipefail

VERSION=${1:-"latest"}
VERIFY_ONLY=""
if [ "${2:-}" = "--verify-only" ]; then
    VERIFY_ONLY=1
fi
BUILD_DIR="build"

# ── Version contract ─────────────────────────────────────────────────────────
# Every reported version must equal the tag: the Go constant, each addon
# plugin.cfg, each addon stagehand_version.gd, and the Claude Code plugin
# mirrors.
verify_versions() {
    local expected="$1"
    local failed=0

    local source_version
    source_version=$(sed -nE 's/^const Version = "([^"]*)"$/\1/p' internal/version/version.go)
    if [ "$source_version" != "$expected" ]; then
        echo "Error: internal/version/version.go reports '$source_version' but the release version is '$expected'." >&2
        failed=1
    fi

    local file reported
    while IFS= read -r file; do
        reported=$(sed -nE 's/^version="([^"]*)"$/\1/p' "$file")
        if [ "$reported" != "$expected" ]; then
            echo "Error: $file reports '$reported' but the release version is '$expected'." >&2
            failed=1
        fi
    done < <(git ls-files | grep -E '(^|/)addons/stagehand/plugin\.cfg$')

    while IFS= read -r file; do
        reported=$(sed -nE 's/^const VERSION: String = "([^"]*)"$/\1/p' "$file")
        if [ "$reported" != "$expected" ]; then
            echo "Error: $file reports '$reported' but the release version is '$expected'." >&2
            failed=1
        fi
    done < <(git ls-files | grep -E '(^|/)addons/stagehand/stagehand_version\.gd$')

    # Claude Code plugin mirrors (docs/design/claude-code-plugin.md).
    check_mirror() {
        local file="$1" want="$2" reported="$3"
        if [ "$reported" != "$want" ]; then
            echo "Error: $file reports '$reported' but the release needs '$want'." >&2
            failed=1
        fi
    }
    check_mirror integrations/claude-code/.claude-plugin/plugin.json "$expected" \
        "$(sed -nE 's/^  "version": "([^"]*)",?$/\1/p' integrations/claude-code/.claude-plugin/plugin.json)"
    check_mirror integrations/claude-code/bin/godot-stagehand "$expected" \
        "$(sed -nE 's/^VERSION="([^"]*)"$/\1/p' integrations/claude-code/bin/godot-stagehand)"
    check_mirror .claude-plugin/marketplace.json "v$expected" \
        "$(sed -nE 's/^ *"ref": "([^"]*)",?$/\1/p' .claude-plugin/marketplace.json)"

    if [ "$failed" -ne 0 ]; then
        echo "" >&2
        echo "Run './scripts/set-version.sh $expected', commit, and re-tag." >&2
        return 1
    fi
    echo "Version check passed: every reported version is $expected"
}

if [ "$VERSION" != "latest" ]; then
    verify_versions "$VERSION"
fi

if [ -n "$VERIFY_ONLY" ]; then
    exit 0
fi

mkdir -p "$BUILD_DIR"

echo "Building Stagehand v$VERSION..."

# Asset names built by this run, in build order. Only these go into SHA256SUMS,
# never whatever else happens to be lying around in $BUILD_DIR.
BUILT_ASSETS=()

build_target() {
    local goos="$1"
    local goarch="$2"
    local asset_name="$3"
    echo "Building $asset_name..."
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -o "$BUILD_DIR/$asset_name" .
    BUILT_ASSETS+=("$asset_name")
}

# SHA256SUMS is a published contract, not just a convenience: the Claude Code
# plugin launcher verifies its download against it. The format is exactly GNU
# coreutils `sha256sum` text output, "<hex>  <name>", one line per binary with
# a bare name (hence hashing from inside $BUILD_DIR). shasum -a 256 emits the
# identical format and keeps a local macOS build working.
write_checksums() {
    (
        cd "$BUILD_DIR"
        if command -v sha256sum >/dev/null 2>&1; then
            sha256sum "$@"
        else
            shasum -a 256 "$@"
        fi
    ) >"$BUILD_DIR/SHA256SUMS"
}

# Exact published asset matrix. Keep in sync with release.yml,
# docs/release-checklist.md, README.md, and editor/release_assets.gd.
#
# Deliberately NOT bundled: the agent skill
# (integrations/claude-code/skills/stagehand/SKILL.md) and docs/. These are
# prompt and documentation files a user reads from the source tree or gets
# through the Claude Code plugin, not runtime assets the binary needs (unlike
# addons/stagehand, which is go:embed'ed into the binary so `setup` can install
# it standalone). Bundling them here would mean maintaining a second,
# binary-release copy of files that already ship for free with the repo. See
# release_assets_contract_test.go.
build_target linux   amd64 godot-stagehand-linux-amd64
build_target darwin  amd64 godot-stagehand-darwin-amd64
build_target darwin  arm64 godot-stagehand-darwin-arm64
build_target windows amd64 godot-stagehand-windows-amd64.exe

write_checksums "${BUILT_ASSETS[@]}"
echo "Wrote $BUILD_DIR/SHA256SUMS"

# The linux binary runs on the release runner, so its --version output is the
# one artifact-level check we can actually execute here.
if [ "$VERSION" != "latest" ]; then
    reported=$("$BUILD_DIR/godot-stagehand-linux-amd64" --version | head -n 1)
    if [ "$reported" != "godot-stagehand $VERSION" ]; then
        echo "Error: built binary reports '$reported', expected 'godot-stagehand $VERSION'." >&2
        exit 1
    fi
    echo "Built binary reports: $reported"
fi

echo ""
echo "Build completed! Binaries are in the $BUILD_DIR directory:"
ls -la "$BUILD_DIR/"
