# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandJsonRpc — JSON-RPC 2.0 message parsing and construction.


## Parse a serialized response back into a Dictionary for field assertions.
func _decode(text: String) -> Dictionary:
	var json: JSON = JSON.new()
	var err: Error = json.parse(text)
	assert_int(err).is_equal(OK)
	var data: Variant = json.data
	assert_bool(data is Dictionary).is_true()
	return data


func test_parse_request_valid() -> void:
	var text: String = '{"jsonrpc":"2.0","method":"ping","id":1}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	assert_bool(result.has("error")).is_false()
	var req: Dictionary = result["request"]
	assert_that(req.get("method")).is_equal("ping")
	# JSON has no integer type, so a numeric id arrives as a float. parse_request
	# deliberately hands the request through unchanged; normalization back to an
	# int happens on the response side (see _normalize_id / the make_response
	# tests below), which is what the client actually correlates against.
	assert_float(req.get("id")).is_equal_approx(1.0, 0.001)


func test_parse_request_no_id() -> void:
	var text: String = '{"jsonrpc":"2.0","method":"notify"}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	var req: Dictionary = result["request"]
	assert_that(req.get("method")).is_equal("notify")


func test_parse_request_string_id() -> void:
	var text: String = '{"jsonrpc":"2.0","method":"ping","id":"abc"}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	var req: Dictionary = result["request"]
	assert_that(req.get("id")).is_equal("abc")


func test_parse_request_invalid_json() -> void:
	var result: Dictionary = StagehandJsonRpc.parse_request("not valid json {{{")
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_empty_string() -> void:
	var result: Dictionary = StagehandJsonRpc.parse_request("")
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_not_a_dict() -> void:
	var result: Dictionary = StagehandJsonRpc.parse_request("[1, 2, 3]")
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_wrong_version() -> void:
	var text: String = '{"jsonrpc":"1.0","method":"ping","id":1}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_missing_version() -> void:
	var text: String = '{"method":"ping","id":1}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()


func test_parse_request_missing_method() -> void:
	var text: String = '{"jsonrpc":"2.0","id":1}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_method_not_string() -> void:
	var text: String = '{"jsonrpc":"2.0","method":42,"id":1}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()


## A malformed request that still carries an id must echo that id back, so the
## client can correlate the failure with the call it made.
func test_parse_request_error_preserves_id() -> void:
	var text: String = '{"jsonrpc":"2.0","id":9}'
	var result: Dictionary = StagehandJsonRpc.parse_request(text)
	var error_text: String = result["error"]
	assert_str(error_text).contains('"id":9')


func test_make_response_is_valid_json() -> void:
	var text: String = StagehandJsonRpc.make_response(1, {"value": 42})
	var json: JSON = JSON.new()
	assert_int(json.parse(text)).is_equal(OK)


func test_make_response_fields() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_response(7, "ok"))
	assert_that(envelope.get("jsonrpc")).is_equal("2.0")
	assert_float(envelope.get("id")).is_equal_approx(7.0, 0.001)
	assert_bool(envelope.has("result")).is_true()
	assert_bool(envelope.has("error")).is_false()


func test_make_response_null_id() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_response(null, true))
	assert_that(envelope.get("id")).is_equal(null)


func test_make_response_string_id() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_response("req-1", [1, 2]))
	assert_that(envelope.get("id")).is_equal("req-1")


## JSON has no integer type, so an id that round-trips through a client may
## arrive as 3.0; _normalize_id must hand it back as the integer 3.
func test_make_response_normalizes_whole_float_id() -> void:
	# Asserted against the serialized text: decoding would turn the int back
	# into a float again, hiding the very normalization under test.
	assert_str(StagehandJsonRpc.make_response(3.0, "ok")).contains('"id":3,')


func test_make_error_response_is_valid_json() -> void:
	var text: String = StagehandJsonRpc.make_error_response(
		1, StagehandJsonRpc.METHOD_NOT_FOUND, "Not found"
	)
	var json: JSON = JSON.new()
	assert_int(json.parse(text)).is_equal(OK)


func test_make_error_response_fields() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_error_response(
		3, StagehandJsonRpc.INVALID_PARAMS, "Bad params"
	))
	assert_that(envelope.get("jsonrpc")).is_equal("2.0")
	assert_float(envelope.get("id")).is_equal_approx(3.0, 0.001)
	assert_bool(envelope.has("result")).is_false()
	var err: Dictionary = envelope.get("error", {})
	assert_float(err.get("code")).is_equal_approx(float(StagehandJsonRpc.INVALID_PARAMS), 0.001)
	assert_that(err.get("message")).is_equal("Bad params")


func test_make_error_response_no_data_field_by_default() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_error_response(
		1, StagehandJsonRpc.INTERNAL_ERROR, "Oops"
	))
	var err: Dictionary = envelope.get("error", {})
	assert_bool(err.has("data")).is_false()


func test_make_error_response_with_data() -> void:
	var envelope: Dictionary = _decode(StagehandJsonRpc.make_error_response(
		1, StagehandJsonRpc.INTERNAL_ERROR, "Oops", {"detail": "stack trace"}
	))
	var err: Dictionary = envelope.get("error", {})
	assert_bool(err.has("data")).is_true()
	assert_that(err.get("data")).is_equal({"detail": "stack trace"})


func test_error_codes_are_defined() -> void:
	assert_int(StagehandJsonRpc.PARSE_ERROR).is_equal(-32700)
	assert_int(StagehandJsonRpc.INVALID_REQUEST).is_equal(-32600)
	assert_int(StagehandJsonRpc.METHOD_NOT_FOUND).is_equal(-32601)
	assert_int(StagehandJsonRpc.INVALID_PARAMS).is_equal(-32602)
	assert_int(StagehandJsonRpc.INTERNAL_ERROR).is_equal(-32603)
