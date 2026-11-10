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
const VALID_TOKEN: String = "peer-limits-suite-valid-auth-token"

var _server: StagehandServer
var _open_connections: Array[StreamPeerTCP] = []
var _open_ws_clients: Array[WebSocketPeer] = []


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
	for ws_client: WebSocketPeer in _open_ws_clients:
		ws_client.close()
	_open_ws_clients.clear()


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


# --- authentication deadline and failure (godot-stagehand-ext-review-2026-11-327t.5)


## Opens a real WebSocket client and pumps both ends until the upgrade
## completes, so the server-side peer is in STATE_OPEN (unlike
## _connect_raw_client, whose peer never leaves STATE_CONNECTING).
func _connect_ws_client() -> WebSocketPeer:
	var client: WebSocketPeer = WebSocketPeer.new()
	var url: String = "ws://127.0.0.1:%d" % _server._tcp_server.get_local_port()
	assert_int(client.connect_to_url(url)).is_equal(OK)
	_open_ws_clients.append(client)
	for _attempt: int in range(200):
		client.poll()
		_server._accept_new_connections()
		_server._poll_clients()
		if client.get_ready_state() == WebSocketPeer.STATE_OPEN:
			break
		OS.delay_msec(10)
	assert_int(client.get_ready_state()).is_equal(WebSocketPeer.STATE_OPEN)
	return client


func _send_authenticate(client: WebSocketPeer, token: Variant) -> void:
	var request: String = JSON.stringify({
		"jsonrpc": "2.0", "id": 1, "method": "authenticate", "params": {"token": token},
	})
	assert_int(client.send_text(request)).is_equal(OK)


## Sends an `authenticate` request and pumps both ends until the server has
## answered, returning the decoded response.
func _authenticate(client: WebSocketPeer, token: String) -> Dictionary:
	_send_authenticate(client, token)
	for _attempt: int in range(200):
		client.poll()
		_server._poll_clients()
		if client.get_available_packet_count() > 0:
			var parsed: Variant = JSON.parse_string(client.get_packet().get_string_from_utf8())
			assert_bool(parsed is Dictionary).is_true()
			var response: Dictionary = parsed
			return response
		OS.delay_msec(10)
	fail("no authenticate response from the server")
	return {}


func _pump_until_closed(client: WebSocketPeer) -> void:
	for _attempt: int in range(200):
		client.poll()
		_server._poll_clients()
		if client.get_ready_state() == WebSocketPeer.STATE_CLOSED:
			return
		OS.delay_msec(10)


func test_poll_clients_closes_unauthenticated_open_peer_after_auth_deadline() -> void:
	_connect_ws_client()
	assert_int(_server._clients.size()).is_equal(1)
	var peer_id: int = _server._clients.keys()[0]
	var peer: WebSocketPeer = _server._clients[peer_id]
	assert_int(peer.get_ready_state()).is_equal(WebSocketPeer.STATE_OPEN)

	var past_deadline_msec: int = (
		_server._peer_opened_at_msec[peer_id] + StagehandServer.AUTH_DEADLINE_MS + 1
	)
	_server._poll_clients(past_deadline_msec)

	assert_bool(_server._clients.has(peer_id)).is_false()
	assert_bool(_server._peer_connected_at_msec.has(peer_id)).is_false()
	assert_bool(_server._peer_opened_at_msec.has(peer_id)).is_false()


func test_poll_clients_keeps_unauthenticated_open_peer_within_auth_deadline() -> void:
	_connect_ws_client()
	var peer_id: int = _server._clients.keys()[0]

	var before_deadline_msec: int = (
		_server._peer_opened_at_msec[peer_id] + StagehandServer.AUTH_DEADLINE_MS - 1
	)
	_server._poll_clients(before_deadline_msec)

	assert_bool(_server._clients.has(peer_id)).is_true()


func test_auth_deadline_is_shorter_than_the_handshake_deadline() -> void:
	assert_int(StagehandServer.AUTH_DEADLINE_MS).is_less(StagehandServer.HANDSHAKE_TIMEOUT_MS)


func test_poll_clients_keeps_authenticated_peer_past_auth_deadline() -> void:
	_server._auth_token = VALID_TOKEN
	var client: WebSocketPeer = _connect_ws_client()
	var response: Dictionary = _authenticate(client, VALID_TOKEN)
	assert_bool(response.has("result")).is_true()
	var peer_id: int = _server._clients.keys()[0]

	var long_past_deadline_msec: int = (
		_server._peer_opened_at_msec[peer_id] + StagehandServer.AUTH_DEADLINE_MS * 10
	)
	_server._poll_clients(long_past_deadline_msec)

	assert_bool(_server._clients.has(peer_id)).is_true()


## The AUTHENTICATION_FAILED reply that precedes the close is asserted by the
## Go test TestStagehandFailedAuthenticationClosesPeer: Godot's own client
## WebSocketPeer was observed going straight to STATE_CLOSED without surfacing
## a data frame that arrives in the same poll as the close frame.
func _assert_failed_authenticate_closes_the_peer(token: Variant) -> void:
	_server._auth_token = VALID_TOKEN
	var client: WebSocketPeer = _connect_ws_client()
	_send_authenticate(client, token)

	_pump_until_closed(client)
	assert_int(client.get_ready_state()).is_equal(WebSocketPeer.STATE_CLOSED)
	assert_int(client.get_close_code()).is_equal(StagehandServer.POLICY_VIOLATION_CLOSE_CODE)
	assert_bool(_server._clients.is_empty()).is_true()


func test_failed_authenticate_closes_the_peer() -> void:
	_assert_failed_authenticate_closes_the_peer("wrong-token")


func test_failed_authenticate_with_non_string_token_closes_the_peer() -> void:
	_assert_failed_authenticate_closes_the_peer(42)


func test_failed_reauthenticate_drops_earlier_authentication_and_closes() -> void:
	_server._auth_token = VALID_TOKEN
	var client: WebSocketPeer = _connect_ws_client()
	assert_bool(_authenticate(client, VALID_TOKEN).has("result")).is_true()
	var peer_id: int = _server._clients.keys()[0]
	_send_authenticate(client, "wrong-token")

	_pump_until_closed(client)
	assert_int(client.get_ready_state()).is_equal(WebSocketPeer.STATE_CLOSED)
	assert_bool(_server._authenticated_peers.has(peer_id)).is_false()
	assert_bool(_server._clients.is_empty()).is_true()


# --- weak configured token warning ---------------------------------------------


func test_weak_auth_token_warning_flags_short_configured_token() -> void:
	var warning: String = StagehandServer._weak_auth_token_warning("short")
	assert_str(warning).contains("STAGEHAND_AUTH_TOKEN")
	assert_str(warning).contains(str(StagehandServer.MIN_AUTH_TOKEN_LENGTH))


func test_weak_auth_token_warning_accepts_minimum_length_token() -> void:
	var token: String = "x".repeat(StagehandServer.MIN_AUTH_TOKEN_LENGTH)
	assert_str(StagehandServer._weak_auth_token_warning(token)).is_empty()


func test_weak_auth_token_warning_ignores_generated_token() -> void:
	assert_str(StagehandServer._weak_auth_token_warning("")).is_empty()


@warning_ignore_restore("return_value_discarded")
