# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Regression test for godot-stagehand-sprh: a paused SceneTree must not
## freeze StagehandServer's socket accept/poll loop.
##
## StagehandServer services its TCP accept queue and WebSocketPeer polling
## from _process(). With the default process_mode (PROCESS_MODE_INHERIT under
## the root's PROCESS_MODE_PAUSABLE), pausing the tree — e.g. a host game's
## intro/menu overlay pausing at startup — stops _process() from firing, so a
## client's TCP connection is accepted at the socket layer but the WebSocket
## upgrade handshake never runs and the client times out. This drives a real
## client-side WebSocketPeer against a real server instance with the tree
## paused and asserts the handshake completes anyway.

const StagehandServer := preload("res://addons/stagehand/autoload/stagehand_server.gd")

var _server: StagehandServer
var _client: WebSocketPeer


func before_test() -> void:
	_server = StagehandServer.new()
	add_child(_server)
	auto_free(_server)
	# _ready() ran on add_child above. STAGEHAND_ENABLED is unset in the test
	# environment, so its enablement gate already called set_process(false) —
	# unrelated to what this test exercises, so undo it here. process_mode
	# (what this test actually exercises) is set unconditionally in _ready(),
	# before that gate, so it is already correct on _server.
	_server._tcp_server = TCPServer.new()
	var listen_err: Error = _server._tcp_server.listen(0, "127.0.0.1")
	assert_int(listen_err).is_equal(OK)
	_server._active = true
	_server.set_process(true)


func after_test() -> void:
	get_tree().paused = false
	if _client != null:
		_client.close()


func test_paused_tree_still_completes_websocket_handshake() -> void:
	get_tree().paused = true

	var port: int = _server._tcp_server.get_local_port()
	_client = WebSocketPeer.new()
	var connect_err: Error = _client.connect_to_url("ws://127.0.0.1:%d" % port)
	assert_int(connect_err).is_equal(OK)

	var handshake_completed: bool = false
	for _attempt: int in range(200):
		_client.poll()
		var state: WebSocketPeer.State = _client.get_ready_state()
		if state == WebSocketPeer.STATE_OPEN:
			handshake_completed = true
			break
		if state == WebSocketPeer.STATE_CLOSED:
			break
		await get_tree().process_frame
		OS.delay_msec(5)

	assert_bool(get_tree().paused).is_true()
	assert_bool(handshake_completed).is_true()


@warning_ignore_restore("return_value_discarded")
