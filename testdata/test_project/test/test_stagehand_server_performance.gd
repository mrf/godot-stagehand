# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Integration tests for StagehandServer._handle_assert_performance and
## _handle_get_performance: warmup, sampling aggregation, statistic selection,
## thresholds, and invalid-parameter rejection.
##
## Real Performance monitor values are not controllable from a test, so these
## assert on shape (sample counts, timing, which fields are present, internal
## consistency between "value" and the selected statistic) rather than exact
## numbers — the exact-value math already has dedicated coverage in
## test_performance_sampler.gd.

const TestableStagehandServer := preload("res://test/fixtures/testable_stagehand_server.gd")
const PERFORMANCE_SAMPLER := preload("res://addons/stagehand/core/performance_sampler.gd")

## Short enough to keep the suite fast, long enough to poll a few times.
const WARMUP_MS: int = 40
const INTERVAL_MS: int = 10

var _server: TestableStagehandServer


func before_test() -> void:
	_server = TestableStagehandServer.new()
	add_child(_server)
	auto_free(_server)


# ── backward compatibility ───────────────────────────────────────────────

func test_default_params_take_exactly_one_sample() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
	})
	assert_int(result.get("sample_count", -1)).is_equal(1)
	assert_float(PERFORMANCE_SAMPLER.to_float(result["min"])).is_equal_approx(
		PERFORMANCE_SAMPLER.to_float(result["max"]), 0.0001
	)
	assert_float(PERFORMANCE_SAMPLER.to_float(result["value"])).is_equal_approx(
		PERFORMANCE_SAMPLER.to_float(result["mean"]), 0.0001
	)


# ── aggregation ──────────────────────────────────────────────────────────

func test_sample_count_controls_how_many_samples_are_taken() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
		"sample_count": 4, "sample_interval_ms": INTERVAL_MS,
	})
	assert_int(result.get("sample_count", -1)).is_equal(4)


func test_duration_ms_derives_sample_count_from_the_interval() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
		"duration_ms": 50, "sample_interval_ms": INTERVAL_MS,
	})
	# floor(50 / 10) = 5.
	assert_int(result.get("sample_count", -1)).is_equal(5)


func test_sample_count_and_duration_ms_together_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
		"sample_count": 3, "duration_ms": 50,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("not both")


func test_duration_ms_without_a_positive_interval_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
		"duration_ms": 50, "sample_interval_ms": 0,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")


# ── warmup ───────────────────────────────────────────────────────────────

func test_warmup_delays_sampling_relative_to_no_warmup() -> void:
	# Wall-clock timer precision under the test runner is not reliable enough
	# to assert a tight floor against WARMUP_MS (see docs/gdscript-testing.md
	# and test_waiter.gd's own note on this) — assert the warmup path takes
	# measurably longer than the no-warmup path instead of a fixed amount.
	var start_without: int = Time.get_ticks_msec()
	var _no_warmup: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
	})
	var elapsed_without: int = Time.get_ticks_msec() - start_without

	var start_with: int = Time.get_ticks_msec()
	var _with_warmup: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "warmup_ms": WARMUP_MS,
	})
	var elapsed_with: int = Time.get_ticks_msec() - start_with

	assert_int(elapsed_with).is_greater(elapsed_without)


func test_negative_warmup_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "warmup_ms": -1,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")


func test_negative_sample_interval_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "sample_interval_ms": -1,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")


func test_zero_sample_count_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "sample_count": 0,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")


# ── statistic selection ──────────────────────────────────────────────────

func test_selected_statistic_is_echoed_and_drives_value() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
		"sample_count": 3, "sample_interval_ms": INTERVAL_MS, "statistic": "max",
	})
	assert_str(result.get("statistic", "")).is_equal("max")
	assert_float(PERFORMANCE_SAMPLER.to_float(result["value"])).is_equal_approx(
		PERFORMANCE_SAMPLER.to_float(result["max"]), 0.0001
	)


func test_unknown_statistic_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte", "statistic": "p99",
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("Unknown statistic")


# ── thresholds and invalid monitors/operators ────────────────────────────

func test_unknown_monitor_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "NOT_A_REAL_MONITOR", "threshold": 0,
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("Unknown monitor")


func test_unknown_operator_is_invalid_params() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "not_an_op",
	})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")
	assert_str(str(result.get("error", ""))).contains("Unknown operator")


func test_missing_monitor_is_a_missing_param_error() -> void:
	var result: Dictionary = await _server._handle_assert_performance({"threshold": 0})
	assert_str(str(result.get("error_code", ""))).is_equal("invalid_params")


func test_failing_threshold_reports_passed_false_with_a_message() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": -1, "op": "lt",
	})
	assert_bool(result.get("passed", true)).is_false()
	assert_str(str(result.get("message", ""))).contains("does not satisfy")


func test_result_includes_environment_metadata() -> void:
	var result: Dictionary = await _server._handle_assert_performance({
		"monitor": "OBJECT_COUNT", "threshold": 0, "op": "gte",
	})
	var environment: Dictionary = result.get("environment", {})
	assert_bool(environment.has("engine_version")).is_true()
	assert_bool(environment.has("headless")).is_true()
	assert_bool(environment.has("render_mode")).is_true()


# ── get_performance is unaffected ────────────────────────────────────────

func test_get_performance_default_monitors_still_include_time_fps() -> void:
	var result: Dictionary = _server._handle_get_performance({})
	var metrics: Dictionary = result.get("metrics", {})
	assert_bool(metrics.has("TIME_FPS")).is_true()


@warning_ignore_restore("return_value_discarded")
