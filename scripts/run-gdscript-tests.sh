#!/usr/bin/env bash
# run-gdscript-tests.sh — Run the GDScript unit suite headlessly via GdUnit4.
#
# Executes every test suite under testdata/test_project/test against the
# checked-in addon copy in that project, which addon_copy_drift_test.go keeps
# byte-for-byte identical to canonical addons/stagehand. The project elevates
# every gdscript/warnings/* to an error, so a suite that trips a strict-mode
# warning fails here as a parse error rather than silently passing.
#
# Usage:
#   GODOT_BIN=/path/to/godot ./scripts/run-gdscript-tests.sh [extra gdunit args]
#
# Environment:
#   GODOT_BIN        Godot binary to run (required).
#   GDUNIT_REPORTS   Report output directory, relative to the test project
#                    (default: reports).
#
# Exit codes:
#   0   every test passed
#   1   GODOT_BIN missing/not executable, or GdUnit4 is not installed
#   *   GdUnit4's own exit code (100 = test failures, 103/105 = startup/parse)
#
# On completion the path to the JUnit XML report is printed as:
#   GDUNIT_REPORT=<absolute path to results.xml>
# which is what TestGdUnitSuite (gdscript_suite_test.go) parses.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TEST_PROJECT="$REPO_ROOT/testdata/test_project"
GDUNIT_REPORTS="${GDUNIT_REPORTS:-reports}"

if [[ -z "${GODOT_BIN:-}" ]]; then
    echo "ERROR: GODOT_BIN is not set. Point it at a Godot 4.3+ binary." >&2
    exit 1
fi
if [[ ! -x "$GODOT_BIN" ]]; then
    echo "ERROR: GODOT_BIN ($GODOT_BIN) is not an executable file." >&2
    exit 1
fi
if [[ ! -d "$TEST_PROJECT/addons/gdUnit4" ]]; then
    echo "ERROR: GdUnit4 is not installed at $TEST_PROJECT/addons/gdUnit4." >&2
    exit 1
fi

cd "$TEST_PROJECT"

# GdUnitCmdTool resolves GdUnit4's classes through the global class cache,
# which only exists once the project has been imported. A fresh checkout (or a
# cleaned .godot) therefore needs one import pass before the runner will load.
if [[ ! -d ".godot" ]]; then
    echo "Importing project (first run, building the GDScript class cache) ..."
    "$GODOT_BIN" --headless --path . --import >/dev/null 2>&1 || true
fi

# --ignoreHeadlessMode: GdUnit4 refuses headless runs by default because
#   Input.parse_input_event is not delivered without a display server. These
#   suites synthesize input through Viewport.push_input instead, and the few
#   that do use Input.parse_input_event await a frame, so headless is fine.
# -c: do not fail fast — report every failing test in one run rather than
#   aborting the suite at the first one.
# --remote-debug tcp://127.0.0.1:0: port 0 is never bound, so the connection is
#   refused and Godot skips its interactive `debug>` prompt, which would
#   otherwise hang a CI run on a parse error.
set +e
"$GODOT_BIN" --headless --path . -s -d \
    --remote-debug tcp://127.0.0.1:0 \
    res://addons/gdUnit4/bin/GdUnitCmdTool.gd \
    --ignoreHeadlessMode \
    -c \
    -rd "$GDUNIT_REPORTS" \
    -a res://test/ \
    "$@"
exit_code=$?
set -e

# GdUnit4 writes into <reports>/report_<n>; surface the newest for the caller.
latest_report=""
if [[ -d "$GDUNIT_REPORTS" ]]; then
    latest_report="$(find "$GDUNIT_REPORTS" -name results.xml -print0 2>/dev/null \
        | xargs -0 -r ls -t 2>/dev/null | head -n 1)"
fi
if [[ -n "$latest_report" ]]; then
    echo "GDUNIT_REPORT=$(cd "$(dirname "$latest_report")" && pwd)/results.xml"
else
    echo "GDUNIT_REPORT="
fi

exit "$exit_code"
