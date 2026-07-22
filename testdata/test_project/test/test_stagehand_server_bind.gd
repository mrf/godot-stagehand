@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandServer bind-failure self-quit gating.
##
## When TCPServer.listen() fails (port collision), the server must self-quit so
## headless game launches don't linger forever as zombie processes. But that
## self-quit must fire ONLY when the server was activated as a game/CLI run —
## the STAGEHAND_ENABLED env var or the --stagehand CLI flag.

const StagehandServer := preload("res://addons/stagehand/autoload/stagehand_server.gd")

const _ENV: String = "STAGEHAND_ENABLED"
const _SETTING: String = "stagehand/server/enabled"

var _saved_env: String


func before_each() -> void:
	_saved_env = OS.get_environment(_ENV)


func after_each() -> void:
	OS.set_environment(_ENV, _saved_env)
	ProjectSettings.set_setting(_SETTING, false)


func test_no_explicit_activation_does_not_self_quit() -> void:
	OS.set_environment(_ENV, "")
	assert_bool(StagehandServer._enabled_via_game_launch()).is_false()


func test_env_var_activation_self_quits() -> void:
	OS.set_environment(_ENV, "1")
	assert_bool(StagehandServer._enabled_via_game_launch()).is_true()


func test_stale_project_setting_does_not_activate_server() -> void:
	OS.set_environment(_ENV, "")
	ProjectSettings.set_setting(_SETTING, true)
	assert_bool(StagehandServer._enabled_via_game_launch()).is_false()
	assert_bool(StagehandServer._is_enabled()).is_false()


func test_editor_play_allows_explicit_activation() -> void:
	assert_bool(StagehandServer._activation_allowed(true, false, false)).is_true()


func test_debug_export_allows_explicit_activation() -> void:
	assert_bool(StagehandServer._activation_allowed(true, false, false)).is_true()


func test_release_export_rejects_ordinary_activation() -> void:
	assert_bool(StagehandServer._activation_allowed(true, true, false)).is_false()


func test_release_export_requires_explicit_release_opt_in() -> void:
	assert_bool(StagehandServer._activation_allowed(true, true, true)).is_true()


func test_exit_code_is_nonzero() -> void:
	# A self-quit on bind failure must report a nonzero exit code so port
	# collisions are distinguishable from a clean shutdown.
	assert_int(StagehandServer.BIND_FAILURE_EXIT_CODE).is_not_equal(0)


@warning_ignore_restore("return_value_discarded")
