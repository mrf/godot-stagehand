extends Node
## WebSocket server that accepts JSON-RPC 2.0 commands from external clients.
## Registered as an autoload by the Stagehand editor plugin.
## Only activates when STAGEHAND_ENABLED=1 or the --stagehand CLI flag is
## supplied. Release exports additionally require STAGEHAND_ALLOW_RELEASE=1.

const DEFAULT_PORT: int = 26700
const DEFAULT_BIND_ADDRESS: String = "127.0.0.1"
const AUTHENTICATION_REQUIRED: int = -32001
const AUTHENTICATION_FAILED: int = -32002
const UNSAFE_CAPABILITY_REQUIRED: int = -32003
## Exit code used when a game/CLI launch self-quits because the WebSocket port
## could not be bound. Nonzero so the failure is distinguishable from a clean
## shutdown. 70 == EX_SOFTWARE (sysexits.h).
const BIND_FAILURE_EXIT_CODE: int = 70
## Grace period after the last client disconnects before a game/CLI launch
## self-quits. The MCP client reconnects with exponential backoff capped at
## 5s (see internal/godotconn/reconnect.go maxBackoff), so this must clear
## that cap with headroom for the TCP+WS handshake and auth round trip —
## otherwise a transient network flap would be indistinguishable from the
## client actually going away and would kill the game mid-session.
const QUIT_ON_DISCONNECT_GRACE_MS: int = 10000
## Replay speed used when the client does not ask for one: realtime.
const RECORDER_SPEED_DEFAULT: float = 1.0
## Hard cap on concurrent client connections. Each accepted peer reserves
## inbound_buffer_size + outbound_buffer_size (24 MiB, see
## _accept_new_connections), so an unbounded accept path is a memory-exhaustion
## vector. Connections beyond the cap are refused at the TCP layer before a
## WebSocketPeer (and its buffers) is ever allocated for them.
const MAX_CONCURRENT_CLIENTS: int = 32
## A peer that never completes the WebSocket handshake (stuck in
## STATE_CONNECTING — e.g. a TCP connection opened and then abandoned) is
## force-closed and reaped after this many milliseconds, so it can't hold a
## client slot and its buffers forever.
const HANDSHAKE_TIMEOUT_MS: int = 10000

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
const ACCESSIBILITY_TREE := preload("res://addons/stagehand/core/accessibility_tree.gd")
const COMMAND_ROUTER := preload("res://addons/stagehand/core/command_router.gd")
const ERRORS := preload("res://addons/stagehand/core/errors.gd")
const INPUT_RECORDER := preload("res://addons/stagehand/core/input_recorder.gd")
const INPUT_SIMULATOR := preload("res://addons/stagehand/core/input_simulator.gd")
const JSON_RPC := preload("res://addons/stagehand/protocol/json_rpc.gd")
const EXPRESSION_EVALUATOR := preload("res://addons/stagehand/core/expression_evaluator.gd")
const METHOD_HANDLER := preload("res://addons/stagehand/core/method_handler.gd")
const PERFORMANCE_SAMPLER := preload("res://addons/stagehand/core/performance_sampler.gd")
const PROPERTY_HANDLER := preload("res://addons/stagehand/core/property_handler.gd")
const SCENE_HANDLER := preload("res://addons/stagehand/core/scene_handler.gd")
const SCREENSHOT_CAPTURE := preload("res://addons/stagehand/core/screenshot_capture.gd")
const STAGEHAND_VERSION := preload("res://addons/stagehand/stagehand_version.gd")
const TREE_SERIALIZER := preload("res://addons/stagehand/core/tree_serializer.gd")
const WAITER := preload("res://addons/stagehand/core/waiter.gd")

var _tcp_server: TCPServer
var _clients: Dictionary = {}  # int -> WebSocketPeer
var _authenticated_peers: Dictionary = {}  # int -> true
var _peer_connected_at_msec: Dictionary = {}  # int -> int
var _next_peer_id: int = 0
var _router: COMMAND_ROUTER
var _port: int = DEFAULT_PORT
var _bind_address: String = DEFAULT_BIND_ADDRESS
var _auth_token: String = ""
var _allow_unsafe: bool = false
var _active: bool = false
var _recorder: INPUT_RECORDER
var _quit_on_disconnect: bool = true
var _had_client: bool = false
var _pending_quit_at_msec: int = -1


func _ready() -> void:
	# A pure-networking node with no gameplay side effects: it must keep
	# servicing its TCP accept/WebSocket poll loop even if the host game
	# pauses the SceneTree (e.g. an intro/menu overlay at startup), or a
	# client's WebSocket handshake never completes while paused
	# (godot-stagehand-sprh). Set unconditionally, before the enablement
	# gate below, since a disabled server already stops processing via
	# set_process(false) regardless of process_mode.
	process_mode = Node.PROCESS_MODE_ALWAYS

	if not _is_enabled():
		set_process(false)
		return

	_router = COMMAND_ROUTER.new()
	_register_builtin_handlers()

	_port = _get_port()
	_bind_address = _get_bind_address()
	_auth_token = _get_auth_token()
	_allow_unsafe = OS.get_environment("STAGEHAND_ALLOW_UNSAFE") == "1"
	_quit_on_disconnect = OS.get_environment("STAGEHAND_QUIT_ON_DISCONNECT") != "0"
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
	_check_pending_quit()


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
		if _clients.size() >= MAX_CONCURRENT_CLIENTS:
			# Refuse before a WebSocketPeer (and its 24 MiB of buffers) is ever
			# allocated. No WebSocket close handshake is possible here — the
			# stream hasn't been upgraded yet — so a clean TCP disconnect is the
			# best available refusal.
			push_warning(
				"Stagehand: Refusing connection — at MAX_CONCURRENT_CLIENTS (%d)"
				% MAX_CONCURRENT_CLIENTS
			)
			tcp_peer.disconnect_from_host()
			continue
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
			_peer_connected_at_msec[peer_id] = Time.get_ticks_msec()
			_had_client = true
			_pending_quit_at_msec = -1
		else:
			push_warning("Stagehand: Failed to accept WebSocket stream: %s" % error_string(err))


## Polls every connected peer, dispatches messages from open peers, and reaps
## peers that are closed or that never completed the WebSocket handshake
## within HANDSHAKE_TIMEOUT_MS. `now_msec` defaults to the real clock; callers
## (namely tests) may inject a synthetic value to exercise the handshake
## deadline without sleeping in wall-clock time.
func _poll_clients(now_msec: int = -1) -> void:
	if now_msec < 0:
		now_msec = Time.get_ticks_msec()
	var disconnected: Array[int] = []
	for peer_id: int in _clients:
		var ws: WebSocketPeer = _clients[peer_id]
		ws.poll()
		var state: WebSocketPeer.State = ws.get_ready_state()
		if state == WebSocketPeer.STATE_CLOSED:
			disconnected.append(peer_id)
			continue
		if state == WebSocketPeer.STATE_CONNECTING:
			var connected_at_msec: int = _peer_connected_at_msec.get(peer_id, now_msec)
			if now_msec - connected_at_msec >= HANDSHAKE_TIMEOUT_MS:
				push_warning(
					"Stagehand: Closing peer %d — handshake did not complete within %dms"
					% [peer_id, HANDSHAKE_TIMEOUT_MS]
				)
				ws.close()
				disconnected.append(peer_id)
			continue
		if state == WebSocketPeer.STATE_OPEN:
			while ws.get_available_packet_count() > 0:
				var packet: PackedByteArray = ws.get_packet()
				var text: String = packet.get_string_from_utf8()
				_handle_message(peer_id, text)
	for peer_id: int in disconnected:
		var _erased: bool = _clients.erase(peer_id)
		var _auth_erased: bool = _authenticated_peers.erase(peer_id)
		var _time_erased: bool = _peer_connected_at_msec.erase(peer_id)
	if (
		_had_client and _clients.is_empty() and _quit_on_disconnect
		and _pending_quit_at_msec < 0
	):
		_pending_quit_at_msec = now_msec + QUIT_ON_DISCONNECT_GRACE_MS


## Self-quit once the last client has been gone for QUIT_ON_DISCONNECT_GRACE_MS
## without a reconnect, so an abandoned game/CLI launch (client crashed, or the
## MCP server exited without cleanly killing this process) doesn't linger as a
## ~500MB headless zombie. Opt out with STAGEHAND_QUIT_ON_DISCONNECT=0.
func _check_pending_quit() -> void:
	if _pending_quit_at_msec < 0:
		return
	if Time.get_ticks_msec() < _pending_quit_at_msec:
		return
	print(
		"Stagehand: No client reconnected within %dms of the last disconnect — quitting "
		% QUIT_ON_DISCONNECT_GRACE_MS
		+ "(opt out with STAGEHAND_QUIT_ON_DISCONNECT=0)"
	)
	get_tree().quit()


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
	# dispatch_checked awaits the handler Callable (coroutine handlers such as
	# screenshot and input_text suspend on `await`, and the synchronous
	# dispatch() would return null instead of the real result) and classifies
	# what came back. The method was confirmed registered in _handle_message
	# before deferring here, which is dispatch_checked's precondition.
	var outcome: Dictionary = await _router.dispatch_checked(method, params)
	# Notifications (no id) get no response per JSON-RPC 2.0 spec.
	if id == null:
		return

	# Any failure — a handler's own canonical error envelope or an aborted
	# handler — becomes a JSON-RPC *error* response. Forwarding it as a
	# successful `result` with an embedded "error" key made a failed call look
	# indistinguishable from a successful one to every client
	# (godot-stagehand-vv2.8; docs/audits/2026-07-08-implementation-audit.md
	# finding S8).
	if outcome["outcome"] == COMMAND_ROUTER.OUTCOME_ERROR:
		var envelope: Dictionary = outcome["error"]
		var _err_send: Error = _send_to_peer(peer_id, JSON_RPC.make_handler_error_response(
			id, method, envelope, _selector_of(params)
		))
		return

	var response_text: String = JSON_RPC.make_response(id, outcome["result"])
	var send_error: Error = _send_to_peer(peer_id, response_text)
	if send_error != OK:
		var _fallback_send_error: Error = _send_to_peer(peer_id, JSON_RPC.make_handler_error_response(
			id,
			method,
			ERRORS.make(
				ERRORS.IO_ERROR,
				"Failed to send Stagehand response to WebSocket peer: %s" % error_string(send_error),
				{
					"payload_bytes": response_text.to_utf8_buffer().size(),
					"next_action": "Reduce screenshot size/crop area or increase the WebSocket outbound buffer.",
				}
			),
			_selector_of(params)
		))


## The `selector` request parameter, if the call carried one. Echoed back in a
## failure's `error.data` so a client can attribute the failure to a target
## without re-parsing the request it sent.
static func _selector_of(params: Variant) -> String:
	var p: Dictionary = _params(params)
	var raw: Variant = p.get("selector", "")
	if raw is not String:
		return ""
	var selector: String = raw
	return selector


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
	_router.register("get_accessibility_tree", _handle_get_accessibility_tree)
	_router.register("get_property", _handle_get_property)
	_router.register("set_property", _handle_set_property)
	_router.register("get_game_state", _handle_get_game_state)
	_router.register("input_mouse", _handle_input_mouse)
	_router.register("input_action", _handle_input_action)
	_router.register("input_key", _handle_input_key)
	_router.register("input_text", _handle_input_text)
	_router.register("input_touch", _handle_input_touch)
	_router.register("input_mouse_move", _handle_input_mouse_move)
	_router.register("focus_window", _handle_focus_window)
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


func _handle_focus_window(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	return INPUT_SIMULATOR.focus_window(get_tree(), p)


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
		"stagehand_version": STAGEHAND_VERSION.VERSION,
		# The protocol version is the compatibility contract the client checks
		# before it hands the session to a caller; capabilities tell it which
		# method families this process will actually serve. See
		# internal/gwp/gwp.go for the matching negotiation rules.
		"protocol_version": STAGEHAND_VERSION.PROTOCOL_VERSION,
		"protocol": STAGEHAND_VERSION.PROTOCOL_ID,
		"capabilities": STAGEHAND_VERSION.capabilities(_allow_unsafe),
		"instance_token": OS.get_environment("STAGEHAND_INSTANCE_TOKEN"),
	}


## Canonical failure for a subtree root path that resolves to nothing. Shared by
## every tool that walks a subtree, so the same condition always reports the same
## kind and the same context.
static func _root_not_found(root_path: String) -> Dictionary:
	return ERRORS.make(ERRORS.NODE_NOT_FOUND, "Root node not found: %s" % root_path, {
		"root_path": root_path,
		"next_action": "Call get_tree with the default root_path (/root) to see which nodes exist.",
	})


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
		return _root_not_found(root_path)

	return TREE_SERIALIZER.serialize_tree(root, max_depth, include_properties)


func _handle_query_nodes(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var selector: String = p.get("selector", "")
	if selector.is_empty():
		return ERRORS.missing_param("selector")
	var properties: Array[String] = []
	if p.has("properties"):
		for item: Variant in p["properties"]:
			properties.append(str(item))
	var limit: int = p.get("limit", 50)
	return TREE_SERIALIZER.query_nodes(get_tree(), selector, properties, limit)


## Derived semantic/role view of the UI. Godot's real AccessKit tree is a
## write-only push API with no GDScript read path, so this reports roles derived
## from the Control hierarchy in the engine's own vocabulary, tagged
## source="derived". Errors on engines older than 4.5.
func _handle_get_accessibility_tree(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var root_path: String = p.get("root_path", "/root")
	var max_depth: int = p.get("max_depth", 10)

	var root: Node = get_tree().root.get_node_or_null(NodePath(root_path))
	if root == null:
		return _root_not_found(root_path)

	return ACCESSIBILITY_TREE.build_response(root, max_depth)


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
		return ERRORS.missing_param("selector")
	var signal_name: String = p.get("signal_name", "")
	if signal_name.is_empty():
		return ERRORS.missing_param("signal_name")
	var timeout_ms: int = p.get("timeout_ms", 5000)
	var waiter: WAITER = WAITER.new()
	add_child(waiter)
	var result: Dictionary = await waiter.wait_for_signal(selector, signal_name, timeout_ms)
	waiter.queue_free()
	return result


func _handle_get_performance(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var requested: Array = p.get("monitors", [])
	var to_query: Array = requested if not requested.is_empty() else PERFORMANCE_SAMPLER.DEFAULT_MONITORS
	var metrics: Dictionary = {}
	for item: Variant in to_query:
		var monitor_name: String = str(item)
		if PERFORMANCE_SAMPLER.MONITORS.has(monitor_name):
			var monitor_enum: Performance.Monitor = PERFORMANCE_SAMPLER.MONITORS[monitor_name]
			metrics[monitor_name] = Performance.get_monitor(monitor_enum)
		else:
			metrics[monitor_name] = null
	return {"metrics": metrics}


## Samples a Performance monitor — optionally after a warmup, over several
## samples spaced by sample_interval_ms — and asserts a chosen statistic
## against a threshold. The defaults (1 sample, no warmup) reproduce the old
## instantaneous single-read check exactly; naming sample_count/duration_ms
## and a statistic trades that noise for a steadier read. See README's
## performance-monitoring note for what this is not (yet): proven statistical
## regression gating.
func _handle_assert_performance(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	var monitor: String = p.get("monitor", "")
	if monitor.is_empty():
		return ERRORS.missing_param("monitor")
	if not PERFORMANCE_SAMPLER.MONITORS.has(monitor):
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Unknown monitor: %s" % monitor, {
			"monitor": monitor,
			"known_monitors": PERFORMANCE_SAMPLER.MONITORS.keys(),
		})

	var threshold: float = _to_float(p.get("threshold", 0.0))
	var op: String = p.get("op", PERFORMANCE_SAMPLER.DEFAULT_OP)
	if not PERFORMANCE_SAMPLER.OPERATORS.has(op):
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Unknown operator: %s" % op, {
			"operator": op,
			"known_operators": PERFORMANCE_SAMPLER.OPERATORS,
		})

	var statistic: String = p.get("statistic", PERFORMANCE_SAMPLER.DEFAULT_STATISTIC)
	if not PERFORMANCE_SAMPLER.STATISTICS.has(statistic):
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Unknown statistic: %s" % statistic, {
			"statistic": statistic,
			"known_statistics": PERFORMANCE_SAMPLER.STATISTICS,
		})

	var warmup_ms: int = p.get("warmup_ms", PERFORMANCE_SAMPLER.DEFAULT_WARMUP_MS)
	if warmup_ms < 0:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "warmup_ms must not be negative", {"warmup_ms": warmup_ms})

	var sample_interval_ms: int = p.get("sample_interval_ms", PERFORMANCE_SAMPLER.DEFAULT_SAMPLE_INTERVAL_MS)
	if sample_interval_ms < 0:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "sample_interval_ms must not be negative", {
			"sample_interval_ms": sample_interval_ms,
		})

	if p.has("sample_count") and p.has("duration_ms"):
		return ERRORS.make(ERRORS.INVALID_PARAMS, "Specify sample_count or duration_ms, not both", {})

	var sample_count: int
	if p.has("duration_ms"):
		var duration_ms: int = p.get("duration_ms", 0)
		if duration_ms < 0:
			return ERRORS.make(ERRORS.INVALID_PARAMS, "duration_ms must not be negative", {"duration_ms": duration_ms})
		if sample_interval_ms <= 0:
			return ERRORS.make(ERRORS.INVALID_PARAMS, "duration_ms requires sample_interval_ms greater than 0", {
				"sample_interval_ms": sample_interval_ms,
			})
		sample_count = maxi(1, floori(float(duration_ms) / float(sample_interval_ms)))
	else:
		sample_count = p.get("sample_count", PERFORMANCE_SAMPLER.DEFAULT_SAMPLE_COUNT)
	if sample_count < 1:
		return ERRORS.make(ERRORS.INVALID_PARAMS, "sample_count must be at least 1", {"sample_count": sample_count})

	if warmup_ms > 0:
		await get_tree().create_timer(warmup_ms * 0.001).timeout

	var monitor_enum: Performance.Monitor = PERFORMANCE_SAMPLER.MONITORS[monitor]
	var samples: Array[float] = []
	for i: int in range(sample_count):
		samples.append(Performance.get_monitor(monitor_enum))
		if i < sample_count - 1 and sample_interval_ms > 0:
			await get_tree().create_timer(sample_interval_ms * 0.001).timeout

	var stats: Dictionary = PERFORMANCE_SAMPLER.compute_statistics(samples)
	var value: float = _to_float(stats[statistic])
	var passed: bool = PERFORMANCE_SAMPLER.compare(value, op, threshold)

	var result: Dictionary = {
		"passed": passed,
		"monitor": monitor,
		"value": value,
		"statistic": statistic,
		"threshold": threshold,
		"op": op,
		"sample_count": samples.size(),
		"min": _to_float(stats["min"]),
		"max": _to_float(stats["max"]),
		"mean": _to_float(stats["mean"]),
		"median": _to_float(stats["median"]),
		"p95": _to_float(stats["p95"]),
		"environment": PERFORMANCE_SAMPLER.environment_metadata(),
	}
	if not passed:
		result["message"] = "%s %s (n=%d) = %.4f does not satisfy %s %.4f" % [
			monitor, statistic, samples.size(), value, op, threshold,
		]
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
		return ERRORS.missing_param("selector")
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
		return ERRORS.missing_param("selector")
	var property: String = p.get("property", "")
	if property.is_empty():
		return ERRORS.missing_param("property")
	var operator: String = p.get("operator", "")
	if operator.is_empty():
		return ERRORS.missing_param("operator")
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
	# An absent or empty output_path means "pick one" — the recorder writes to a
	# session-named file under user:// so a caller need not invent a path.
	var output_path: String = p.get("output_path", "")
	var include_mouse_move: bool = p.get("include_mouse_move", false)
	_ensure_recorder()
	return _recorder.start_recording(output_path, include_mouse_move)


func _handle_record_stop(_unused_params: Variant) -> Dictionary:
	_ensure_recorder()
	return _recorder.stop_recording()


func _handle_replay(params: Variant) -> Dictionary:
	var p: Dictionary = _params(params)
	# `input_path` is the pre-vrj.6 spelling, still accepted so an older client
	# can drive this addon.
	var recording_path: String = p.get("recording_path", "")
	if recording_path.is_empty():
		recording_path = p.get("input_path", "")
	if recording_path.is_empty():
		return ERRORS.missing_param("recording_path")
	# JSON has a single number type, so a speed of 1.0 arrives as an int;
	# _to_float widens it rather than letting the typed assignment drop it.
	var speed: float = RECORDER_SPEED_DEFAULT
	if p.has("speed"):
		speed = _to_float(p.get("speed"))
	var wait_for_ready: bool = p.get("wait_for_ready", true)
	_ensure_recorder()
	return await _recorder.start_replay(recording_path, speed, wait_for_ready)


func _ensure_recorder() -> void:
	if _recorder == null:
		_recorder = INPUT_RECORDER.new()
		add_child(_recorder)


func _stop() -> void:
	if not _active:
		return
	if _recorder != null and _recorder.is_recording():
		var _result: Dictionary = _recorder.stop_recording()
	for peer_id: int in _clients:
		var ws: WebSocketPeer = _clients[peer_id]
		ws.close()
	_clients.clear()
	_authenticated_peers.clear()
	_peer_connected_at_msec.clear()
	if _tcp_server:
		_tcp_server.stop()
	_active = false
	print("Stagehand: Server stopped")


static func _is_enabled() -> bool:
	return _enabled_via_game_launch()


## Whether the server was activated as an explicit game/CLI launch — the
## STAGEHAND_ENABLED env var or the --stagehand CLI flag. Release exports need
## the additional STAGEHAND_ALLOW_RELEASE opt-in.
static func _enabled_via_game_launch() -> bool:
	return _activation_allowed(
		_activation_requested(),
		OS.has_feature("release"),
		OS.get_environment("STAGEHAND_ALLOW_RELEASE") == "1"
	)


static func _activation_requested() -> bool:
	if OS.get_environment("STAGEHAND_ENABLED") == "1":
		return true
	if "--stagehand" in OS.get_cmdline_args():
		return true
	if "--stagehand" in OS.get_cmdline_user_args():
		return true
	return false


static func _activation_allowed(
	explicit_activation: bool, release_build: bool, release_opt_in: bool
) -> bool:
	return explicit_activation and (not release_build or release_opt_in)


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
