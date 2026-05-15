extends Node
## WebSocket server that accepts JSON-RPC 2.0 commands from external clients.
## Registered as an autoload by the Stagehand editor plugin.
## Only activates when STAGEHAND_ENABLED=1 env var, --stagehand CLI flag,
## or the editor toolbar toggle is on.

const DEFAULT_PORT := 26700
const VERSION := "0.1.0"

const StagehandCommandRouter := preload("res://addons/stagehand/core/command_router.gd")
const StagehandInputRecorder := preload("res://addons/stagehand/core/input_recorder.gd")
const StagehandInputSimulator := preload("res://addons/stagehand/core/input_simulator.gd")
const StagehandJsonRpc := preload("res://addons/stagehand/protocol/json_rpc.gd")
const StagehandExpressionEvaluator := preload("res://addons/stagehand/core/expression_evaluator.gd")
const StagehandMethodHandler := preload("res://addons/stagehand/core/method_handler.gd")
const StagehandPropertyHandler := preload("res://addons/stagehand/core/property_handler.gd")
const StagehandSceneHandler := preload("res://addons/stagehand/core/scene_handler.gd")
const StagehandScreenshotCapture := preload("res://addons/stagehand/core/screenshot_capture.gd")
const StagehandTreeSerializer := preload("res://addons/stagehand/core/tree_serializer.gd")
const StagehandWaiter := preload("res://addons/stagehand/core/waiter.gd")

var _tcp_server: TCPServer
var _clients: Dictionary = {}  # int -> WebSocketPeer
var _next_peer_id: int = 0
var _router: StagehandCommandRouter
var _port: int = DEFAULT_PORT
var _active: bool = false
var _recorder: StagehandInputRecorder


func _ready() -> void:
	if not _is_enabled():
		set_process(false)
		return

	_router = StagehandCommandRouter.new()
	_register_builtin_handlers()

	_port = _get_port()
	_tcp_server = TCPServer.new()
	var err := _tcp_server.listen(_port)
	if err != OK:
		push_error("Stagehand: Failed to listen on port %d: %s" % [_port, error_string(err)])
		set_process(false)
		return

	_active = true
	print("Stagehand: Server listening on port %d" % _port)


func _process(_delta: float) -> void:
	if not _active:
		return
	_accept_new_connections()
	_poll_clients()


func _exit_tree() -> void:
	_stop()


## Return the command router so external code can register additional handlers.
func get_router() -> StagehandCommandRouter:
	return _router


## Whether the server is actively listening.
func is_active() -> bool:
	return _active


## The port the server is listening on (only meaningful when active).
func get_port() -> int:
	return _port


func _accept_new_connections() -> void:
	while _tcp_server.is_connection_available():
		var tcp_peer: StreamPeerTCP = _tcp_server.take_connection()
		var ws_peer := WebSocketPeer.new()
		var err := ws_peer.accept_stream(tcp_peer)
		if err == OK:
			var peer_id: int = _next_peer_id
			_next_peer_id += 1
			_clients[peer_id] = ws_peer
		else:
			push_warning("Stagehand: Failed to accept WebSocket stream: %s" % error_string(err))


func _poll_clients() -> void:
	var disconnected: Array[int] = []
	for peer_id: int in _clients:
		var ws: WebSocketPeer = _clients[peer_id]
		ws.poll()
		match ws.get_ready_state():
			WebSocketPeer.STATE_OPEN:
				while ws.get_available_packet_count() > 0:
					var packet: PackedByteArray = ws.get_packet()
					var text: String = packet.get_string_from_utf8()
					_handle_message(peer_id, text)
			WebSocketPeer.STATE_CLOSED:
				disconnected.append(peer_id)
	for peer_id: int in disconnected:
		_clients.erase(peer_id)


func _handle_message(peer_id: int, text: String) -> void:
	var parsed: Dictionary = StagehandJsonRpc.parse_request(text)
	if parsed.has("error"):
		_send_to_peer(peer_id, parsed["error"])
		return

	var request: Dictionary = parsed["request"]
	var id: Variant = request.get("id")
	var method: String = request["method"]
	var params: Variant = request.get("params", {})

	if not _router.has_handler(method):
		_send_to_peer(peer_id, StagehandJsonRpc.make_error_response(
			id, StagehandJsonRpc.METHOD_NOT_FOUND,
			"Method not found: %s" % method
		))
		return

	# Await so async handlers (e.g. screenshot) resolve before responding.
	var result: Variant = await _router.dispatch(method, params)
	# Notifications (no id) get no response per JSON-RPC 2.0 spec.
	if id != null:
		_send_to_peer(peer_id, StagehandJsonRpc.make_response(id, result))


func _send_to_peer(peer_id: int, text: String) -> void:
	if not _clients.has(peer_id):
		return
	var ws: WebSocketPeer = _clients[peer_id]
	if ws.get_ready_state() == WebSocketPeer.STATE_OPEN:
		ws.send_text(text)


func _register_builtin_handlers() -> void:
	_router.register("ping", _handle_ping)
	_router.register("get_tree", _handle_get_tree)
	_router.register("query_nodes", _handle_query_nodes)
	_router.register("get_property", _handle_get_property)
	_router.register("set_property", _handle_set_property)
	_router.register("get_game_state", _handle_get_game_state)
	_router.register("input_mouse", _handle_input_mouse)
	_router.register("input_action", _handle_input_action)
	_router.register("input_key", _handle_input_key)
	_router.register("input_touch", _handle_input_touch)
	_router.register("screenshot", _handle_screenshot)
	_router.register("call_method", _handle_call_method)
	_router.register("evaluate", _handle_evaluate)
	_router.register("change_scene", _handle_change_scene)
	_router.register("wait_for_node", _handle_wait_for_node)
	_router.register("wait_for_property", _handle_wait_for_property)
	_router.register("wait_signal", _handle_wait_signal)
	_router.register("get_performance", _handle_get_performance)
	_router.register("assert_performance", _handle_assert_performance)
	_router.register("record_start", _handle_record_start)
	_router.register("record_stop", _handle_record_stop)
	_router.register("replay", _handle_replay)


func _handle_input_mouse(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandInputSimulator.input_mouse(get_tree(), p)


func _handle_input_action(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandInputSimulator.input_action(get_tree(), p)


func _handle_input_key(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandInputSimulator.input_key(get_tree(), p)


func _handle_input_touch(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandInputSimulator.input_touch(get_tree(), p)


func _handle_screenshot(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return await StagehandScreenshotCapture.capture(get_tree(), p)


func _handle_ping(_params: Variant) -> Dictionary:
	return {
		"status": "ok",
		"engine": "godot",
		"engine_version": Engine.get_version_info()["string"],
		"stagehand_version": VERSION,
	}


func _handle_get_tree(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var root_path: String = p.get("root_path", "/root")
	var max_depth: int = p.get("max_depth", 10)
	var include_properties: Array[String] = []
	if p.has("properties"):
		for item: Variant in p["properties"]:
			include_properties.append(String(item))

	var root: Node = get_tree().root.get_node_or_null(NodePath(root_path))
	if root == null:
		return {"error": "Root node not found: %s" % root_path}

	return StagehandTreeSerializer.serialize_tree(root, max_depth, include_properties)


func _handle_query_nodes(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var properties: Array[String] = []
	if p.has("properties"):
		for item: Variant in p["properties"]:
			properties.append(String(item))
	var limit: int = p.get("limit", 50)
	return StagehandTreeSerializer.query_nodes(get_tree(), selector, properties, limit)


func _handle_get_property(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandPropertyHandler.get_property(get_tree(), p)


func _handle_set_property(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandPropertyHandler.set_property(get_tree(), p)


func _handle_call_method(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandMethodHandler.call_method(get_tree(), p)


func _handle_evaluate(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandExpressionEvaluator.evaluate(get_tree(), p)


func _handle_change_scene(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	return StagehandSceneHandler.change_scene(get_tree(), p)


func _handle_wait_signal(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var signal_name: String = p.get("signal_name", "")
	if signal_name.is_empty():
		return {"error": "Missing signal_name"}
	var timeout_ms: int = p.get("timeout_ms", 5000)
	var waiter := StagehandWaiter.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_signal(selector, signal_name, timeout_ms)
	waiter.queue_free()
	return result


## Maps Performance monitor names to their enum values.
const _PERFORMANCE_MONITORS: Dictionary = {
	"TIME_FPS": Performance.TIME_FPS,
	"TIME_PROCESS": Performance.TIME_PROCESS,
	"TIME_PHYSICS_PROCESS": Performance.TIME_PHYSICS_PROCESS,
	"TIME_NAVIGATION_PROCESS": Performance.TIME_NAVIGATION_PROCESS,
	"MEMORY_STATIC": Performance.MEMORY_STATIC,
	"MEMORY_STATIC_MAX": Performance.MEMORY_STATIC_MAX,
	"MEMORY_MESSAGE_BUFFER_MAX": Performance.MEMORY_MESSAGE_BUFFER_MAX,
	"OBJECT_COUNT": Performance.OBJECT_COUNT,
	"OBJECT_RESOURCE_COUNT": Performance.OBJECT_RESOURCE_COUNT,
	"OBJECT_NODE_COUNT": Performance.OBJECT_NODE_COUNT,
	"OBJECT_ORPHAN_NODE_COUNT": Performance.OBJECT_ORPHAN_NODE_COUNT,
	"RENDER_TOTAL_OBJECTS_IN_FRAME": Performance.RENDER_TOTAL_OBJECTS_IN_FRAME,
	"RENDER_TOTAL_PRIMITIVES_IN_FRAME": Performance.RENDER_TOTAL_PRIMITIVES_IN_FRAME,
	"RENDER_TOTAL_DRAW_CALLS_IN_FRAME": Performance.RENDER_TOTAL_DRAW_CALLS_IN_FRAME,
	"RENDER_VIDEO_MEM_USED": Performance.RENDER_VIDEO_MEM_USED,
	"RENDER_TEXTURE_MEM_USED": Performance.RENDER_TEXTURE_MEM_USED,
	"RENDER_BUFFER_MEM_USED": Performance.RENDER_BUFFER_MEM_USED,
	"PHYSICS_2D_ACTIVE_OBJECTS": Performance.PHYSICS_2D_ACTIVE_OBJECTS,
	"PHYSICS_2D_COLLISION_PAIRS": Performance.PHYSICS_2D_COLLISION_PAIRS,
	"PHYSICS_2D_ISLAND_COUNT": Performance.PHYSICS_2D_ISLAND_COUNT,
	"PHYSICS_3D_ACTIVE_OBJECTS": Performance.PHYSICS_3D_ACTIVE_OBJECTS,
	"PHYSICS_3D_COLLISION_PAIRS": Performance.PHYSICS_3D_COLLISION_PAIRS,
	"PHYSICS_3D_ISLAND_COUNT": Performance.PHYSICS_3D_ISLAND_COUNT,
	"AUDIO_OUTPUT_LATENCY": Performance.AUDIO_OUTPUT_LATENCY,
}

const _DEFAULT_PERFORMANCE_MONITORS: Array[String] = [
	"TIME_FPS", "TIME_PROCESS", "TIME_PHYSICS_PROCESS",
	"MEMORY_STATIC", "OBJECT_COUNT", "RENDER_TOTAL_DRAW_CALLS_IN_FRAME",
]


func _handle_get_performance(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var requested: Array = p.get("monitors", [])
	var to_query: Array = requested if not requested.is_empty() else _DEFAULT_PERFORMANCE_MONITORS
	var metrics: Dictionary = {}
	for item: Variant in to_query:
		var name: String = String(item)
		if _PERFORMANCE_MONITORS.has(name):
			metrics[name] = Performance.get_monitor(_PERFORMANCE_MONITORS[name])
		else:
			metrics[name] = null
	return {"metrics": metrics}


func _handle_assert_performance(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var monitor: String = p.get("monitor", "")
	var threshold: float = float(p.get("threshold", 0.0))
	var op: String = p.get("op", "lte")

	if monitor.is_empty():
		return {"error": "Missing monitor"}

	var perf: Dictionary = _handle_get_performance({"monitors": [monitor]})
	if perf.has("error"):
		return perf

	var metrics: Dictionary = perf.get("metrics", {})
	if not metrics.has(monitor) or metrics[monitor] == null:
		return {"error": "Unknown monitor: %s" % monitor}

	var value: float = float(metrics[monitor])
	var passed: bool = false
	match op:
		"lt":
			passed = value < threshold
		"lte":
			passed = value <= threshold
		"gt":
			passed = value > threshold
		"gte":
			passed = value >= threshold
		"eq":
			passed = value == threshold
		_:
			return {"error": "Unknown operator: %s" % op}

	var result: Dictionary = {
		"passed": passed,
		"monitor": monitor,
		"value": value,
		"threshold": threshold,
		"op": op,
	}
	if not passed:
		result["message"] = "%s: %.4f does not satisfy %s %.4f" % [monitor, value, op, threshold]
	return result


func _handle_get_game_state(_params: Variant) -> Dictionary:
	var viewport: Viewport = get_viewport()
	var size: Vector2i = viewport.size if viewport != null else Vector2i.ZERO
	return {
		"current_scene": str(get_tree().current_scene.scene_file_path) if get_tree().current_scene != null else null,
		"fps": Engine.get_frames_per_second(),
		"physics_ticks": Engine.get_physics_frames(),
		"window_size": {"x": size.x, "y": size.y},
		"connected": true,
		"engine_version": Engine.get_version_info()["string"],
	}


func _handle_wait_for_node(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var state: String = p.get("state", "exists")
	var timeout_ms: int = p.get("timeout_ms", 10000)
	var poll_interval_ms: int = p.get("poll_interval_ms", 100)

	var waiter := StagehandWaiter.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_node(selector, state, timeout_ms, poll_interval_ms)
	waiter.queue_free()
	return result


func _handle_wait_for_property(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var property: String = p.get("property", "")
	if property.is_empty():
		return {"error": "Missing property"}
	var operator: String = p.get("operator", "")
	if operator.is_empty():
		return {"error": "Missing operator"}
	var timeout_ms: int = p.get("timeout_ms", 10000)
	var poll_interval_ms: int = p.get("poll_interval_ms", 100)
	var expected_value: Variant = p.get("expected_value")

	var waiter := StagehandWaiter.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_property(selector, property, operator, expected_value, timeout_ms, poll_interval_ms)
	waiter.queue_free()
	return result


func _handle_record_start(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var output_path: String = p.get("output_path", "")
	if output_path.is_empty():
		return {"error": "Missing output_path"}
	_ensure_recorder()
	return _recorder.start_recording(output_path)


func _handle_record_stop(_params: Variant) -> Dictionary:
	_ensure_recorder()
	return _recorder.stop_recording()


func _handle_replay(params: Variant) -> Dictionary:
	var p: Dictionary = {} if params == null else params
	var input_path: String = p.get("input_path", "")
	if input_path.is_empty():
		return {"error": "Missing input_path"}
	_ensure_recorder()
	return await _recorder.start_replay(input_path)


func _ensure_recorder() -> void:
	if _recorder == null:
		_recorder = StagehandInputRecorder.new()
		add_child(_recorder)


func _stop() -> void:
	if not _active:
		return
	for peer_id: int in _clients:
		_clients[peer_id].close()
	_clients.clear()
	if _tcp_server:
		_tcp_server.stop()
	_active = false
	print("Stagehand: Server stopped")


static func _is_enabled() -> bool:
	if OS.get_environment("STAGEHAND_ENABLED") == "1":
		return true
	if "--stagehand" in OS.get_cmdline_args():
		return true
	if "--stagehand" in OS.get_cmdline_user_args():
		return true
	if ProjectSettings.get_setting("stagehand/server/enabled", false):
		return true
	return false


static func _get_port() -> int:
	var env_port: String = OS.get_environment("STAGEHAND_PORT")
	if env_port != "" and env_port.is_valid_int():
		return env_port.to_int()
	for arg: String in OS.get_cmdline_user_args():
		if arg.begins_with("--stagehand-port="):
			var port_str: String = arg.substr("--stagehand-port=".length())
			if port_str.is_valid_int():
				return port_str.to_int()
	return DEFAULT_PORT
