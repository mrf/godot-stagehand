# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandSceneHandler — change_scene path validation and the
## success envelope.
##
## change_scene_to_file swaps the running scene, which would pull the tree out
## from under the test runner, so the happy path is exercised against a real,
## loadable scene and then the tree is restored in after_test.

const TEST_SCENE: String = "res://scenes/test_scene.tscn"

var _original_scene: Node


func before_test() -> void:
	_original_scene = get_tree().current_scene


func after_test() -> void:
	# Undo any scene swap so later suites see the tree they expect.
	get_tree().current_scene = _original_scene


func _change(params: Dictionary) -> Dictionary:
	return StagehandSceneHandler.change_scene(get_tree(), params)


# ── error cases ──────────────────────────────────────────────────────────

func test_missing_scene_path_returns_error() -> void:
	var result: Dictionary = _change({})
	assert_str(str(result.get("error", ""))).contains("Missing scene_path")
	assert_bool(result.has("success")).is_false()


func test_empty_scene_path_returns_error() -> void:
	var result: Dictionary = _change({"scene_path": ""})
	assert_str(str(result.get("error", ""))).contains("Missing scene_path")


func test_nonexistent_scene_path_returns_not_found() -> void:
	var result: Dictionary = _change({"scene_path": "res://scenes/no_such_scene.tscn"})
	assert_str(str(result.get("error", ""))).contains("Scene file not found")
	assert_bool(result.has("success")).is_false()


func test_non_scene_resource_path_returns_not_found() -> void:
	# A path outside the project's resource filesystem can't be loaded.
	var result: Dictionary = _change({"scene_path": "/etc/hostname"})
	assert_str(str(result.get("error", ""))).contains("Scene file not found")


func test_directory_path_returns_not_found() -> void:
	var result: Dictionary = _change({"scene_path": "res://scenes/"})
	assert_str(str(result.get("error", ""))).contains("Scene file not found")


func test_failed_change_does_not_report_success() -> void:
	var result: Dictionary = _change({"scene_path": "res://nope.tscn"})
	assert_bool(result.get("success", false)).is_false()


# ── happy path ───────────────────────────────────────────────────────────

func test_valid_scene_path_succeeds() -> void:
	var result: Dictionary = _change({"scene_path": TEST_SCENE})
	assert_bool(result.get("success", false)).is_true()
	assert_bool(result.has("error")).is_false()


func test_valid_scene_path_echoes_new_scene() -> void:
	var result: Dictionary = _change({"scene_path": TEST_SCENE})
	assert_str(str(result.get("new_scene"))).is_equal(TEST_SCENE)


func test_valid_scene_path_reports_previous_scene_field() -> void:
	# previous_scene is always present on success, even when there was no
	# current scene (script-mode runs start with none), in which case it is "".
	var result: Dictionary = _change({"scene_path": TEST_SCENE})
	assert_bool(result.has("previous_scene")).is_true()
