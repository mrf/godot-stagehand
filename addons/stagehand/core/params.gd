class_name StagehandParams
extends RefCounted
## Shared typed-parameter validation for every RPC handler.
##
## A typed assignment from a Variant (`var s: String = params.get("selector")`)
## raises a runtime error when a client sends the wrong JSON type, which pauses
## the game under the debugger and aborts the handler. Handlers instead call
## [method check] first — a wrong type becomes an `invalid_params` envelope
## (JSON-RPC -32602) — and read values through the total [code]get_*[/code]
## readers, which never raise.
##
## Numbers: JSON has one number type and Godot may deliver it as int or float,
## so [constant TYPE_INT] and [constant TYPE_FLOAT] both accept either. Counts
## and depths are bounded; the limits are documented in docs/tools.md.

const ERRORS := preload("res://addons/stagehand/core/errors.gd")

## Deepest subtree get_tree / get_accessibility_tree will walk.
const MAX_TREE_DEPTH: int = 64
## Most nodes query_nodes will serialize.
const MAX_QUERY_LIMIT: int = 1000
## Most samples one assert_performance call will take.
const MAX_SAMPLE_COUNT: int = 10000
## Longest array parameter (properties, monitors, args, modifiers) accepted.
const MAX_ARRAY_ITEMS: int = 256


## Validate the types of the parameters named in [param spec] (name → a
## [enum Variant.Type]: TYPE_STRING, TYPE_INT, TYPE_FLOAT, TYPE_BOOL, TYPE_ARRAY
## or TYPE_DICTIONARY) and of each {x, y} object named in [param points] (see
## [method check_point]). An absent parameter passes — handlers own their
## required/default rules. Returns the first violation as an `invalid_params`
## envelope, or an empty Dictionary when every present parameter is well typed.
static func check(params: Dictionary, spec: Dictionary, points: Array[String] = []) -> Dictionary:
	for key: Variant in spec:
		var name: String = str(key)
		if not params.has(name):
			continue
		var expected: int = spec[key]
		var failure: Dictionary = _check_value(name, params[name], expected)
		if not failure.is_empty():
			return failure
	for point_key: String in points:
		var point_failure: Dictionary = check_point(params, point_key)
		if not point_failure.is_empty():
			return point_failure
	return {}


## Validate that [param key], when present, is an {x, y} object whose
## coordinates are numbers. A missing coordinate passes (it defaults to 0).
static func check_point(params: Dictionary, key: String) -> Dictionary:
	var failure: Dictionary = check(params, {key: TYPE_DICTIONARY})
	if not failure.is_empty() or not params.has(key):
		return failure
	var point: Dictionary = params[key]
	for axis: String in ["x", "y"]:
		if point.has(axis):
			var axis_failure: Dictionary = _check_value("%s.%s" % [key, axis], point[axis], TYPE_FLOAT)
			if not axis_failure.is_empty():
				return _with_parameter(axis_failure, key)
	return {}


## [param key] as a String, or [param default] when absent or not a String.
static func get_string(params: Dictionary, key: String, default: String) -> String:
	var raw: Variant = params.get(key)
	if raw is String:
		var text: String = raw
		return text
	return default


## [param key] as a bool, or [param default] when absent or not a bool.
static func get_bool(params: Dictionary, key: String, default: bool) -> bool:
	var raw: Variant = params.get(key)
	if raw is bool:
		var flag: bool = raw
		return flag
	return default


## [param key] as a float, or [param default] when absent or not a number.
static func get_float(params: Dictionary, key: String, default: float) -> float:
	var raw: Variant = params.get(key)
	if raw is float:
		var value: float = raw
		return value
	if raw is int:
		var whole: int = raw
		return float(whole)
	return default


## [param key] as an int clamped to [[param lo], [param hi]], or [param default]
## (unclamped) when absent or not a number. The clamp happens in float space so
## an absurd magnitude cannot overflow the int conversion.
static func get_int(
	params: Dictionary, key: String, default: int, lo: int = -9007199254740992, hi: int = 9007199254740992
) -> int:
	var raw: Variant = params.get(key)
	if raw is int:
		var whole: int = raw
		return clampi(whole, lo, hi)
	if raw is float:
		var value: float = raw
		if is_nan(value):
			return default
		return int(clampf(value, float(lo), float(hi)))
	return default


## [param key] as an Array, or an empty one when absent or not an Array.
static func get_array(params: Dictionary, key: String) -> Array:
	var raw: Variant = params.get(key)
	if raw is Array:
		var items: Array = raw
		return items
	return []


## [param key] as an Array[String] (each item stringified), or an empty one.
static func get_string_array(params: Dictionary, key: String) -> Array[String]:
	var names: Array[String] = []
	for item: Variant in get_array(params, key):
		names.append(str(item))
	return names


static func _check_value(name: String, value: Variant, expected: int) -> Dictionary:
	var actual: int = typeof(value)
	var ok: bool = false
	match expected:
		TYPE_INT, TYPE_FLOAT:
			ok = _is_finite_number(value)
		TYPE_ARRAY:
			ok = actual == TYPE_ARRAY
			if ok:
				var items: Array = value
				if items.size() > MAX_ARRAY_ITEMS:
					return ERRORS.make(
						ERRORS.INVALID_PARAMS,
						"Parameter '%s' has %d items; the limit is %d" % [name, items.size(), MAX_ARRAY_ITEMS],
						{"parameter": name, "limit": MAX_ARRAY_ITEMS, "got": items.size()}
					)
		_:
			ok = actual == expected
	if ok:
		return {}
	var expected_name: String = _kind_name(expected)
	return ERRORS.make(
		ERRORS.INVALID_PARAMS,
		"Parameter '%s' must be of type %s, got %s" % [name, expected_name, _kind_name(actual)],
		{"parameter": name, "expected": expected_name, "got": _kind_name(actual)}
	)


static func _is_finite_number(value: Variant) -> bool:
	if value is int:
		return true
	if value is float:
		var number: float = value
		return is_finite(number)
	return false


static func _with_parameter(envelope: Dictionary, parameter: String) -> Dictionary:
	var details: Dictionary = ERRORS.details_of(envelope)
	details["parameter"] = parameter
	return ERRORS.make(ERRORS.INVALID_PARAMS, ERRORS.message_of(envelope), details)


## The JSON-vocabulary name of a Variant type ("string", "integer", "object"...).
static func _kind_name(variant_type: int) -> String:
	match variant_type:
		TYPE_STRING: return "string"
		TYPE_INT: return "integer"
		TYPE_FLOAT: return "number"
		TYPE_BOOL: return "boolean"
		TYPE_ARRAY: return "array"
		TYPE_DICTIONARY: return "object"
		TYPE_NIL: return "null"
		_: return type_string(variant_type)
