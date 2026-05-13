## Handles change_scene command.
class_name StagehandSceneHandler
extends RefCounted

## Load and switch to a new scene.
## Params: { scene_path: string }
static func change_scene(tree: SceneTree, params: Dictionary) -> Dictionary:
	var scene_path: String = params.get("scene_path", "")
	if scene_path.is_empty():
		return {"error": "Missing scene_path"}
	
	# Validate path exists (optional, but gives better error messages).
	if not ResourceLoader.exists(scene_path):
		return {"error": "Scene file not found: %s" % scene_path}
	
	var current_scene := tree.current_scene
	var current_path := current_scene.scene_file_path if current_scene != null else null
	
	# Use change_scene_to_file (Godot 4+).
	var err := tree.change_scene_to_file(scene_path)
	if err != OK:
		return {"error": "Failed to change scene: %s" % error_string(err)}
	
	# Wait until the scene is actually changed? The change is deferred and will happen
	# after the current frame. We can't wait here synchronously because we need to respond.
	# The client can poll get_game_state to see when current_scene changes.
	return {
		"success": true,
		"previous_scene": current_path,
		"new_scene": scene_path,
	}