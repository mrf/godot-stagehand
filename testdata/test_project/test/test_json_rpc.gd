extends GdUnitTestSuite
## Tests for StagehandJsonRpc — JSON-RPC 2.0 message parsing and construction.


func test_parse_request_valid() -> void:
	var text := '{"jsonrpc":"2.0","method":"ping","id":1}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	assert_bool(result.has("error")).is_false()
	var req: Dictionary = result["request"]
	assert_that(req.get("method")).is_equal("ping")
	assert_that(req.get("id")).is_equal(1)


func test_parse_request_no_id() -> void:
	var text := '{"jsonrpc":"2.0","method":"notify"}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	var req: Dictionary = result["request"]
	assert_that(req.get("method")).is_equal("notify")


func test_parse_request_string_id() -> void:
	var text := '{"jsonrpc":"2.0","method":"ping","id":"abc"}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("request")).is_true()
	assert_that(result["request"].get("id")).is_equal("abc")


func test_parse_request_invalid_json() -> void:
	var result := StagehandJsonRpc.parse_request("not valid json {{{")
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_not_a_dict() -> void:
	var result := StagehandJsonRpc.parse_request("[1, 2, 3]")
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_wrong_version() -> void:
	var text := '{"jsonrpc":"1.0","method":"ping","id":1}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_missing_version() -> void:
	var text := '{"method":"ping","id":1}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()


func test_parse_request_missing_method() -> void:
	var text := '{"jsonrpc":"2.0","id":1}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()
	assert_bool(result.has("request")).is_false()


func test_parse_request_method_not_string() -> void:
	var text := '{"jsonrpc":"2.0","method":42,"id":1}'
	var result := StagehandJsonRpc.parse_request(text)
	assert_bool(result.has("error")).is_true()


func test_make_response_is_valid_json() -> void:
	var text := StagehandJsonRpc.make_response(1, {"value": 42})
	var json := JSON.new()
	assert_that(json.parse(text)).is_equal(OK)


func test_make_response_fields() -> void:
	var text := StagehandJsonRpc.make_response(7, "ok")
	var json := JSON.new()
	json.parse(text)
	var d: Dictionary = json.data
	assert_that(d.get("jsonrpc")).is_equal("2.0")
	assert_that(d.get("id")).is_equal(7)
	assert_bool(d.has("result")).is_true()
	assert_bool(d.has("error")).is_false()


func test_make_response_null_id() -> void:
	var text := StagehandJsonRpc.make_response(null, true)
	var json := JSON.new()
	json.parse(text)
	var d: Dictionary = json.data
	assert_that(d.get("id")).is_equal(null)


func test_make_response_string_id() -> void:
	var text := StagehandJsonRpc.make_response("req-1", [1, 2])
	var json := JSON.new()
	json.parse(text)
	assert_that(json.data.get("id")).is_equal("req-1")


func test_make_error_response_is_valid_json() -> void:
	var text := StagehandJsonRpc.make_error_response(1, StagehandJsonRpc.METHOD_NOT_FOUND, "Not found")
	var json := JSON.new()
	assert_that(json.parse(text)).is_equal(OK)


func test_make_error_response_fields() -> void:
	var text := StagehandJsonRpc.make_error_response(
		3, StagehandJsonRpc.INVALID_PARAMS, "Bad params"
	)
	var json := JSON.new()
	json.parse(text)
	var d: Dictionary = json.data
	assert_that(d.get("jsonrpc")).is_equal("2.0")
	assert_that(d.get("id")).is_equal(3)
	assert_bool(d.has("result")).is_false()
	var err: Dictionary = d.get("error", {})
	assert_that(err.get("code")).is_equal(StagehandJsonRpc.INVALID_PARAMS)
	assert_that(err.get("message")).is_equal("Bad params")


func test_make_error_response_no_data_field_by_default() -> void:
	var text := StagehandJsonRpc.make_error_response(1, StagehandJsonRpc.INTERNAL_ERROR, "Oops")
	var json := JSON.new()
	json.parse(text)
	var err: Dictionary = json.data.get("error", {})
	assert_bool(err.has("data")).is_false()


func test_make_error_response_with_data() -> void:
	var text := StagehandJsonRpc.make_error_response(
		1, StagehandJsonRpc.INTERNAL_ERROR, "Oops", {"detail": "stack trace"}
	)
	var json := JSON.new()
	json.parse(text)
	var err: Dictionary = json.data.get("error", {})
	assert_bool(err.has("data")).is_true()
	assert_that(err.get("data")).is_equal({"detail": "stack trace"})


func test_error_codes_are_defined() -> void:
	assert_that(StagehandJsonRpc.PARSE_ERROR).is_equal(-32700)
	assert_that(StagehandJsonRpc.INVALID_REQUEST).is_equal(-32600)
	assert_that(StagehandJsonRpc.METHOD_NOT_FOUND).is_equal(-32601)
	assert_that(StagehandJsonRpc.INVALID_PARAMS).is_equal(-32602)
	assert_that(StagehandJsonRpc.INTERNAL_ERROR).is_equal(-32603)
