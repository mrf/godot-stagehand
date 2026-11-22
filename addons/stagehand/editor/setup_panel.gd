@tool
extends Window
## Editor-only setup wizard for Stagehand. Reachable from the "Setup…" button in
## the Stagehand editor toolbar. Walks the user through three states:
##
##   NEEDS_SETUP — the server binary is not present at the resolved path.
##   READY       — the binary exists but no running game server answered a ping.
##   CONNECTED   — a running game's Stagehand server answered a ping.
##
## This script is @tool / editor-only: it is instantiated solely by plugin.gd
## inside the editor. It does NOT run in game/headless launches (the EditorPlugin
## never loads there), so it must never touch the runtime autoload server.

const DEFAULT_PORT: int = 26700
const CONNECT_TIMEOUT_MS: int = 3000
## JSON-RPC error code the runtime server returns to an unauthenticated peer
## (mirrors AUTHENTICATION_REQUIRED in autoload/stagehand_server.gd).
const AUTHENTICATION_REQUIRED: int = -32001

enum State { NEEDS_SETUP, READY, CONNECTED }

var _state: State = State.NEEDS_SETUP

# UI references, wired in _build_ui().
var _status_label: RichTextLabel
var _path_edit: LineEdit
var _port_spin: SpinBox
var _download_button: Button
var _test_button: Button
var _copy_button: Button
var _snippet_edit: TextEdit
var _log_label: RichTextLabel

var _sums_http: HTTPRequest
var _http: HTTPRequest
var _file_dialog: FileDialog
## SHA256SUMS text for the in-flight binary download.
var _pending_sums: String = ""


func _init() -> void:
	title = "Stagehand Setup"
	size = Vector2i(640, 560)
	# Hide instead of destroying when the user closes the window.
	var _close_connect_err: int = close_requested.connect(hide)
	_build_ui()


func _ready() -> void:
	_sums_http = HTTPRequest.new()
	var _sums_connect_err: int = _sums_http.request_completed.connect(_on_sums_completed)
	add_child(_sums_http)

	_http = HTTPRequest.new()
	var _http_connect_err: int = _http.request_completed.connect(_on_download_completed)
	add_child(_http)

	_file_dialog = FileDialog.new()
	_file_dialog.file_mode = FileDialog.FILE_MODE_SAVE_FILE
	_file_dialog.access = FileDialog.ACCESS_FILESYSTEM
	_file_dialog.use_native_dialog = true
	var _file_dialog_connect_err: int = _file_dialog.file_selected.connect(_on_save_path_selected)
	add_child(_file_dialog)

	_path_edit.text = _default_binary_path()
	_refresh_snippet()
	_recompute_state()


## Called by the plugin when the toolbar "Setup…" button is pressed.
func open_centered() -> void:
	_recompute_state()
	_refresh_snippet()
	popup_centered()


# ── UI construction ─────────────────────────────────────────────────────────

func _build_ui() -> void:
	var margin: MarginContainer = MarginContainer.new()
	margin.set_anchors_preset(Control.PRESET_FULL_RECT)
	margin.add_theme_constant_override("margin_left", 12)
	margin.add_theme_constant_override("margin_right", 12)
	margin.add_theme_constant_override("margin_top", 12)
	margin.add_theme_constant_override("margin_bottom", 12)
	add_child(margin)

	var root: VBoxContainer = VBoxContainer.new()
	root.add_theme_constant_override("separation", 8)
	margin.add_child(root)

	_status_label = RichTextLabel.new()
	_status_label.bbcode_enabled = true
	_status_label.fit_content = true
	_status_label.custom_minimum_size = Vector2(0, 28)
	root.add_child(_status_label)

	root.add_child(_make_section_label("1. Server binary"))

	var detect: Label = Label.new()
	detect.text = _describe_target()
	root.add_child(detect)

	var path_row: HBoxContainer = HBoxContainer.new()
	root.add_child(path_row)
	_path_edit = LineEdit.new()
	_path_edit.placeholder_text = "Destination path for the server binary"
	_path_edit.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	var _path_edit_connect_err: int = _path_edit.text_changed.connect(_on_path_changed)
	path_row.add_child(_path_edit)
	var browse: Button = Button.new()
	browse.text = "Browse…"
	var _browse_connect_err: int = browse.pressed.connect(_on_browse_pressed)
	path_row.add_child(browse)

	_download_button = Button.new()
	_download_button.text = "Download server binary"
	var _download_connect_err: int = _download_button.pressed.connect(_on_download_pressed)
	root.add_child(_download_button)

	root.add_child(_make_section_label("2. MCP client config"))

	_snippet_edit = TextEdit.new()
	_snippet_edit.editable = false
	_snippet_edit.custom_minimum_size = Vector2(0, 140)
	root.add_child(_snippet_edit)

	_copy_button = Button.new()
	_copy_button.text = "Copy config to clipboard"
	var _copy_connect_err: int = _copy_button.pressed.connect(_on_copy_pressed)
	root.add_child(_copy_button)

	root.add_child(_make_section_label("3. Connection test"))

	var port_row: HBoxContainer = HBoxContainer.new()
	root.add_child(port_row)
	var port_label: Label = Label.new()
	port_label.text = "Port:"
	port_row.add_child(port_label)
	_port_spin = SpinBox.new()
	_port_spin.min_value = 1
	_port_spin.max_value = 65535
	_port_spin.value = DEFAULT_PORT
	var _port_connect_err: int = _port_spin.value_changed.connect(_on_port_changed)
	port_row.add_child(_port_spin)
	_test_button = Button.new()
	_test_button.text = "Test connection"
	var _test_connect_err: int = _test_button.pressed.connect(_on_test_pressed)
	port_row.add_child(_test_button)

	_log_label = RichTextLabel.new()
	_log_label.bbcode_enabled = true
	_log_label.fit_content = true
	_log_label.custom_minimum_size = Vector2(0, 48)
	root.add_child(_log_label)


func _make_section_label(text: String) -> Label:
	var label: Label = Label.new()
	label.text = text
	label.add_theme_font_size_override("font_size", 16)
	return label


# ── State ─────────────────────────────────────────────────────────────────────

func _recompute_state() -> void:
	if not _binary_exists():
		_set_state(State.NEEDS_SETUP)
	elif _state != State.CONNECTED:
		_set_state(State.READY)
	_update_status_label()


func _set_state(new_state: State) -> void:
	_state = new_state
	_update_status_label()


func _update_status_label() -> void:
	if _status_label == null:
		return
	match _state:
		State.NEEDS_SETUP:
			_status_label.text = "[color=#e0a030]● Needs setup[/color] — download the server binary below."
		State.READY:
			_status_label.text = "[color=#3aa3ff]● Ready[/color] — binary present. Run your game with --stagehand, then test the connection."
		State.CONNECTED:
			_status_label.text = "[color=#3ac06a]● Connected[/color] — a running Stagehand server answered."


# ── Binary path / download ────────────────────────────────────────────────────

func _describe_target() -> String:
	var asset: String = StagehandReleaseAssets.current_asset_name()
	if asset.is_empty():
		return "Detected: %s / %s (unsupported — no prebuilt asset; build from source)." % [
			OS.get_name(), Engine.get_architecture_name()
		]
	return "Detected: %s / %s  →  asset: %s" % [
		OS.get_name(), Engine.get_architecture_name(), asset
	]


func _default_binary_path() -> String:
	return StagehandReleaseAssets.default_binary_path(OS.get_name(), OS.get_data_dir())


func _binary_path() -> String:
	return _path_edit.text.strip_edges()


func _binary_exists() -> bool:
	var path: String = _binary_path()
	if path.is_empty():
		return false
	return FileAccess.file_exists(path)


func _on_path_changed(_new_text: String) -> void:
	_refresh_snippet()
	_recompute_state()


func _on_browse_pressed() -> void:
	_file_dialog.current_path = _binary_path()
	_file_dialog.popup_centered_ratio(0.6)


func _on_save_path_selected(path: String) -> void:
	_path_edit.text = path
	_refresh_snippet()
	_recompute_state()


func _on_download_pressed() -> void:
	if StagehandReleaseAssets.current_asset_name().is_empty():
		_log("[color=#e05050]No prebuilt binary for %s / %s. Build from source instead.[/color]" % [
			OS.get_name(), Engine.get_architecture_name()
		])
		return
	var dest: String = _binary_path()
	if dest.is_empty():
		_log("[color=#e05050]Choose a destination path first.[/color]")
		return
	var dir_err: Error = DirAccess.make_dir_recursive_absolute(dest.get_base_dir())
	if dir_err != OK:
		_log("[color=#e05050]Could not create %s: %s[/color]" % [dest.get_base_dir(), error_string(dir_err)])
		return

	_download_button.disabled = true
	var sums_url: String = StagehandReleaseAssets.checksums_url()
	_log("Fetching %s …" % sums_url)
	var err: Error = _sums_http.request(sums_url)
	if err != OK:
		_download_button.disabled = false
		_log("[color=#e05050]Could not start download: %s[/color]" % error_string(err))


func _on_sums_completed(
	result: int, response_code: int, _headers: PackedStringArray, body: PackedByteArray
) -> void:
	var failure: String = _http_failure(result, response_code)
	if failure.is_empty():
		_pending_sums = body.get_string_from_utf8()
		if StagehandReleaseAssets.expected_sha256(
			_pending_sums, StagehandReleaseAssets.current_asset_name()
		).is_empty():
			failure = "SHA256SUMS has no line for %s" % StagehandReleaseAssets.current_asset_name()
	if not failure.is_empty():
		_download_button.disabled = false
		_log("[color=#e05050]Could not fetch SHA256SUMS: %s.[/color]" % failure)
		return

	var url: String = StagehandReleaseAssets.current_download_url()
	_log("Downloading %s …" % url)
	_http.download_file = _binary_path() + StagehandReleaseAssets.PART_SUFFIX
	var err: Error = _http.request(url)
	if err != OK:
		_http.download_file = ""
		_download_button.disabled = false
		_log("[color=#e05050]Could not start download: %s[/color]" % error_string(err))


func _on_download_completed(
	result: int, response_code: int, _headers: PackedStringArray, _body: PackedByteArray
) -> void:
	var part_path: String = _http.download_file
	# The destination chosen at download start, even if the path field was
	# edited while the request was in flight.
	var dest: String = part_path.trim_suffix(StagehandReleaseAssets.PART_SUFFIX)
	_download_button.disabled = false
	_http.download_file = ""

	var failure: String = _http_failure(result, response_code)
	if failure.is_empty():
		failure = StagehandReleaseAssets.install_verified(
			part_path, dest, _pending_sums, StagehandReleaseAssets.current_asset_name()
		)
	elif FileAccess.file_exists(part_path):
		var _remove_err: Error = DirAccess.remove_absolute(part_path)
	_pending_sums = ""
	if not failure.is_empty():
		_log("[color=#e05050]Download failed: %s (existing binary left untouched).[/color]" % failure)
		_recompute_state()
		return

	if StagehandReleaseAssets.needs_executable_bit(OS.get_name()):
		_mark_executable(dest)

	_log("[color=#3ac06a]Downloaded and verified (SHA-256) to %s[/color]" % dest)
	_refresh_snippet()
	_recompute_state()


## Returns "" for a successful 2xx response, otherwise a user-facing reason.
func _http_failure(result: int, response_code: int) -> String:
	if result != HTTPRequest.RESULT_SUCCESS:
		return "result %d — check your network or build from source" % result
	if response_code == 404:
		return "404 Not Found — release %s has no published assets yet; build from source for now" % StagehandReleaseAssets.RELEASE_TAG
	if response_code < 200 or response_code >= 300:
		return "HTTP %d" % response_code
	return ""


func _mark_executable(path: String) -> void:
	# Godot core has no chmod; shell out. Unix only (guarded by caller).
	var output: Array = []
	var code: int = OS.execute("chmod", ["+x", path], output, true)
	if code != 0:
		_log("[color=#e0a030]Could not mark executable (chmod exit %d). Run: chmod +x %s[/color]" % [code, path])


# ── MCP config snippet ──────────────────────────────────────────────────────

func _refresh_snippet() -> void:
	if _snippet_edit == null:
		return
	_snippet_edit.text = _mcp_snippet(_binary_path())


## Mirrors internal/setup/guidance.go mcpSnippet() so the editor wizard and the
## CLI "setup" command emit the same config shape.
func _mcp_snippet(binary_path: String) -> String:
	var command: String = binary_path if not binary_path.is_empty() else "/absolute/path/to/godot-stagehand"
	var config: Dictionary = {
		"mcpServers": {
			"godot-stagehand": {
				"command": command,
			},
		},
	}
	return JSON.stringify(config, "  ")


func _on_copy_pressed() -> void:
	DisplayServer.clipboard_set(_snippet_edit.text)
	_log("[color=#3ac06a]Config copied to clipboard.[/color]")


# ── Connection test ───────────────────────────────────────────────────────────

func _on_test_pressed() -> void:
	_test_button.disabled = true
	_log("Connecting to 127.0.0.1:%d …" % int(_port_spin.value))
	var result: Dictionary = await _ping_server(int(_port_spin.value))
	_test_button.disabled = false
	if result.get("ok", false):
		_set_state(State.CONNECTED)
		if result.get("authenticated", false):
			_log("[color=#3ac06a]Connected — Stagehand server v%s responded.[/color]" % result.get("version", "?"))
		else:
			_log("[color=#3ac06a]Server reachable (auth required) — the MCP client authenticates with the session token.[/color]")
	else:
		if _binary_exists():
			_set_state(State.READY)
		_log("[color=#e0a030]No response: %s[/color]" % result.get("error", "unknown"))


## Opens a WebSocket to the running game's Stagehand server and sends a JSON-RPC
## ping. Returns the shape documented on _parse_ping_response, or
## {"ok": false, "error": String} when no reply arrived.
func _ping_server(port: int) -> Dictionary:
	var ws: WebSocketPeer = WebSocketPeer.new()
	var url: String = "ws://127.0.0.1:%d" % port
	var connect_err: Error = ws.connect_to_url(url)
	if connect_err != OK:
		return {"ok": false, "error": "connect failed: %s" % error_string(connect_err)}

	var deadline: int = Time.get_ticks_msec() + CONNECT_TIMEOUT_MS
	var sent: bool = false
	while Time.get_ticks_msec() < deadline:
		ws.poll()
		var ready_state: WebSocketPeer.State = ws.get_ready_state()
		if ready_state == WebSocketPeer.STATE_OPEN:
			if not sent:
				var request: String = JSON.stringify({
					"jsonrpc": "2.0",
					"id": 1,
					"method": "ping",
					"params": {},
				})
				var send_err: Error = ws.send_text(request)
				if send_err != OK:
					ws.close()
					return {"ok": false, "error": "send failed: %s" % error_string(send_err)}
				sent = true
			while ws.get_available_packet_count() > 0:
				var packet: PackedByteArray = ws.get_packet()
				var text: String = packet.get_string_from_utf8()
				ws.close()
				return _parse_ping_response(text)
		elif ready_state == WebSocketPeer.STATE_CLOSED:
			return {"ok": false, "error": "connection closed before reply"}
		await get_tree().process_frame

	ws.close()
	return {"ok": false, "error": "timed out (is the game running with --stagehand?)"}


## Maps a ping reply to {"ok", "reachable", "authenticated", "version"|"error"}.
## The server rejects every method but `authenticate` from an unauthenticated
## peer, so an AUTHENTICATION_REQUIRED error still proves a live Stagehand
## server answered: it counts as ok/reachable with authenticated=false.
static func _parse_ping_response(text: String) -> Dictionary:
	var json: JSON = JSON.new()
	if json.parse(text) != OK:
		return {"ok": false, "reachable": false, "error": "invalid response"}
	var data: Variant = json.data
	if data is not Dictionary:
		return {"ok": false, "reachable": false, "error": "unexpected response shape"}
	var dict: Dictionary = data
	var response_result: Variant = dict.get("result")
	if response_result is Dictionary:
		var rd: Dictionary = response_result
		return {
			"ok": true,
			"reachable": true,
			"authenticated": true,
			"version": str(rd.get("stagehand_version", "?")),
		}
	var response_error: Variant = dict.get("error")
	if response_error is Dictionary:
		var ed: Dictionary = response_error
		if ed.get("code") == AUTHENTICATION_REQUIRED:
			return {"ok": true, "reachable": true, "authenticated": false}
		return {"ok": false, "reachable": true, "error": str(ed.get("message", "server error"))}
	return {"ok": false, "reachable": false, "error": "no result in response"}


# ── helpers ───────────────────────────────────────────────────────────────────

func _on_port_changed(_value: float) -> void:
	pass


func _log(bbcode: String) -> void:
	if _log_label != null:
		_log_label.text = bbcode
