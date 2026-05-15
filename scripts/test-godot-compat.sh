#!/usr/bin/env bash
# test-godot-compat.sh — Test Stagehand addon parse compatibility across Godot versions.
#
# Downloads each Godot version (using ci-install-godot.sh) and runs a headless
# parse check against the test project. Reports a pass/fail matrix.
#
# Usage:
#   ./scripts/test-godot-compat.sh              # test default versions (4.3, 4.4)
#   ./scripts/test-godot-compat.sh 4.2 4.3 4.4  # test specific versions
#
# Environment:
#   GODOT_CACHE_DIR  Cache directory for Godot binaries (default: ~/.cache/godot-ci)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_PROJECT="${PROJECT_DIR}/testdata/test_project"

DEFAULT_VERSIONS=("4.3" "4.4")
if [[ $# -gt 0 ]]; then
    VERSIONS=("$@")
else
    VERSIONS=("${DEFAULT_VERSIONS[@]}")
fi

export GODOT_CACHE_DIR="${GODOT_CACHE_DIR:-${HOME}/.cache/godot-ci}"

declare -a RESULTS=()
PASS=0
FAIL=0
SKIP=0

for ver in "${VERSIONS[@]}"; do
    printf "\n=== Godot %s ===\n" "$ver"

    export GODOT_VERSION="${ver}.stable"

    # Download / locate cached binary (capture output from a single invocation)
    install_output="$(bash "${SCRIPT_DIR}/ci-install-godot.sh" 2>&1)" || {
        printf "  SKIP: download failed for Godot %s\n" "$ver"
        RESULTS+=("${ver}|SKIP|download failed")
        ((SKIP++)) || true
        continue
    }

    GODOT_BIN="$(grep '^GODOT_BIN=' <<< "$install_output" | head -1 | cut -d= -f2)"

    if [[ ! -x "${GODOT_BIN:-}" ]]; then
        printf "  SKIP: binary not found for Godot %s\n" "$ver"
        RESULTS+=("${ver}|SKIP|binary not found")
        ((SKIP++)) || true
        continue
    fi

    # Run headless parse check — Godot loads all GDScript files on project open
    log_file="$(mktemp)"
    "${GODOT_BIN}" --headless --path "${TEST_PROJECT}" --quit 2>&1 \
        | tee "$log_file" || true

    if grep -qE "ERROR|SCRIPT ERROR|Parse Error" "$log_file"; then
        printf "  FAIL: parse errors detected\n"
        grep -E "ERROR|SCRIPT ERROR|Parse Error" "$log_file" | head -20
        RESULTS+=("${ver}|FAIL|parse errors")
        ((FAIL++)) || true
    else
        printf "  PASS\n"
        RESULTS+=("${ver}|PASS|ok")
        ((PASS++)) || true
    fi

    rm -f "$log_file"
done

# Summary
printf "\n--- Compatibility Matrix ---\n"
printf "%-10s %-8s %s\n" "VERSION" "RESULT" "NOTES"
printf "%-10s %-8s %s\n" "-------" "------" "-----"
for entry in "${RESULTS[@]}"; do
    IFS='|' read -r v r n <<< "$entry"
    printf "%-10s %-8s %s\n" "$v" "$r" "$n"
done
printf "\nTotal: %d pass, %d fail, %d skip\n" "$PASS" "$FAIL" "$SKIP"

if [[ $FAIL -gt 0 ]]; then
    exit 1
fi
