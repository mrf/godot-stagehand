@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for the editor Setup panel's "Test connection" ping-reply parsing.
## The runtime server rejects every method except `authenticate` from an
## unauthenticated peer, so an AUTHENTICATION_REQUIRED error is proof the
## server is reachable — not a failed test.

const SetupPanel := preload("res://addons/stagehand/editor/setup_panel.gd")


func test_authentication_required_error_means_reachable() -> void:
	var text: String = JSON.stringify({
		"jsonrpc": "2.0",
		"id": 1,
		"error": {
			"code": -32001,
			"message": "Authentication required before calling Stagehand methods",
		},
	})
	var parsed: Dictionary = SetupPanel._parse_ping_response(text)
	assert_that(parsed["ok"]).is_equal(true)
	assert_that(parsed["reachable"]).is_equal(true)
	assert_that(parsed["authenticated"]).is_equal(false)


func test_result_reply_is_reachable_and_authenticated() -> void:
	var text: String = JSON.stringify({
		"jsonrpc": "2.0",
		"id": 1,
		"result": {"stagehand_version": "1.2.3"},
	})
	var parsed: Dictionary = SetupPanel._parse_ping_response(text)
	assert_that(parsed["ok"]).is_equal(true)
	assert_that(parsed["reachable"]).is_equal(true)
	assert_that(parsed["authenticated"]).is_equal(true)
	assert_str(str(parsed["version"])).is_equal("1.2.3")


func test_other_error_is_not_ok() -> void:
	var text: String = JSON.stringify({
		"jsonrpc": "2.0",
		"id": 1,
		"error": {"code": -32601, "message": "Method not found"},
	})
	var parsed: Dictionary = SetupPanel._parse_ping_response(text)
	assert_that(parsed["ok"]).is_equal(false)
	assert_str(str(parsed["error"])).contains("Method not found")


func test_invalid_json_is_not_ok() -> void:
	var parsed: Dictionary = SetupPanel._parse_ping_response("not json")
	assert_that(parsed["ok"]).is_equal(false)


@warning_ignore_restore("return_value_discarded")
