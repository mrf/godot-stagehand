#!/usr/bin/env bash
# ci-install-godot.sh — Download and cache headless Godot for CI use.
#
# Usage:
#   source scripts/ci-install-godot.sh        # sets GODOT_BIN in current shell
#   GODOT_VERSION=4.3.stable ./scripts/ci-install-godot.sh
#
# Environment variables:
#   GODOT_VERSION   Godot release tag, e.g. "4.3.stable" (default: 4.3.stable)
#   GODOT_CACHE_DIR Directory to cache the binary  (default: ~/.cache/godot-ci)

set -euo pipefail

GODOT_VERSION="${GODOT_VERSION:-4.3.stable}"
GODOT_CACHE_DIR="${GODOT_CACHE_DIR:-${HOME}/.cache/godot-ci}"

# Parse version components from e.g. "4.3.stable" or "4.3.1.stable"
# Godot release filenames use underscores: Godot_v4.3-stable_linux.x86_64
_ver="${GODOT_VERSION//.stable/}"   # strip trailing ".stable" suffix
_tag="${_ver}-stable"               # e.g. "4.3-stable"
_dotver="${_ver}"                   # e.g. "4.3" or "4.3.1"

GODOT_FILENAME="Godot_v${_dotver}-stable_linux.x86_64"
GODOT_ZIP="${GODOT_FILENAME}.zip"
GODOT_DOWNLOAD_URL="https://github.com/godotengine/godot/releases/download/${_dotver}-stable/${GODOT_ZIP}"

GODOT_BIN="${GODOT_CACHE_DIR}/${GODOT_FILENAME}"

if [[ -x "${GODOT_BIN}" ]]; then
    echo "Godot ${GODOT_VERSION} already cached at ${GODOT_BIN}"
else
    echo "Downloading Godot ${GODOT_VERSION} from ${GODOT_DOWNLOAD_URL} ..."
    mkdir -p "${GODOT_CACHE_DIR}"
    tmp_zip="${GODOT_CACHE_DIR}/${GODOT_ZIP}"
    curl -fsSL --retry 3 --retry-delay 5 -o "${tmp_zip}" "${GODOT_DOWNLOAD_URL}"
    unzip -q "${tmp_zip}" -d "${GODOT_CACHE_DIR}"
    rm -f "${tmp_zip}"
    chmod +x "${GODOT_BIN}"
    echo "Godot installed at ${GODOT_BIN}"
fi

# Export for callers that source this script
export GODOT_BIN
echo "GODOT_BIN=${GODOT_BIN}"
