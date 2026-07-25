# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the NON-embedded (real OS-level) [Window] case — a project running
## with [code]display/window/subwindows/embed_subwindows = false[/code], where a
## popped dialog becomes its own operating-system window instead of being drawn
## inside the main one (godot-stagehand-inpw).
##
## The embedded case is covered by test_input_simulator_subwindow.gd. That suite
## exercises [method StagehandInputSimulator._resolve_click_target]'s
## walk-out-of-the-embedder loop; this one covers the branch that loop breaks out
## of immediately, which nothing exercised before.
##
## [b]Why the windows here are never shown.[/b] Popping a non-embedded [Window]
## permanently breaks [method Input.parse_input_event] delivery for the rest of
## the process. Traced against Godot 4.6.2 --headless, one suite, a recorder node
## counting [InputEventKey] in [method Node._input] after each step:
##
## [codeblock]
## [P] baseline                                        keys=1
## [P] os window up                                    keys=0
## [P] after hide                                      keys=0
## [P] after free + gui_embed_subwindows restore       keys=0
## [P] after window_move_to_foreground(MAIN_WINDOW_ID) keys=0
## [P] after root.grab_focus()                         keys=0
## [/codeblock]
##
## Irrecoverable, so a visible OS window in this shared runner takes down every
## later key/action/text suite with it (confirmed: 6 failures + 5 errors in
## test_input_simulator.gd). [method Window.is_embedded] is false as soon as the
## embedder is gone, visible or not, so the branch under test is still genuinely
## exercised — and the refusals below short-circuit before anything is pushed,
## which is the code path that matters.
##
## The visible-OS-window behaviour is checked instead in its own throwaway
## process, headless and on a real display, by os_subwindow_probe.gd and
## TestOsSubwindowInputIsUnreachable (os_subwindow_gate_test.go).

const INNER_GROUP: StringName = &"os_subwindow_inner_button"
const OUTER_GROUP: StringName = &"os_subwindow_outer_button"

## Matches the size StagehandInputSimulator normalises the headless root window
## to, so the main-window regression check below is not fighting a resize.
const ROOT_SIZE: Vector2i = Vector2i(1152, 648)

## Deliberately not the project resolution: the size test below asserts the
## dialog is left exactly as the application set it, which only means something
## if the value differs from what _ensure_headless_window_sized would impose.
const DIALOG_SIZE: Vector2i = Vector2i(360, 220)

var _saved_size: Vector2i
var _saved_embed: bool
var _dialog: AcceptDialog
var _inner: Button
var _outer: Button
var _outer_presses: int = 0


func before_test() -> void:
	var root: Window = get_tree().root
	_saved_size = root.size
	_saved_embed = root.gui_embed_subwindows
	root.size = ROOT_SIZE
	# The whole point of the suite: no embedder, so a child Window is its own
	# OS window and Window.is_embedded() is false.
	root.gui_embed_subwindows = false

	_outer_presses = 0

	_outer = auto_free(Button.new())
	_outer.name = "OutsideOsDialogButton"
	_outer.position = Vector2(10.0, 10.0)
	_outer.size = Vector2(120.0, 40.0)
	_outer.add_to_group(OUTER_GROUP)
	_outer.pressed.connect(func() -> void: _outer_presses += 1)
	add_child(_outer)

	_dialog = AcceptDialog.new()
	_dialog.name = "OsSubwindowDialog"
	_dialog.size = DIALOG_SIZE
	_inner = Button.new()
	_inner.name = "InsideOsDialogButton"
	_inner.text = "New Project"
	_inner.custom_minimum_size = Vector2(290.0, 27.0)
	_inner.add_to_group(INNER_GROUP)
	_dialog.add_child(_inner)
	add_child(_dialog)
	await get_tree().process_frame


## Leaking embed=false into the rest of the run would change every later suite's
## windowing mode, and the engine refuses the change while a child window is
## still parented — so the dialog goes first, unconditionally.
func after_test() -> void:
	var root: Window = get_tree().root
	remove_child(_dialog)
	_dialog.free()
	_dialog = null
	_inner = null
	await get_tree().process_frame

	root.gui_embed_subwindows = _saved_embed
	root.size = _saved_size


## Guards the premise of every test below. If a future engine embeds anyway,
## these tests would be asserting about the embedded path under an OS-window
## name — so fail loudly instead of passing vacuously.
func test_the_fixture_really_produces_a_non_embedded_window() -> void:
	assert_bool(_dialog.is_embedded()).is_false()
	assert_array(get_tree().root.get_embedded_subwindows()).is_empty()
	assert_object(_inner.get_viewport()).is_same(_dialog)


# ── _resolve_click_target: the branch the embedded walk breaks out of ────

## For a non-embedded subwindow the push target is that [Window] itself and the
## point stays in its own canvas space — no embedder translation, because there
## is no embedder.
func test_resolve_click_target_targets_the_os_window_itself() -> void:
	var target: StagehandInputSimulator.ClickTarget = (
		StagehandInputSimulator._resolve_click_target(_inner)
	)
	assert_object(target.viewport).is_same(_dialog)
	assert_object(target.hit_viewport).is_same(_dialog)

	var in_dialog: Vector2 = _inner.get_global_transform_with_canvas() * (_inner.size / 2.0)
	assert_float(target.position.x).is_equal_approx(in_dialog.x, 0.001)
	assert_float(target.position.y).is_equal_approx(in_dialog.y, 0.001)


## The embedded path adds the subwindow's on-screen offset. The non-embedded one
## must not: an OS window's position is a screen coordinate, meaningless inside
## its own viewport.
func test_resolve_click_target_does_not_add_the_os_window_offset() -> void:
	_dialog.position = Vector2i(640, 400)
	var target: StagehandInputSimulator.ClickTarget = (
		StagehandInputSimulator._resolve_click_target(_inner)
	)
	var embedder_style: Vector2 = (
		_dialog.get_final_transform()
		* (_inner.get_global_transform_with_canvas() * (_inner.size / 2.0))
		+ Vector2(_dialog.position)
	)
	assert_vector(target.position).is_not_equal(embedder_style)


# ── the refusal ──────────────────────────────────────────────────────────

## Before this issue the caller got "is not the topmost Control … something is
## covering the target", which sent them hunting for a non-existent overlay. The
## refusal has to name the real cause and the real recovery.
func test_click_inside_an_os_subwindow_returns_a_typed_refusal() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % INNER_GROUP, "hold_ms": 0}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")
	assert_str(str(result.get("error", ""))).contains(str(_dialog.get_path()))

	var details: Dictionary = result.get("details", {})
	assert_str(str(details.get("window", ""))).is_equal(str(_dialog.get_path()))
	assert_str(str(details.get("next_action", ""))).contains("embed_subwindows")


## input_mouse_move has no delivery confirmation of its own, so without the
## guard it reported success for a hover that never happened — a false positive
## worse than the click's misleading error.
func test_mouse_move_onto_a_control_in_an_os_subwindow_returns_a_typed_refusal() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse_move(
		get_tree(), {"selector": "group:%s" % INNER_GROUP}
	)
	assert_bool(result.get("success", false)).is_false()
	assert_str(str(result.get("error_code", ""))).is_equal("not_supported")
	var details: Dictionary = result.get("details", {})
	assert_str(str(details.get("next_action", ""))).contains("embed_subwindows")


## The refusal is about the window, not about the click landing badly, so it has
## to be reported before the delivery check gets a chance to blame an overlay
## that does not exist.
func test_the_refusal_does_not_blame_a_covering_control() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % INNER_GROUP, "hold_ms": 0}
	)
	assert_str(str(result.get("error", ""))).not_contains("topmost Control")


# ── the headless window-size correction must not touch a subwindow ───────

## _ensure_headless_window_sized exists to repair the root Window's degenerate
## headless size stub. Applied to a non-embedded subwindow it rewrites the
## application's own dialog geometry — observed under --headless against Godot
## 4.6.2 before this fix: a 306x88 AcceptDialog came back 1152x648 after a
## single click attempt.
func test_the_headless_size_correction_leaves_an_os_subwindow_alone() -> void:
	var size_before: Vector2i = _dialog.size
	StagehandInputSimulator._ensure_headless_window_sized(_dialog)
	assert_vector(Vector2(_dialog.size)).is_equal(Vector2(size_before))


## The correction still has to do its job on the window it was written for,
## otherwise the narrowing above would silently reintroduce godot-stagehand-nry.
func test_the_headless_size_correction_still_fixes_the_root_window() -> void:
	var root: Window = get_tree().root
	if DisplayServer.get_name() != "headless":
		# Deliberately a no-op on a real display server, where the window size is
		# legitimate: assert that instead of asserting nothing.
		root.size = Vector2i(64, 64)
		StagehandInputSimulator._ensure_headless_window_sized(root)
		assert_vector(Vector2(root.size)).is_equal(Vector2(64.0, 64.0))
		return
	root.size = Vector2i(64, 64)
	StagehandInputSimulator._ensure_headless_window_sized(root)
	assert_vector(Vector2(root.size)).is_equal(Vector2(ROOT_SIZE))


# ── regression: the main window is unaffected by embedding being off ─────

## Turning embedding off changes nothing about the root window, which is never
## embedded in the first place. The ordinary click path must still work.
func test_main_window_clicks_still_work_with_embedding_disabled() -> void:
	var result: Dictionary = StagehandInputSimulator.input_mouse(
		get_tree(), {"selector": "group:%s" % OUTER_GROUP, "hold_ms": 0}
	)
	assert_str(str(result.get("error", ""))).is_empty()
	assert_bool(result.get("success", false)).is_true()
	await get_tree().process_frame
	await get_tree().process_frame
	assert_int(_outer_presses).is_equal(1)
