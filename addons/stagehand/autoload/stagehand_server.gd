extends Node
## WebSocket server that accepts JSON-RPC 2.0 commands from external clients.
## Registered as an autoload by the Stagehand editor plugin.
## Only activates when STAGEHAND_ENABLED=1 env var, --stagehand CLI flag,
## or the editor toolbar toggle is on.

const DEFAULT_PORT := 26700
const VERSION := "0.1.0"

var _tcp_server: TCPServer
var _clients: Dictionary = {}  # int -> WebSocketPeer
var _next_peer_id: int = 0
var _router: StagehandCommandRouter
var _port: int = DEFAULT_PORT
var _active: bool = false


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

	var result: Variant = _router.dispatch(method, params)
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


func _handle_ping(_params: Variant) -> Dictionary:
	return {
		"status": "ok",
		"engine": "godot",
		"engine_version": Engine.get_version_info()["string"],
		"stagehand_version": VERSION,
	}


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
