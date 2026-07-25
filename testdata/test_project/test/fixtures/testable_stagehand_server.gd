extends "res://addons/stagehand/autoload/stagehand_server.gd"
## Test double for StagehandServer used by dispatch-path tests (e.g. the
## handler-abort-response fix). Overrides `_send_to_peer` to record the
## outgoing JSON-RPC frame instead of writing to a real WebSocketPeer, so a
## test can assert on the exact response text without a live network
## connection or WebSocket handshake.

var sent_frames: Array[Dictionary] = []


func _send_to_peer(peer_id: int, text: String) -> Error:
	sent_frames.append({"peer_id": peer_id, "text": text})
	return OK
