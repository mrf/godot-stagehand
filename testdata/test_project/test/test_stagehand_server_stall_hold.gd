# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the quit-on-disconnect stall hold.
##
## A main-loop block outlives the client's pong deadline and its reconnect
## window, so the server wakes with nobody attached through no fault of the
## client. The hold keeps a session worth saving alive past the ordinary grace.

const StagehandServer := preload("res://addons/stagehand/autoload/stagehand_server.gd")
const WOKE_AT_MSEC: int = 1000000

var _server: StagehandServer


func before_test() -> void:
	_server = StagehandServer.new()
	add_child(_server)
	auto_free(_server)
	_server._quit_on_disconnect = true
	_server._had_authenticated_client = true


func _block_for(block_ms: int) -> void:
	_server._last_process_msec = WOKE_AT_MSEC - block_ms
	_server._note_main_loop_gap(WOKE_AT_MSEC)


func test_a_block_past_the_threshold_holds_the_quit() -> void:
	_block_for(StagehandServer.STALL_THRESHOLD_MS)

	assert_int(_server._quit_not_before_msec).is_equal(
		WOKE_AT_MSEC + StagehandServer.STALL_RECOVERY_GRACE_MS
	)


func test_an_ordinary_frame_sets_no_hold() -> void:
	_block_for(StagehandServer.STALL_THRESHOLD_MS - 1)

	assert_int(_server._quit_not_before_msec).is_equal(-1)


func test_every_tick_advances_the_gap_clock() -> void:
	_block_for(1)

	assert_int(_server._last_process_msec).is_equal(WOKE_AT_MSEC)


func test_a_session_nobody_authenticated_is_not_held() -> void:
	# A launcher that timed out during boot took the only copy of the token with
	# it: no client can ever attach, so the game must not outlive it.
	_server._had_authenticated_client = false

	_block_for(StagehandServer.STALL_THRESHOLD_MS)

	assert_int(_server._quit_not_before_msec).is_equal(-1)


func test_a_held_quit_is_not_due_when_its_grace_has_elapsed() -> void:
	_server._pending_quit_at_msec = WOKE_AT_MSEC
	_block_for(StagehandServer.STALL_THRESHOLD_MS)

	assert_bool(_server._quit_is_due(WOKE_AT_MSEC + 1)).is_false()


func test_a_held_quit_comes_due_when_the_hold_ends() -> void:
	_server._pending_quit_at_msec = WOKE_AT_MSEC
	_block_for(StagehandServer.STALL_THRESHOLD_MS)

	assert_bool(
		_server._quit_is_due(WOKE_AT_MSEC + StagehandServer.STALL_RECOVERY_GRACE_MS)
	).is_true()


func test_a_hold_never_arms_a_quit_on_its_own() -> void:
	_block_for(StagehandServer.STALL_THRESHOLD_MS)

	assert_bool(
		_server._quit_is_due(WOKE_AT_MSEC + StagehandServer.STALL_RECOVERY_GRACE_MS)
	).is_false()


func test_an_unheld_quit_comes_due_at_its_grace() -> void:
	_server._pending_quit_at_msec = WOKE_AT_MSEC

	assert_bool(_server._quit_is_due(WOKE_AT_MSEC - 1)).is_false()
	assert_bool(_server._quit_is_due(WOKE_AT_MSEC)).is_true()


@warning_ignore_restore("return_value_discarded")
