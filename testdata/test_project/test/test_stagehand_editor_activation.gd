@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the editor-only toolbar activation argument.

const StagehandPlugin := preload("res://addons/stagehand/plugin.gd")


func test_toolbar_enable_appends_stagehand_argument() -> void:
	assert_str(StagehandPlugin._run_args_with_activation("--verbose", true)).is_equal(
		"--verbose --stagehand"
	)


func test_toolbar_disable_removes_only_stagehand_argument() -> void:
	assert_str(
		StagehandPlugin._run_args_with_activation("--verbose --stagehand --debug", false)
	).is_equal("--verbose --debug")


func test_toolbar_enable_does_not_duplicate_stagehand_argument() -> void:
	assert_str(
		StagehandPlugin._run_args_with_activation("--stagehand --verbose", true)
	).is_equal("--verbose --stagehand")


@warning_ignore_restore("return_value_discarded")
