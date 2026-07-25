## Handles change_scene command.
class_name StagehandSceneHandler
extends RefCounted

const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## Load and switch to a new scene.
## Params: { scene_path: string }
static func change_scene(tree: SceneTree, params: Dictionary) -> Dictionary:
	var scene_path: String = params.get("scene_path", "")
	if scene_path.is_empty():
		return ERRORS.missing_param("scene_path")

	# Validate path exists (optional, but gives better error messages).
	if not ResourceLoader.exists(scene_path):
		return ERRORS.make(ERRORS.SCENE_NOT_FOUND, "Scene file not found: %s" % scene_path, {
			"scene_path": scene_path,
			"next_action": "Use a res:// path to a .tscn/.scn that is present in the exported project.",
		})

	var current_scene: Node = tree.current_scene
	var current_path: String = ""
	if current_scene != null:
		current_path = current_scene.scene_file_path

	var err: Error = tree.change_scene_to_file(scene_path)
	if err != OK:
		return ERRORS.make(
			ERRORS.SCENE_CHANGE_FAILED,
			"Failed to change scene: %s" % error_string(err),
			{
				"scene_path": scene_path,
				"godot_error": error_string(err),
				"next_action": "Check the Godot output for load errors in the target scene.",
			}
		)

	# Change is deferred to next frame; client can poll get_game_state to confirm.
	return {
		"success": true,
		"previous_scene": current_path,
		"new_scene": scene_path,
	}
