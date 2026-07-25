class_name StagehandJsonRpc
extends RefCounted
## JSON-RPC 2.0 message parsing and construction for the Stagehand wire protocol.

## Preloaded rather than referenced by its `class_name` so it resolves in a
## headless launch with an empty global class cache — see the rationale block in
## autoload/stagehand_server.gd.
const ERRORS := preload("res://addons/stagehand/core/errors.gd")

const PARSE_ERROR: int = -32700
const INVALID_REQUEST: int = -32600
const METHOD_NOT_FOUND: int = -32601
const INVALID_PARAMS: int = -32602
const INTERNAL_ERROR: int = -32603


## Parse a JSON-RPC 2.0 request string.
## Returns {"request": Dictionary} on success, {"error": String} on failure.
static func parse_request(text: String) -> Dictionary:
	var json: JSON = JSON.new()
	var err: Error = json.parse(text)
	if err != OK:
		return {"error": make_error_response(null, PARSE_ERROR, "Parse error")}

	var data: Variant = json.data
	if data is not Dictionary:
		return {"error": make_error_response(null, INVALID_REQUEST, "Invalid request")}

	var dict: Dictionary = data
	if dict.get("jsonrpc") != "2.0":
		return {"error": make_error_response(
			dict.get("id"), INVALID_REQUEST, "Missing or invalid jsonrpc version"
		)}

	if not dict.has("method") or dict["method"] is not String:
		return {"error": make_error_response(
			dict.get("id"), INVALID_REQUEST, "Missing or invalid method"
		)}

	return {"request": dict}


## Construct a JSON-RPC 2.0 success response.
static func make_response(id: Variant, result: Variant) -> String:
	return JSON.stringify({
		"jsonrpc": "2.0",
		"id": _normalize_id(id),
		"result": result,
	})


## Construct a JSON-RPC 2.0 error response.
static func make_error_response(
	id: Variant, code: int, message: String, data: Variant = null
) -> String:
	var error_obj: Dictionary = {"code": code, "message": message}
	if data != null:
		error_obj["data"] = data
	return JSON.stringify({
		"jsonrpc": "2.0",
		"id": _normalize_id(id),
		"error": error_obj,
	})


## Construct a JSON-RPC 2.0 error response from a canonical handler failure
## envelope (see core/errors.gd). The numeric code comes from the envelope's
## stable string kind; the machine-readable kind, the method that failed, the
## selector it targeted, and any structured context travel in `error.data` so a
## client never has to parse the human-readable message to react.
static func make_handler_error_response(
	id: Variant, method: String, envelope: Dictionary, selector: String = ""
) -> String:
	var error_code: String = ERRORS.code_of(envelope)
	var data: Dictionary = {
		"error_code": error_code,
		"method": method,
	}
	if not selector.is_empty():
		data["selector"] = selector
	var details: Dictionary = ERRORS.details_of(envelope)
	if not details.is_empty():
		data["details"] = details
	return make_error_response(
		id, ERRORS.json_rpc_code(error_code), ERRORS.message_of(envelope), data
	)


static func _normalize_id(id: Variant) -> Variant:
	if id is float:
		var f: float = id
		var i: int = int(f)
		if is_equal_approx(f, float(i)):
			return i
	return id
