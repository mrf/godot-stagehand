@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandServer port resolution.
##
## --stagehand-port= must take effect regardless of whether it lands before or
## after the `--` separator in the launch command — Godot silently drops
## unrecognized pre-separator flags, which previously made the port flag a
## silent no-op unless the caller remembered to route it through
## get_cmdline_user_args() (i.e. put it after --).

const StagehandServer := preload("res://addons/stagehand/autoload/stagehand_server.gd")


func test_no_env_or_flag_uses_default_port() -> void:
	assert_int(StagehandServer._resolve_port("", PackedStringArray(), PackedStringArray())).is_equal(
		StagehandServer.DEFAULT_PORT
	)


func test_env_var_takes_precedence_over_flag() -> void:
	var user_args: PackedStringArray = PackedStringArray(["--stagehand-port=26711"])
	assert_int(StagehandServer._resolve_port("26722", user_args, PackedStringArray())).is_equal(26722)


func test_flag_after_separator_is_honored() -> void:
	var user_args: PackedStringArray = PackedStringArray(["--stagehand", "--stagehand-port=26711"])
	assert_int(StagehandServer._resolve_port("", user_args, PackedStringArray())).is_equal(26711)


func test_flag_before_separator_is_still_honored() -> void:
	# The bug this guards: a hand-rolled launch that puts --stagehand-port=
	# before -- was silently ignored, falling back to DEFAULT_PORT with no
	# indication anything was wrong.
	var engine_args: PackedStringArray = PackedStringArray(["--headless", "--stagehand-port=26711"])
	assert_int(StagehandServer._resolve_port("", PackedStringArray(), engine_args)).is_equal(26711)


func test_flag_after_separator_wins_over_flag_before_separator() -> void:
	var user_args: PackedStringArray = PackedStringArray(["--stagehand-port=26711"])
	var engine_args: PackedStringArray = PackedStringArray(["--stagehand-port=26722"])
	assert_int(StagehandServer._resolve_port("", user_args, engine_args)).is_equal(26711)


func test_invalid_flag_value_falls_back_to_default() -> void:
	var user_args: PackedStringArray = PackedStringArray(["--stagehand-port=not-a-number"])
	assert_int(StagehandServer._resolve_port("", user_args, PackedStringArray())).is_equal(
		StagehandServer.DEFAULT_PORT
	)


@warning_ignore_restore("return_value_discarded")
