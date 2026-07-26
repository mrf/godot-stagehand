# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the quit-on-disconnect grace period (godot-stagehand-mt3i).
##
## Confirms two things established by instrumentation against a real Godot
## instance (see .orchestrator-note): (1) a genuinely never-connected server
## does not arm the pending-quit timer at all, and (2) a bare TCP-level touch
## of the port — one that never completes the WebSocket handshake, e.g. a
## port-liveness probe — is enough to set `_had_client` and therefore DOES
## arm it once that half-open peer is reaped, which is why a hand-rolled
## launch could quit despite no real Stagehand client ever attaching. The
## fix gives that first arm-and-resolve cycle a longer grace than every
## later one, on the theory that a manual launch is on a human/agent's clock
## (reading the port/token from the log, loading MCP tool schemas) rather
## than a launcher's.

const StagehandServer := preload("res://addons/stagehand/autoload/stagehand_server.gd")

var _server: StagehandServer
var _open_connections: Array[StreamPeerTCP] = []


func before_test() -> void:
	_server = StagehandServer.new()
	add_child(_server)
	auto_free(_server)
	_server._tcp_server = TCPServer.new()
	var listen_err: Error = _server._tcp_server.listen(0, "127.0.0.1")
	assert_int(listen_err).is_equal(OK)


func after_test() -> void:
	for conn: StreamPeerTCP in _open_connections:
		conn.disconnect_from_host()
	_open_connections.clear()


## Opens a raw TCP connection to the server's listening port and pumps it
## until the TCP handshake (not the WebSocket handshake) completes. The
## WebSocket upgrade is deliberately never sent, so the resulting peer stays
## in STATE_CONNECTING once the server wraps it in a WebSocketPeer — mirroring
## a bare port-liveness probe rather than a real Stagehand client.
func _connect_raw_client() -> StreamPeerTCP:
	var client: StreamPeerTCP = StreamPeerTCP.new()
	var connect_err: Error = client.connect_to_host("127.0.0.1", _server._tcp_server.get_local_port())
	assert_int(connect_err).is_equal(OK)
	for _attempt: int in range(100):
		var _poll_err: Error = client.poll()
		if client.get_status() == StreamPeerTCP.STATUS_CONNECTED:
			break
		OS.delay_msec(10)
	assert_int(client.get_status()).is_equal(StreamPeerTCP.STATUS_CONNECTED)
	_open_connections.append(client)
	return client


func _accept_available_connections() -> void:
	for _attempt: int in range(50):
		if _server._tcp_server.is_connection_available():
			break
		OS.delay_msec(10)
	_server._accept_new_connections()


func test_no_connection_ever_does_not_arm_pending_quit() -> void:
	_server._poll_clients()
	assert_int(_server._pending_quit_at_msec).is_equal(-1)


func test_first_disconnect_after_bare_tcp_probe_uses_initial_grace() -> void:
	_connect_raw_client()
	_accept_available_connections()
	assert_bool(_server._had_client).is_true()
	var peer_id: int = _server._clients.keys()[0]

	var reap_at_msec: int = (
		_server._peer_connected_at_msec[peer_id] + StagehandServer.HANDSHAKE_TIMEOUT_MS + 1
	)
	_server._poll_clients(reap_at_msec)

	assert_bool(_server._clients.is_empty()).is_true()
	assert_int(_server._pending_quit_at_msec).is_equal(
		reap_at_msec + StagehandServer.INITIAL_QUIT_ON_DISCONNECT_GRACE_MS
	)


func test_second_disconnect_uses_standard_grace() -> void:
	# First arm-and-resolve cycle, exactly as above.
	_connect_raw_client()
	_accept_available_connections()
	var first_peer_id: int = _server._clients.keys()[0]
	var first_reap_at_msec: int = (
		_server._peer_connected_at_msec[first_peer_id] + StagehandServer.HANDSHAKE_TIMEOUT_MS + 1
	)
	_server._poll_clients(first_reap_at_msec)
	assert_int(_server._pending_quit_at_msec).is_equal(
		first_reap_at_msec + StagehandServer.INITIAL_QUIT_ON_DISCONNECT_GRACE_MS
	)

	# A second peer connecting cancels the pending quit (existing behavior),
	# then disconnecting again re-arms it — this time with the standard grace.
	_connect_raw_client()
	_accept_available_connections()
	assert_int(_server._pending_quit_at_msec).is_equal(-1)
	var second_peer_id: int = _server._clients.keys()[0]
	var second_reap_at_msec: int = (
		_server._peer_connected_at_msec[second_peer_id] + StagehandServer.HANDSHAKE_TIMEOUT_MS + 1
	)
	_server._poll_clients(second_reap_at_msec)

	assert_int(_server._pending_quit_at_msec).is_equal(
		second_reap_at_msec + StagehandServer.QUIT_ON_DISCONNECT_GRACE_MS
	)


@warning_ignore_restore("return_value_discarded")
