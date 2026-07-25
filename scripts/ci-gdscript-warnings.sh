#!/usr/bin/env bash
# ci-gdscript-warnings.sh — GDScript strict-warning gate for the addon.
#
# CLAUDE.md requires every addon .gd file to be strict-mode compliant. Godot
# never surfaces GDScript warnings on stdout in the editor UI sense, but with
# every `debug/gdscript/warnings/*` project setting elevated to 2 (error) a
# plain headless project load DOES emit them as "Parse Error: ... (Warning
# treated as error.)". testdata/test_project sets exactly that, so loading it
# headless is a real warnings-as-errors gate over the checked-in addon copy.
#
# Two things make this fragile enough to need a self-test:
#   1. Godot excludes res://addons from warnings by default, via a setting
#      that was renamed mid-4.x (exclude_addons -> directory_rules).
#   2. `godot --quit` exits 0 even when scripts fail to compile, so detection
#      is grep-based and a regex/format change would silently pass.
# Either regression turns this gate into a no-op that reports green forever.
# `selftest` is the negative control that catches that: it injects a known
# violation into the addon and fails if Godot does NOT complain.
#
# Usage:
#   GODOT_BIN=/path/to/godot ./scripts/ci-gdscript-warnings.sh check
#   GODOT_BIN=/path/to/godot ./scripts/ci-gdscript-warnings.sh selftest
#
# Environment variables:
#   GODOT_BIN  Path to a Godot binary (required).

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
PROJECT="$REPO_ROOT/testdata/test_project"

# Bare ERROR is intentionally excluded: headless Godot emits engine-level ERROR
# lines (Vulkan, display driver, etc.) that are harmless on CI runners.
DIAGNOSTIC_RE="SCRIPT ERROR|Parse Error"

# The violation `selftest` injects. Two warnings in three lines: an untyped
# return type and an untyped variable, both `untyped_declaration`, which every
# supported Godot version reports identically.
readonly PROBE_FILE="addons/stagehand/stagehand_version.gd"
readonly PROBE_SOURCE='

static func _stagehand_warning_gate_probe():
	var probe_value = 1
	return probe_value
'

if [[ -z "${GODOT_BIN:-}" ]]; then
    echo "ERROR: GODOT_BIN is not set" >&2
    exit 1
fi
if [[ ! -x "${GODOT_BIN}" ]]; then
    echo "ERROR: GODOT_BIN is not executable: ${GODOT_BIN}" >&2
    exit 1
fi

# diagnostics <project-dir> — print every warning/error line from a headless
# project load. Always succeeds; callers decide what an empty result means.
diagnostics() {
    local project="$1" log
    log="$(mktemp)"
    # `|| true`: --quit reports success even on compile failure, and the log is
    # the only signal. Never let the exit code decide.
    "${GODOT_BIN}" --headless --path "${project}" --quit >"${log}" 2>&1 || true
    # -A1 pulls in the "at: GDScript::reload (res://...gd:NN)" line that Godot
    # prints under each diagnostic; the path lives there, not on the message
    # line. It cannot produce false positives on its own — context is only
    # emitted for an actual match.
    grep -E -A1 "${DIAGNOSTIC_RE}" "${log}" || true
    rm -f "${log}"
}

# scratch_project — copy testdata/test_project somewhere writable, minus the
# .godot import cache, and echo the path. Godot rewrites project.godot on load,
# so the gate must never run against the working tree copy.
scratch_project() {
    local dir
    dir="$(mktemp -d)/test_project"
    cp -R "${PROJECT}" "${dir}"
    rm -rf "${dir}/.godot"
    echo "${dir}"
}

cmd_check() {
    local project found
    project="$(scratch_project)"
    found="$(diagnostics "${project}")"
    if [[ -n "${found}" ]]; then
        echo "GDScript warnings-as-errors violations detected in the addon:"
        echo "${found}"
        exit 1
    fi
    echo "OK: addon loads clean with every gdscript/warnings/* elevated to error."
}

cmd_selftest() {
    local project found
    project="$(scratch_project)"
    printf '%s' "${PROBE_SOURCE}" >>"${project}/${PROBE_FILE}"
    found="$(diagnostics "${project}")"
    if [[ -z "${found}" ]]; then
        echo "ERROR: the strict-warning gate is DISARMED." >&2
        echo "A deliberate untyped_declaration violation was appended to" >&2
        echo "${PROBE_FILE} and Godot reported nothing. Warnings for" >&2
        echo "res://addons are being excluded, or the diagnostic format" >&2
        echo "changed and /${DIAGNOSTIC_RE}/ no longer matches." >&2
        echo "See the gdscript/warnings comments in testdata/test_project/project.godot." >&2
        exit 1
    fi
    if ! grep -q "${PROBE_FILE}" <<<"${found}"; then
        echo "ERROR: the gate reported diagnostics, but none for ${PROBE_FILE}:" >&2
        echo "${found}" >&2
        exit 1
    fi
    echo "OK: gate is armed — the injected addon violation was caught:"
    grep "${PROBE_FILE}" <<<"${found}" || true
}

case "${1:-}" in
    check) cmd_check ;;
    selftest) cmd_selftest ;;
    *)
        echo "Usage: GODOT_BIN=<godot> $0 {check|selftest}" >&2
        exit 2
        ;;
esac
