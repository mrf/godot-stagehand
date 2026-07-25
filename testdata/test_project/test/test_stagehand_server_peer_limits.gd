# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandServer's peer cap and half-open handshake reaping
## (docs/audits/2026-07-08-implementation-audit.md finding S6).
##
## Both behaviors are exercised with real loopback TCP connections against a
## server instance driven manually (its accept/poll methods, not _process),
## so no autoload activation or WebSocket upgrade is required.

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
## in STATE_CONNECTING once the server wraps it in a WebSocketPeer.
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
	# TCPServer.is_connection_available() needs the connecting client's poll
	# to have run at least once for the accept queue to see it (already done
	# inside _connect_raw_client), then a moment for the listener to notice.
	for _attempt: int in range(50):
		if _server._tcp_server.is_connection_available():
			break
		OS.delay_msec(10)
	_server._accept_new_connections()


# --- peer cap ----------------------------------------------------------------


func test_accept_new_connections_admits_up_to_the_cap() -> void:
	for _i: int in range(StagehandServer.MAX_CONCURRENT_CLIENTS):
		_connect_raw_client()
		_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS)


func test_accept_new_connections_refuses_beyond_the_cap() -> void:
	for _i: int in range(StagehandServer.MAX_CONCURRENT_CLIENTS):
		_connect_raw_client()
		_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS)

	var refused: StreamPeerTCP = _connect_raw_client()
	_accept_available_connections()

	# The cap must not have grown ...
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS)
	# ... and the refused client must observe a clean close, not a hang.
	for _attempt: int in range(50):
		var _poll_err: Error = refused.poll()
		if refused.get_status() != StreamPeerTCP.STATUS_CONNECTED:
			break
		OS.delay_msec(10)
	assert_int(refused.get_status()).is_not_equal(StreamPeerTCP.STATUS_CONNECTED)


# --- half-open handshake reaping ----------------------------------------------


func test_poll_clients_reaps_peer_stuck_in_handshake_after_deadline() -> void:
	_connect_raw_client()
	_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(1)
	var peer_id: int = _server._clients.keys()[0]
	var peer: WebSocketPeer = _server._clients[peer_id]
	assert_int(peer.get_ready_state()).is_equal(WebSocketPeer.STATE_CONNECTING)

	var past_deadline_msec: int = (
		_server._peer_connected_at_msec[peer_id] + StagehandServer.HANDSHAKE_TIMEOUT_MS + 1
	)
	_server._poll_clients(past_deadline_msec)

	assert_bool(_server._clients.has(peer_id)).is_false()
	assert_bool(_server._authenticated_peers.has(peer_id)).is_false()
	assert_bool(_server._peer_connected_at_msec.has(peer_id)).is_false()


func test_poll_clients_does_not_reap_peer_within_deadline() -> void:
	_connect_raw_client()
	_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(1)
	var peer_id: int = _server._clients.keys()[0]

	var before_deadline_msec: int = (
		_server._peer_connected_at_msec[peer_id] + StagehandServer.HANDSHAKE_TIMEOUT_MS - 1
	)
	_server._poll_clients(before_deadline_msec)

	assert_bool(_server._clients.has(peer_id)).is_true()


func test_reaping_a_half_open_peer_frees_a_cap_slot() -> void:
	for _i: int in range(StagehandServer.MAX_CONCURRENT_CLIENTS):
		_connect_raw_client()
		_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS)

	# Backdate only ONE peer's connect time so it alone crosses the handshake
	# deadline; every other peer connected within the same test run (well
	# within HANDSHAKE_TIMEOUT_MS of "now") and must be left alone.
	var target_peer_id: int = _server._clients.keys()[0]
	_server._peer_connected_at_msec[target_peer_id] = (
		Time.get_ticks_msec() - StagehandServer.HANDSHAKE_TIMEOUT_MS - 1
	)
	_server._poll_clients()
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS - 1)
	assert_bool(_server._clients.has(target_peer_id)).is_false()

	_connect_raw_client()
	_accept_available_connections()
	assert_int(_server._clients.size()).is_equal(StagehandServer.MAX_CONCURRENT_CLIENTS)


@warning_ignore_restore("return_value_discarded")
