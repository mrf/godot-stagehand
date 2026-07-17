extends Node
## WebSocket server that accepts JSON-RPC 2.0 commands from external clients.
## Registered as an autoload by the Stagehand editor plugin.
## Only activates when STAGEHAND_ENABLED=1 env var, --stagehand CLI flag,
## or the editor toolbar toggle is on.

const DEFAULT_PORT: int = 26700
const DEFAULT_BIND_ADDRESS: String = "127.0.0.1"
const VERSION: String = "0.1.0"
const AUTHENTICATION_REQUIRED: int = -32001
const AUTHENTICATION_FAILED: int = -32002
const UNSAFE_CAPABILITY_REQUIRED: int = -32003
## Exit code used when a game/CLI launch self-quits because the WebSocket port
## could not be bound. Nonzero so the failure is distinguishable from a clean
## shutdown. 70 == EX_SOFTWARE (sysexits.h).
const BIND_FAILURE_EXIT_CODE: int = 70

# These scripts are preloaded into SCREAMING_SNAKE_CASE constants rather than
# referenced by their global `class_name`. Two constraints force this:
#   1. In a headless game launch the project's global class cache may be empty
#      (it is populated by the editor), so the global class_names are not
#      resolvable as types — an explicit preload is required.
#   2. A const named identically to a script's global class_name shadows that
#      global identifier, which is an error under
#      gdscript/warnings/exclude_addons=false + warnings-as-errors.
# SCREAMING_SNAKE_CASE names satisfy both: they always resolve (case 1) and do
# not collide with the PascalCase global class_names (case 2). This mirrors the
# SELECTOR_ENGINE convention already used in core/waiter.gd.
const COMMAND_ROUTER := preload("res://addons/stagehand/core/command_router.gd")
const INPUT_RECORDER := preload("res://addons/stagehand/core/input_recorder.gd")
const INPUT_SIMULATOR := preload("res://addons/stagehand/core/input_simulator.gd")
const JSON_RPC := preload("res://addons/stagehand/protocol/json_rpc.gd")
const EXPRESSION_EVALUATOR := preload("res://addons/stagehand/core/expression_evaluator.gd")
const METHOD_HANDLER := preload("res://addons/stagehand/core/method_handler.gd")
const PROPERTY_HANDLER := preload("res://addons/stagehand/core/property_handler.gd")
const SCENE_HANDLER := preload("res://addons/stagehand/core/scene_handler.gd")
const SCREENSHOT_CAPTURE := preload("res://addons/stagehand/core/screenshot_capture.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")
const WAITER := preload("res://addons/stagehand/core/waiter.gd")

var _tcp_server: TCPServer
var _clients: Dictionary = {}  # int -> WebSocketPeer
var _authenticated_peers: Dictionary = {}  # int -> true
var _next_peer_id: int = 0
var _router: COMMAND_ROUTER
var _port: int = DEFAULT_PORT
var _bind_address: String = DEFAULT_BIND_ADDRESS
var _auth_token: String = ""
var _allow_unsafe: bool = false
var _active: bool = false
var _recorder: INPUT_RECORDER


func _ready() -> void:
	if not _is_enabled():
		set_process(false)
		return

	_router = COMMAND_ROUTER.new()
	_register_builtin_handlers()

	_port = _get_port()
	_bind_address = _get_bind_address()
	_auth_token = _get_auth_token()
	_allow_unsafe = OS.get_environment("STAGEHAND_ALLOW_UNSAFE") == "1"
	if OS.get_environment("STAGEHAND_AUTH_TOKEN").is_empty():
		print("Stagehand: Authentication token: %s" % _auth_token)
	if _allow_unsafe:
		push_warning("Stagehand: WARNING: unsafe evaluate and call_method capabilities are enabled")
	_tcp_server = TCPServer.new()
	var err: Error = _tcp_server.listen(_port, _bind_address)
	if err != OK:
		push_error("Stagehand: Failed to listen on port %d: %s" % [_port, error_string(err)])
		set_process(false)
		# A game/CLI launch (STAGEHAND_ENABLED env var or --stagehand flag) that
		# cannot bind the port would otherwise run forever as a headless zombie
		# with no server (~500MB each), accumulating on every port collision.
		# Self-quit so the launcher's port is freed and no zombie lingers.
		# The editor toolbar toggle (ProjectSettings activation) deliberately does
		# NOT self-quit: an occupied port during interactive editor play must not
		# tear down the running session.
		if _enabled_via_game_launch():
			push_error("Stagehand: Quitting (game launch) — port %d unavailable." % _port)
			get_tree().quit(BIND_FAILURE_EXIT_CODE)
		return

	_active = true
	print("Stagehand: Server listening on port %d (%s)" % [_port, _bind_address])


func _process(_delta: float) -> void:
	if not _active:
		return
	_accept_new_connections()
	_poll_clients()


func _exit_tree() -> void:
	_stop()


## Return the command router so external code can register additional handlers.
func get_router() -> COMMAND_ROUTER:
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
		var ws_peer: WebSocketPeer = WebSocketPeer.new()
		# Screenshot responses are base64-encoded PNGs of the full viewport and
		# routinely exceed the 64 KiB default WebSocket buffer. If the outbound
		# buffer is too small, send_text() fails with ERR_OUT_OF_MEMORY and the
		# response is silently dropped. Size the buffers for a 4K-resolution PNG.
		ws_peer.inbound_buffer_size = 1 << 23   # 8 MiB (large replay payloads)
		ws_peer.outbound_buffer_size = 1 << 24  # 16 MiB (full-viewport screenshots)
		var err: Error = ws_peer.accept_stream(tcp_peer)
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
		var _erased: bool = _clients.erase(peer_id)
		var _auth_erased: bool = _authenticated_peers.erase(peer_id)


func _handle_message(peer_id: int, text: String) -> void:
	var parsed: Dictionary = JSON_RPC.parse_request(text)
	if parsed.has("error"):
		var error_text: String = parsed["error"]
		var _parse_send_error: Error = _send_to_peer(peer_id, error_text)
		return

	var request: Dictionary = parsed["request"]
	var id: Variant = request.get("id")
	var method: String = request["method"]
	var params: Variant = request.get("params", {})
	if method == "authenticate":
		_authenticate_peer(peer_id, id, params)
		return

	if not _authenticated_peers.has(peer_id):
		_send_rpc_error(
			peer_id,
			id,
			AUTHENTICATION_REQUIRED,
			"Authentication required before calling Stagehand methods"
		)
		return

	if _is_unsafe_method(method) and not _allow_unsafe:
		_send_rpc_error(
			peer_id,
			id,
			UNSAFE_CAPABILITY_REQUIRED,
			"Unsafe method disabled; relaunch with an explicit unsafe-capability opt-in"
		)
		return

	if not _router.has_handler(method):
		var _method_send_error: Error = _send_to_peer(peer_id, JSON_RPC.make_error_response(
			id, JSON_RPC.METHOD_NOT_FOUND,
			"Method not found: %s" % method
		))
		return

	# Dispatch on next idle frame so _process never blocks on async handlers.
	# This avoids the "coroutine not awaited" strict-mode warning and keeps
	# the WebSocket poll loop running every frame.
	call_deferred("_dispatch_and_respond", peer_id, id, method, params)


func _authenticate_peer(peer_id: int, id: Variant, params: Variant) -> void:
	var p: Dictionary = _params(params)
	var supplied: Variant = p.get("token")
	if supplied is not String:
		_send_rpc_error(peer_id, id, AUTHENTICATION_FAILED, "Authentication failed")
		return
	var supplied_token: String = supplied
	if supplied_token != _auth_token:
		_send_rpc_error(peer_id, id, AUTHENTICATION_FAILED, "Authentication failed")
		return
	_authenticated_peers[peer_id] = true
	if id != null:
		var _auth_send_error: Error = _send_to_peer(peer_id, JSON_RPC.make_response(
			id, {"authenticated": true}
		))


func _send_rpc_error(peer_id: int, id: Variant, code: int, message: String) -> void:
	if id != null:
		var _rpc_send_error: Error = _send_to_peer(
			peer_id, JSON_RPC.make_error_response(id, code, message)
		)


static func _is_unsafe_method(method: String) -> bool:
	return method == "evaluate" or method == "call_method"


func _dispatch_and_respond(peer_id: int, id: Variant, method: String, params: Variant) -> void:
	# Await the handler Callable directly rather than going through dispatch():
	# coroutine handlers (e.g. screenshot, input_text) suspend on `await`, and a
	# synchronous dispatch() would return null instead of the real result. The
	# method was confirmed registered in _handle_message before deferring here.
	var handler: Callable = _router.get_handler(method)
	var result: Variant = await handler.call(params)
	# Notifications (no id) get no response per JSON-RPC 2.0 spec.
	if id != null:
		var response_text: String = JSON_RPC.make_response(id, result)
		var send_error: Error = _send_to_peer(peer_id, response_text)
		if send_error != OK:
			var fallback_result: Dictionary = {
				"error": "Failed to send Stagehand response to WebSocket peer: %s" % error_string(send_error),
				"error_code": "send_buffer_failed",
				"details": {
					"payload_bytes": response_text.to_utf8_buffer().size(),
					"next_action": "Reduce screenshot size/crop area or increase the WebSocket outbound buffer.",
				},
			}
			var _fallback_send_error: Error = _send_to_peer(peer_id, JSON_RPC.make_response(id, fallback_result))


func _send_to_peer(peer_id: int, text: String) -> Error:
	if not _clients.has(peer_id):
		return ERR_UNAVAILABLE
	var ws: WebSocketPeer = _clients[peer_id]
	if ws.get_ready_state() == WebSocketPeer.STATE_OPEN:
		var err: Error = ws.send_text(text)
		if err != OK:
			# A failed send (commonly ERR_OUT_OF_MEMORY when the payload exceeds
			# outbound_buffer_size) drops the response and leaves the client
			# waiting. Surface it instead of swallowing it silently.
			push_error("Stagehand: send_text failed (%s) for a %d-byte payload; raise outbound_buffer_size" % [
				error_string(err), text.to_utf8_buffer().size()
			])
		return err
	return ERR_UNAVAILABLE


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
	_router.register("input_text", _handle_input_text)
	_router.register("input_touch", _handle_input_touch)
	_router.register("input_mouse_move", _handle_input_mouse_move)
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
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.input_mouse(get_tree(), p)


func _handle_input_action(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.input_action(get_tree(), p)


func _handle_input_key(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.input_key(get_tree(), p)


func _handle_input_touch(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.input_touch(get_tree(), p)


func _handle_input_text(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return await INPUT_SIMULATOR.input_text(get_tree(), p)


func _handle_input_mouse_move(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.input_mouse_move(get_tree(), p)


func _handle_screenshot(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return await SCREENSHOT_CAPTURE.capture(get_tree(), p)


func _handle_ping(_unused_params: Variant) -> Dictionary:
	# Echo the per-launch instance token so the launcher can prove the process it
	# spawned is the one it connected to. Empty when launched without a token
	# (e.g. manual --stagehand runs); the launcher only asserts it for launches
	# it initiated.
	return {
		"status": "ok",
		"engine": "godot",
		"engine_version": Engine.get_version_info()["string"],
		"stagehand_version": VERSION,
		"instance_token": OS.get_environment("STAGEHAND_INSTANCE_TOKEN"),
	}


func _handle_get_tree(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var root_path: String = p.get("root_path", "/root")
	var max_depth: int = p.get("max_depth", 10)
	var include_properties: Array[String] = []
	if p.has("properties"):
		for item: Variant in p["properties"]:
			include_properties.append(str(item))

	var root: Node = get_tree().root.get_node_or_null(NodePath(root_path))
	if root == null:
		return {"error": "Root node not found: %s" % root_path}

	return TREE_SERIALIZER.serialize_tree(root, max_depth, include_properties)


func _handle_query_nodes(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var properties: Array[String] = []
	if p.has("properties"):
		for item: Variant in p["properties"]:
			properties.append(str(item))
	var limit: int = p.get("limit", 50)
	return TREE_SERIALIZER.query_nodes(get_tree(), selector, properties, limit)


func _handle_get_property(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return PROPERTY_HANDLER.get_property(get_tree(), p)


func _handle_set_property(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return PROPERTY_HANDLER.set_property(get_tree(), p)


func _handle_call_method(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return METHOD_HANDLER.call_method(get_tree(), p)


func _handle_evaluate(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return EXPRESSION_EVALUATOR.evaluate(get_tree(), p)


func _handle_change_scene(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return SCENE_HANDLER.change_scene(get_tree(), p)


func _handle_wait_signal(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var signal_name: String = p.get("signal_name", "")
	if signal_name.is_empty():
		return {"error": "Missing signal_name"}
	var timeout_ms: int = p.get("timeout_ms", 5000)
	var waiter: WAITER = WAITER.new()
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
	var p: Dictionary = _params(params)
	var requested: Array = p.get("monitors", [])
	var to_query: Array = requested if not requested.is_empty() else _DEFAULT_PERFORMANCE_MONITORS
	var metrics: Dictionary = {}
	for item: Variant in to_query:
		var monitor_name: String = str(item)
		if _PERFORMANCE_MONITORS.has(monitor_name):
			var monitor_enum: Performance.Monitor = _PERFORMANCE_MONITORS[monitor_name]
			metrics[monitor_name] = Performance.get_monitor(monitor_enum)
		else:
			metrics[monitor_name] = null
	return {"metrics": metrics}


func _handle_assert_performance(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var monitor: String = p.get("monitor", "")
	var threshold: float = _to_float(p.get("threshold", 0.0))
	var op: String = p.get("op", "lte")

	if monitor.is_empty():
		return {"error": "Missing monitor"}

	var perf: Dictionary = _handle_get_performance({"monitors": [monitor]})
	if perf.has("error"):
		return perf

	var metrics: Dictionary = perf.get("metrics", {})
	if not metrics.has(monitor) or metrics[monitor] == null:
		return {"error": "Unknown monitor: %s" % monitor}

	var value: float = _to_float(metrics[monitor])
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


func _handle_get_game_state(_unused_params: Variant) -> Dictionary:
	var window: Window = get_window()
	var size: Vector2i = window.size if window != null else Vector2i.ZERO
	var current_scene_path: String = ""
	if get_tree().current_scene != null:
		current_scene_path = get_tree().current_scene.scene_file_path
	return {
		"current_scene": current_scene_path,
		"fps": Engine.get_frames_per_second(),
		"physics_ticks": Engine.get_physics_frames(),
		"window_size": {"x": size.x, "y": size.y},
		"connected": true,
		"engine_version": Engine.get_version_info()["string"],
	}


func _handle_wait_for_node(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return {"error": "Missing selector"}
	var state: String = p.get("state", "exists")
	var timeout_ms: int = p.get("timeout_ms", 10000)
	var poll_interval_ms: int = p.get("poll_interval_ms", 100)

	var waiter: WAITER = WAITER.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_node(selector, state, timeout_ms, poll_interval_ms)
	waiter.queue_free()
	return result


func _handle_wait_for_property(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
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

	var waiter: WAITER = WAITER.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_property(selector, property, operator, expected_value, timeout_ms, poll_interval_ms)
	waiter.queue_free()
	return result


func _handle_record_start(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var output_path: String = p.get("output_path", "")
	if output_path.is_empty():
		return {"error": "Missing output_path"}
	_ensure_recorder()
	return _recorder.start_recording(output_path)


func _handle_record_stop(_unused_params: Variant) -> Dictionary:
	_ensure_recorder()
	return _recorder.stop_recording()


func _handle_replay(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var input_path: String = p.get("input_path", "")
	if input_path.is_empty():
		return {"error": "Missing input_path"}
	_ensure_recorder()
	return await _recorder.start_replay(input_path)


func _ensure_recorder() -> void:
	if _recorder == null:
		_recorder = INPUT_RECORDER.new()
		add_child(_recorder)


func _stop() -> void:
	if not _active:
		return
	if _recorder != null and _recorder._recording:
		var _result: Dictionary = _recorder.stop_recording()
	for peer_id: int in _clients:
		var ws: WebSocketPeer = _clients[peer_id]
		ws.close()
	_clients.clear()
	_authenticated_peers.clear()
	if _tcp_server:
		_tcp_server.stop()
	_active = false
	print("Stagehand: Server stopped")


static func _is_enabled() -> bool:
	if _enabled_via_game_launch():
		return true
	# Editor toolbar toggle persists activation here. This path enables the
	# server but is NOT treated as a game launch (see _enabled_via_game_launch).
	if ProjectSettings.get_setting("stagehand/server/enabled", false):
		return true
	return false


## Whether the server was activated as an explicit game/CLI launch — the
## STAGEHAND_ENABLED env var or the --stagehand CLI flag. This is the only
## activation path that self-quits on a WebSocket bind failure; the editor
## toolbar toggle (ProjectSettings) intentionally does not.
static func _enabled_via_game_launch() -> bool:
	if OS.get_environment("STAGEHAND_ENABLED") == "1":
		return true
	if "--stagehand" in OS.get_cmdline_args():
		return true
	if "--stagehand" in OS.get_cmdline_user_args():
		return true
	return false


static func _to_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0


static func _params(params: Variant) -> Dictionary:
	if params == null:
		return {}
	if not params is Dictionary:
		push_warning("Stagehand: params must be a Dictionary (got %s); ignoring" % type_string(typeof(params)))
		return {}
	# Assign through a typed local instead of `params as Dictionary`: an explicit
	# `as` cast from Variant trips the unsafe_cast warning, whereas a checked
	# assignment does not.
	var dict: Dictionary = params
	return dict


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


static func _get_bind_address() -> String:
	var requested: String = OS.get_environment("STAGEHAND_BIND_ADDRESS").strip_edges()
	if requested.is_empty():
		return DEFAULT_BIND_ADDRESS
	if requested.begins_with("127.") or requested == "::1":
		return requested
	if OS.get_environment("STAGEHAND_ALLOW_REMOTE") == "1":
		push_warning(
			"Stagehand: WARNING: non-loopback bind address %s explicitly enabled; " % requested
			+ "any network peer still needs the session authentication token"
		)
		return requested
	push_warning(
		"Stagehand: Ignoring non-loopback bind address %s without STAGEHAND_ALLOW_REMOTE=1; " % requested
		+ "binding to %s" % DEFAULT_BIND_ADDRESS
	)
	return DEFAULT_BIND_ADDRESS


static func _get_auth_token() -> String:
	var configured: String = OS.get_environment("STAGEHAND_AUTH_TOKEN")
	if not configured.is_empty():
		return configured
	var entropy: PackedByteArray = OS.get_entropy(32)
	return entropy.hex_encode()
