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
if [[ "${GODOT_CACHE_DIR}" == \~ || "${GODOT_CACHE_DIR}" == \~/* ]]; then
    GODOT_CACHE_DIR="${HOME}${GODOT_CACHE_DIR:1}"
fi

# Parse version components from e.g. "4.3.stable" or "4.3.1.stable"
# Godot release filenames use underscores: Godot_v4.3-stable_linux.x86_64
_ver="${GODOT_VERSION//.stable/}"   # strip trailing ".stable" suffix
_tag="${_ver}-stable"               # e.g. "4.3-stable"
_dotver="${_ver}"                   # e.g. "4.3" or "4.3.1"

GODOT_FILENAME="Godot_v${_dotver}-stable_linux.x86_64"
GODOT_ZIP="${GODOT_FILENAME}.zip"
GODOT_RELEASE_URL="https://github.com/godotengine/godot/releases/download/${_dotver}-stable"
GODOT_DOWNLOAD_URL="${GODOT_RELEASE_URL}/${GODOT_ZIP}"
GODOT_SUMS_URL="${GODOT_RELEASE_URL}/SHA512-SUMS.txt"

GODOT_BIN="${GODOT_CACHE_DIR}/${GODOT_FILENAME}"

# The CI cache key includes this script's hash, so a cached binary was
# verified by the current version of the check below.
if [[ -x "${GODOT_BIN}" ]]; then
    echo "Godot ${GODOT_VERSION} already cached at ${GODOT_BIN}"
else
    echo "Downloading Godot ${GODOT_VERSION} from ${GODOT_DOWNLOAD_URL} ..."
    mkdir -p "${GODOT_CACHE_DIR}"
    tmp_zip="${GODOT_CACHE_DIR}/${GODOT_ZIP}"
    tmp_sums="${GODOT_CACHE_DIR}/SHA512-SUMS.txt"
    if ! curl -fsSL --retry 3 --retry-delay 5 -o "${tmp_sums}" "${GODOT_SUMS_URL}" \
        || ! curl -fsSL --retry 3 --retry-delay 5 -o "${tmp_zip}" "${GODOT_DOWNLOAD_URL}"; then
        rm -f "${tmp_zip}" "${tmp_sums}"
        echo "Error: failed to download Godot ${GODOT_VERSION} or its SHA512-SUMS.txt" >&2
        exit 1
    fi
    expected_sha512=$(awk -v name="${GODOT_ZIP}" '$2 == name { print $1 }' "${tmp_sums}")
    actual_sha512=$(sha512sum "${tmp_zip}" | cut -d ' ' -f 1)
    rm -f "${tmp_sums}"
    if [[ -z "${expected_sha512}" || "${expected_sha512}" != "${actual_sha512}" ]]; then
        rm -f "${tmp_zip}"
        echo "Error: ${GODOT_ZIP} SHA-512 '${actual_sha512}' does not match SHA512-SUMS.txt entry '${expected_sha512:-<missing>}'" >&2
        exit 1
    fi
    echo "Verified ${GODOT_ZIP} against SHA512-SUMS.txt"
    unzip -q "${tmp_zip}" -d "${GODOT_CACHE_DIR}"
    rm -f "${tmp_zip}"
    chmod +x "${GODOT_BIN}"
    echo "Godot installed at ${GODOT_BIN}"
fi

# Export for callers that source this script
export GODOT_BIN
echo "GODOT_BIN=${GODOT_BIN}"
