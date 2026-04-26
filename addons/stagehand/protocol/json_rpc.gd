class_name StagehandJsonRpc
extends RefCounted
## JSON-RPC 2.0 message parsing and construction for the Stagehand wire protocol.

const PARSE_ERROR := -32700
const INVALID_REQUEST := -32600
const METHOD_NOT_FOUND := -32601
const INVALID_PARAMS := -32602
const INTERNAL_ERROR := -32603


## Parse a JSON-RPC 2.0 request string.
## Returns {"request": Dictionary} on success, {"error": String} on failure.
static func parse_request(text: String) -> Dictionary:
	var json := JSON.new()
	var err := json.parse(text)
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
		"id": id,
		"result": result,
	})


## Construct a JSON-RPC 2.0 error response.
static func make_error_response(
	id: Variant, code: int, message: String, data: Variant = null
) -> String:
	var error_obj := {"code": code, "message": message}
	if data != null:
		error_obj["data"] = data
	return JSON.stringify({
		"jsonrpc": "2.0",
		"id": id,
		"error": error_obj,
	})
