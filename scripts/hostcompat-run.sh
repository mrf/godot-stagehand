#!/usr/bin/env bash
# hostcompat-run.sh — run one pinned host project through every stage of the
# host-project compatibility suite, and say which stage failed.
#
#   checkout → setup → import → scenario (launch/connect, then the steps)
#
# Stages (docs/design/host-project-compat-suite.md, Amendment 4):
#   checkout  scripts/hostcompat-checkout.sh: clone at the pinned SHA.
#   setup     `godot-stagehand setup --force <project>`, then verify the addon
#             and the autoload are actually in the project. The install path is
#             under test, not plumbing.
#   import    `godot --headless --import` on the installed project. Needs no
#             window and no xvfb. Fails on a non-zero exit, a timeout, or any
#             parse/script diagnostic that points into res://addons/stagehand/
#             (Godot exits 0 on a script that failed to compile, so the log is
#             the only signal). Diagnostics in the host's own code are printed
#             as notes but do not fail the stage: the suite does not judge the
#             host.
#   scenario  `godot-stagehand run` with the manifest's scenario against the
#             checkout. Needs a display (wrap the whole script in `xvfb-run -a`
#             on CI). The scenario report splits it in two: a failure in the
#             connect phase is stage "connect" (launch/connect), any other is
#             stage "scenario".
#
# Only checkout is infrastructure noise. Every later stage failing is our bug,
# and the output and exit code say which.
#
# Exit codes (a stable contract — CI branches on them):
#   0   every requested stage passed
#   2   usage or manifest error (passed through from the checkout script too)
#   3   pin unresolved, passed through from the checkout script
#   75  checkout failed — infrastructure; retry, never a compat failure
#   10  setup failed
#   11  import failed
#   12  launch/connect failed
#   13  scenario failed
#
# Usage: scripts/hostcompat-run.sh [--through checkout|setup|import|scenario] <id>
#   --through STAGE  stop after STAGE passes (default: scenario). `--through
#                    import` is the unattended no-window half.
#
# Environment:
#   STAGEHAND_BIN            godot-stagehand binary (default: <repo>/godot-stagehand)
#   GODOT_BIN                Godot binary; required for import, passed to `run`
#   HOSTCOMPAT_OUT_DIR       artifacts (logs, reports) (default: <dest>/out/<id>)
#   HOSTCOMPAT_IMPORT_TIMEOUT  seconds before the import is killed (default 600)
#   HOSTCOMPAT_MANIFEST, HOSTCOMPAT_DEST_DIR  as for hostcompat-checkout.sh

set -euo pipefail

readonly EXIT_USAGE=2
readonly EXIT_INFRA=75
readonly EXIT_SETUP=10
readonly EXIT_IMPORT=11
readonly EXIT_CONNECT=12
readonly EXIT_SCENARIO=13

readonly STAGES=(checkout setup import scenario)

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
manifest="${HOSTCOMPAT_MANIFEST:-$repo_root/testdata/hostcompat/manifest.json}"
dest_root="${HOSTCOMPAT_DEST_DIR:-$repo_root/.hostcompat}"
stagehand_bin="${STAGEHAND_BIN:-$repo_root/godot-stagehand}"
import_timeout="${HOSTCOMPAT_IMPORT_TIMEOUT:-600}"

say() { echo "hostcompat: $*" >&2; }

usage_die() {
    say "$*"
    exit "$EXIT_USAGE"
}

through=scenario
while [ $# -gt 0 ]; do
    case "$1" in
    --through)
        [ $# -ge 2 ] || usage_die "--through needs a stage"
        through="$2"
        shift 2
        ;;
    --) shift; break ;;
    -*) usage_die "unknown flag $1 (usage: $0 [--through STAGE] <id>)" ;;
    *) break ;;
    esac
done
[ $# -eq 1 ] || usage_die "usage: $0 [--through checkout|setup|import|scenario] <id>"
id="$1"

stage_wanted() {
    # True when $1 is at or before the --through stage.
    local s want_idx=-1 through_idx=-1 i=0
    for s in "${STAGES[@]}"; do
        [ "$s" = "$1" ] && want_idx=$i
        [ "$s" = "$through" ] && through_idx=$i
        i=$((i + 1))
    done
    [ "$through_idx" -ge 0 ] || usage_die "unknown stage '$through' (stages: ${STAGES[*]})"
    [ "$want_idx" -le "$through_idx" ]
}
stage_wanted checkout || true # validates --through before any work

command -v jq >/dev/null 2>&1 || usage_die "jq is required"
command -v timeout >/dev/null 2>&1 || usage_die "timeout (coreutils) is required"

# fail <stage> <exit code> <class> <message...>: name the stage, classify, exit.
fail() {
    local stage="$1" code="$2" class="$3"
    shift 3
    say "$id: FAILED at stage $stage — $class: $*"
    exit "$code"
}
pass() { say "stage $1: PASS"; }

# ---- checkout --------------------------------------------------------------
checkout_out=""
checkout_rc=0
checkout_out="$("$repo_root/scripts/hostcompat-checkout.sh" "$id")" || checkout_rc=$?
case "$checkout_rc" in
0) ;;
"$EXIT_USAGE") exit "$EXIT_USAGE" ;; # the checkout script already said why
3) exit 3 ;;                         # unpinned: it printed the SHA to paste
*) fail checkout "$EXIT_INFRA" "infrastructure" "retry; this is never a compat failure (checkout exit $checkout_rc)" ;;
esac
checkout="$checkout_out"
pass checkout
stage_wanted setup || exit 0

subdir="$(jq -r --arg id "$id" '.projects[] | select(.id == $id) | .project_subdir // empty' "$manifest")"
project="$checkout${subdir:+/$subdir}"
out_dir="${HOSTCOMPAT_OUT_DIR:-$dest_root/out/$id}"
mkdir -p "$out_dir"

# ---- setup -----------------------------------------------------------------
[ -x "$stagehand_bin" ] || usage_die "STAGEHAND_BIN '$stagehand_bin' is not an executable (build it: go build -o godot-stagehand .)"
setup_log="$out_dir/setup.log"
if ! "$stagehand_bin" setup --force "$project" >"$setup_log" 2>&1; then
    fail setup "$EXIT_SETUP" "our bug" "godot-stagehand setup exited non-zero: $(tail -n 3 "$setup_log" | tr '\n' ' ')(full log: $setup_log)"
fi
# `setup` reporting success is not the claim under test; the project being
# installed is.
if [ ! -f "$project/addons/stagehand/plugin.cfg" ]; then
    fail setup "$EXIT_SETUP" "our bug" "setup exited 0 but addons/stagehand/plugin.cfg is not in $project"
fi
if ! grep -q '^StagehandServer=' "$project/project.godot"; then
    fail setup "$EXIT_SETUP" "our bug" "setup exited 0 but project.godot has no StagehandServer autoload"
fi
pass setup
stage_wanted import || exit 0

# ---- import ----------------------------------------------------------------
[ -n "${GODOT_BIN:-}" ] || usage_die "GODOT_BIN is required for the import stage"
[ -x "$GODOT_BIN" ] || usage_die "GODOT_BIN '$GODOT_BIN' is not executable"
import_log="$out_dir/import.log"
import_rc=0
timeout "$import_timeout" "$GODOT_BIN" --headless --path "$project" --import >"$import_log" 2>&1 || import_rc=$?
# -A1: the res:// path sits on the "at:" line under the diagnostic.
diagnostics="$(grep -E -A1 'SCRIPT ERROR|Parse Error' "$import_log" || true)"
ours="$(printf '%s\n' "$diagnostics" | grep -F 'res://addons/stagehand/' || true)"
if [ "$import_rc" -eq 124 ]; then
    fail import "$EXIT_IMPORT" "our bug" "godot --import did not finish in ${import_timeout}s (log: $import_log)"
elif [ "$import_rc" -ne 0 ]; then
    fail import "$EXIT_IMPORT" "our bug" "godot --import exited $import_rc (log: $import_log)"
elif [ -n "$ours" ]; then
    fail import "$EXIT_IMPORT" "our bug" "the addon does not compile in this host: $(printf '%s' "$ours" | tr '\n' ' ')(log: $import_log)"
fi
if [ -n "$diagnostics" ]; then
    say "note: import logged diagnostics outside res://addons/stagehand/ (host code, not judged):"
    printf '%s\n' "$diagnostics" | sed 's/^/hostcompat:   /' >&2
fi
pass import
stage_wanted scenario || exit 0

# ---- scenario (launch/connect, then steps) ---------------------------------
scenario_rel="$(jq -r --arg id "$id" '.projects[] | select(.id == $id) | .scenario // empty' "$manifest")"
[ -n "$scenario_rel" ] || usage_die "project '$id' has no scenario in the manifest"
scenario="$(dirname "$manifest")/$scenario_rel"
[ -f "$scenario" ] || usage_die "scenario file not found: $scenario"

report="$out_dir/report.stdout.json"
run_args=(run "$scenario" --project-path "$project" --out-dir "$out_dir")
[ -z "${GODOT_BIN:-}" ] || run_args+=(--godot-bin "$GODOT_BIN")
run_rc=0
"$stagehand_bin" "${run_args[@]}" >"$report" || run_rc=$?
if [ "$run_rc" -ne 0 ]; then
    phase="$(jq -r '.failure.phase // empty' "$report" 2>/dev/null || true)"
    detail="$(jq -r '.failure | "\(.phase) phase, \(.kind): \(.message)"' "$report" 2>/dev/null || true)"
    : "${detail:=no readable report (run exit $run_rc)}"
    if [ "$phase" = "connect" ]; then
        fail connect "$EXIT_CONNECT" "our bug" "$detail (report: $report)"
    fi
    fail scenario "$EXIT_SCENARIO" "our bug" "$detail (report: $report)"
fi
pass connect
pass scenario
say "$id: all stages passed"
