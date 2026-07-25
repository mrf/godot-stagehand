class_name StagehandErrors
extends RefCounted
## The canonical Godot Wire Protocol failure envelope.
##
## Every handler that cannot fulfil a well-formed request returns
## [method make]'s dictionary — never a bare `{"error": "..."}` string map, and
## never a success-shaped result carrying a hidden failure. The envelope is:
##
## [codeblock]
## {
##     "error": "human readable message",
##     "error_code": "<stable machine kind, see the constants below>",
##     "details": { ... optional structured context ... }
## }
## [/codeblock]
##
## The dispatcher (StagehandServer._dispatch_and_respond) promotes any result
## matching this shape into a JSON-RPC 2.0 error response, so a failure never
## reaches a client as a successful `result`. [method json_rpc_code] is the
## mapping from the stable string kind to the numeric JSON-RPC code.
##
## See docs/error-model.md for the full contract and the Go-side mapping.

## Request parameters were missing, of the wrong type, or mutually inconsistent.
const INVALID_PARAMS: String = "invalid_params"
## A selector string was syntactically unusable.
const INVALID_SELECTOR: String = "invalid_selector"
## A selector parsed cleanly but matched no node in the live tree.
const NODE_NOT_FOUND: String = "node_not_found"
## The target node exists but has no such property.
const PROPERTY_NOT_FOUND: String = "property_not_found"
## The target node (or the global scope) exposes no such method.
const METHOD_NOT_FOUND: String = "method_not_found"
## The target node exists but does not support the requested operation.
const NOT_SUPPORTED: String = "not_supported"
## A supplied value could not be converted to the destination type, or the
## assignment did not take effect.
const INVALID_VALUE: String = "invalid_value"
## A scene file could not be found at the requested path.
const SCENE_NOT_FOUND: String = "scene_not_found"
## A scene was found but the engine refused to switch to it.
const SCENE_CHANGE_FAILED: String = "scene_change_failed"
## An expression failed to parse or raised while executing.
const EVALUATION_FAILED: String = "evaluation_failed"
## A wait or poll gave up before its condition was satisfied.
const TIMEOUT: String = "timeout"
## The renderer produced no pixels. Distinct from [constant TIMEOUT] because a
## caller genuinely branches on it: a headless or GPU-less Godot deterministically
## cannot render a viewport, so a visual check skips rather than fails, whereas a
## real capture timeout is a fault worth reporting.
const RENDERER_UNAVAILABLE: String = "renderer_unavailable"
## The recorder/replayer was asked to do something its current state forbids.
const RECORDER_STATE: String = "recorder_state"
## A file could not be opened, read, written, or parsed.
const IO_ERROR: String = "io_error"
## An unhandled GDScript runtime error aborted the handler, or the addon hit a
## condition it has no more specific kind for.
const INTERNAL: String = "internal"

## JSON-RPC 2.0 reserved code for invalid method parameters.
const _RPC_INVALID_PARAMS: int = -32602
## JSON-RPC 2.0 reserved code for an internal server error.
const _RPC_INTERNAL_ERROR: int = -32603
## Server-defined code: the request was well formed but its target is absent.
const RPC_TARGET_NOT_FOUND: int = -32004
## Server-defined code: the operation gave up before completing.
const RPC_TIMEOUT: int = -32005
## Server-defined code: a handler reported a failure of any other kind.
const RPC_HANDLER_ERROR: int = -32006


## Build a canonical failure envelope. [param details] is omitted from the
## result when empty so the wire payload stays minimal.
static func make(code: String, message: String, details: Dictionary = {}) -> Dictionary:
	var envelope: Dictionary = {
		"error": message,
		"error_code": code,
	}
	if not details.is_empty():
		envelope["details"] = details
	return envelope


## Convenience: a [constant NODE_NOT_FOUND] envelope carrying the selector that
## matched nothing, plus the standard remediation hint.
static func node_not_found(selector: String) -> Dictionary:
	return make(NODE_NOT_FOUND, "Node not found for selector: %s" % selector, {
		"selector": selector,
		"next_action": "Call get_tree or query_nodes to confirm the node exists and the selector matches it.",
	})


## Convenience: an [constant INVALID_PARAMS] envelope naming the missing key.
static func missing_param(param: String) -> Dictionary:
	return make(INVALID_PARAMS, "Missing required parameter: %s" % param, {
		"parameter": param,
	})


## Whether [param result] is a failure envelope. A handler result counts as a
## failure whenever it carries a non-empty top-level `error` string; the
## `error_code` and `details` keys are optional so that a handler predating this
## module is still classified correctly.
static func is_error(result: Variant) -> bool:
	if result is not Dictionary:
		return false
	var dict: Dictionary = result
	if not dict.has("error"):
		return false
	var message: Variant = dict["error"]
	if message is not String:
		# A non-string `error` is still a failure signal, just an unstructured one.
		return true
	var text: String = message
	return not text.is_empty()


## The stable machine kind for [param result], defaulting to
## [constant INTERNAL] when the envelope predates this module.
static func code_of(result: Dictionary) -> String:
	var raw: Variant = result.get("error_code", INTERNAL)
	if raw is not String:
		return INTERNAL
	var code: String = raw
	return INTERNAL if code.is_empty() else code


## The human-readable message for [param result].
static func message_of(result: Dictionary) -> String:
	var raw: Variant = result.get("error", "")
	if raw is String:
		return raw
	return str(raw)


## The structured context for [param result], or an empty Dictionary.
static func details_of(result: Dictionary) -> Dictionary:
	var raw: Variant = result.get("details", {})
	if raw is not Dictionary:
		return {}
	var details: Dictionary = raw
	return details


## Map a stable machine kind onto the JSON-RPC 2.0 error code the dispatcher
## reports. Unknown kinds fall back to [constant RPC_HANDLER_ERROR] rather than
## a reserved code, so a future kind never masquerades as a protocol-level fault.
static func json_rpc_code(error_code: String) -> int:
	match error_code:
		INVALID_PARAMS, INVALID_SELECTOR, INVALID_VALUE:
			return _RPC_INVALID_PARAMS
		NODE_NOT_FOUND, PROPERTY_NOT_FOUND, METHOD_NOT_FOUND, SCENE_NOT_FOUND:
			return RPC_TARGET_NOT_FOUND
		TIMEOUT:
			return RPC_TIMEOUT
		INTERNAL:
			return _RPC_INTERNAL_ERROR
		_:
			return RPC_HANDLER_ERROR
