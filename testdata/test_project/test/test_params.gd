# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for StagehandParams, the shared typed-parameter validator.

const ERRORS := preload("res://addons/stagehand/core/errors.gd")


func test_check_passes_when_params_are_absent_or_well_typed() -> void:
	var spec: Dictionary = {"s": TYPE_STRING, "i": TYPE_INT, "f": TYPE_FLOAT, "b": TYPE_BOOL, "a": TYPE_ARRAY}
	assert_dict(StagehandParams.check({}, spec)).is_empty()
	assert_dict(StagehandParams.check({"s": "x", "i": 3, "f": 1.5, "b": true, "a": []}, spec)).is_empty()


func test_check_accepts_whole_json_numbers_for_numeric_kinds() -> void:
	var spec: Dictionary = {"i": TYPE_INT, "f": TYPE_FLOAT}
	# JSON.parse yields floats even for integral numbers.
	assert_dict(StagehandParams.check({"i": 3.0, "f": 2}, spec)).is_empty()


func test_check_rejects_wrong_type_with_invalid_params_envelope() -> void:
	var result: Dictionary = StagehandParams.check({"s": 5}, {"s": TYPE_STRING})
	assert_str(ERRORS.code_of(result)).is_equal(ERRORS.INVALID_PARAMS)
	var details: Dictionary = ERRORS.details_of(result)
	assert_str(str(details["parameter"])).is_equal("s")
	assert_str(str(details["expected"])).is_equal("string")
	assert_str(str(details["got"])).is_equal("integer")


func test_check_rejects_null_bool_and_non_finite_numbers() -> void:
	assert_bool(ERRORS.is_error(StagehandParams.check({"s": null}, {"s": TYPE_STRING}))).is_true()
	assert_bool(ERRORS.is_error(StagehandParams.check({"i": true}, {"i": TYPE_INT}))).is_true()
	assert_bool(ERRORS.is_error(StagehandParams.check({"f": INF}, {"f": TYPE_FLOAT}))).is_true()
	assert_bool(ERRORS.is_error(StagehandParams.check({"f": NAN}, {"f": TYPE_FLOAT}))).is_true()


func test_check_caps_array_length() -> void:
	var ok: Array = []
	ok.resize(StagehandParams.MAX_ARRAY_ITEMS)
	assert_dict(StagehandParams.check({"a": ok}, {"a": TYPE_ARRAY})).is_empty()
	var too_many: Array = []
	too_many.resize(StagehandParams.MAX_ARRAY_ITEMS + 1)
	assert_bool(ERRORS.is_error(StagehandParams.check({"a": too_many}, {"a": TYPE_ARRAY}))).is_true()


func test_check_point_requires_numeric_x_and_y() -> void:
	assert_dict(StagehandParams.check_point({}, "position")).is_empty()
	assert_dict(StagehandParams.check_point({"position": {"x": 1, "y": 2.5}}, "position")).is_empty()
	assert_dict(StagehandParams.check_point({"position": {}}, "position")).is_empty()
	assert_bool(ERRORS.is_error(StagehandParams.check_point({"position": 5}, "position"))).is_true()
	assert_bool(ERRORS.is_error(StagehandParams.check_point({"position": {"x": "1"}}, "position"))).is_true()


func test_get_int_defaults_converts_and_clamps() -> void:
	assert_int(StagehandParams.get_int({}, "n", 7, 0, 10)).is_equal(7)
	assert_int(StagehandParams.get_int({"n": 4}, "n", 7, 0, 10)).is_equal(4)
	assert_int(StagehandParams.get_int({"n": 4.0}, "n", 7, 0, 10)).is_equal(4)
	assert_int(StagehandParams.get_int({"n": 1000000000.0}, "n", 7, 0, 10)).is_equal(10)
	assert_int(StagehandParams.get_int({"n": -5}, "n", 7, 0, 10)).is_equal(0)
	assert_int(StagehandParams.get_int({"n": 1e300}, "n", 7, 0, 10)).is_equal(10)
	assert_int(StagehandParams.get_int({"n": "x"}, "n", 7, 0, 10)).is_equal(7)


func test_readers_fall_back_to_default_on_wrong_type() -> void:
	assert_float(StagehandParams.get_float({"f": 2}, "f", 0.5)).is_equal(2.0)
	assert_float(StagehandParams.get_float({"f": "x"}, "f", 0.5)).is_equal(0.5)
	assert_str(StagehandParams.get_string({"s": "a"}, "s", "d")).is_equal("a")
	assert_str(StagehandParams.get_string({"s": 1}, "s", "d")).is_equal("d")
	assert_bool(StagehandParams.get_bool({"b": true}, "b", false)).is_true()
	assert_bool(StagehandParams.get_bool({"b": "yes"}, "b", false)).is_false()
	assert_array(StagehandParams.get_array({"a": [1]}, "a")).has_size(1)
	assert_array(StagehandParams.get_array({"a": 1}, "a")).is_empty()
