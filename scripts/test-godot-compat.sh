#!/usr/bin/env bash
# test-godot-compat.sh — Test Stagehand addon compatibility across Godot versions.
#
# Downloads each Godot version (using ci-install-godot.sh) and runs the full
# compatibility protocol:
#   1. install addon (via testdata/test_project, already checked in)
#   2. headless open — no GDScript parse errors
#   3. headless run with STAGEHAND_ENABLED=1 — server starts
#   4. connect from the Go binary — authenticated ping succeeds
#   5. exercise core tools — get_tree, find_nodes, click, screenshot
#
# Steps 3-5 run as `go test` against the real Godot binary for that version,
# reusing the same integration/smoke suites CI runs (see .github/workflows/ci.yml).
# A version that fails step 2 is reported FAIL and steps 3-5 are skipped for it.
#
# Usage:
#   ./scripts/test-godot-compat.sh                      # test default versions
#   ./scripts/test-godot-compat.sh 4.3 4.4 4.5          # test specific versions
#
# Environment:
#   GODOT_CACHE_DIR  Cache directory for Godot binaries (default: ~/.cache/godot-ci)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TEST_PROJECT="${PROJECT_DIR}/testdata/test_project"

# 4.2 is included by default despite being below the documented minimum
# supported version (4.3, see README.md's Godot version compatibility
# section) so a regression *or* an unexpected fix shows up in the matrix
# instead of going unnoticed.
DEFAULT_VERSIONS=("4.2" "4.3" "4.4" "4.5" "4.6.2" "4.7.1")
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

    # Step 2 — headless open; Godot parses every GDScript file on project load.
    # Bare "ERROR" is intentionally excluded: headless Godot emits engine-level
    # ERROR lines (Vulkan, display driver, etc.) that are harmless on runners
    # with no GPU — see the matching check in .github/workflows/ci.yml.
    log_file="$(mktemp)"
    "${GODOT_BIN}" --headless --path "${TEST_PROJECT}" --quit >"$log_file" 2>&1 || true

    if grep -qE "SCRIPT ERROR|Parse Error" "$log_file"; then
        printf "  FAIL: parse errors detected\n"
        grep -E "SCRIPT ERROR|Parse Error" "$log_file" | head -20
        RESULTS+=("${ver}|FAIL|parse errors")
        ((FAIL++)) || true
        rm -f "$log_file"
        continue
    fi
    rm -f "$log_file"
    printf "  PASS: no parse errors\n"

    # Steps 3-5 — activation guard, authenticated ping, and core tool coverage
    # (get_tree, find_nodes, click, screenshot), run against the real binary.
    if ! GODOT_BIN="${GODOT_BIN}" go test -C "${PROJECT_DIR}" -tags=integration \
        ./internal/launch -run '^TestAddonInstallation$' -count=1 -timeout=2m; then
        printf "  FAIL: STAGEHAND_ENABLED activation / authenticated ping\n"
        RESULTS+=("${ver}|FAIL|activation or ping failed")
        ((FAIL++)) || true
        continue
    fi

    if ! GODOT_BIN="${GODOT_BIN}" go test -C "${PROJECT_DIR}" -tags=godot \
        ./internal/godotconn -run '^Test(Smoke|IntegrationGodot)' -count=1 -timeout=3m; then
        printf "  FAIL: core tool smoke coverage (get_tree/find_nodes/click/screenshot)\n"
        RESULTS+=("${ver}|FAIL|smoke coverage failed")
        ((FAIL++)) || true
        continue
    fi

    printf "  PASS: full protocol\n"
    RESULTS+=("${ver}|PASS|ok")
    ((PASS++)) || true
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
