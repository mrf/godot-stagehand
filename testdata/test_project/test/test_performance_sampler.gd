# GdUnit4's fluent assertions return self, so every unchained assert_*() is a
# discarded return value. Scoped relaxation — see docs/gdscript-testing.md.
@warning_ignore_start("return_value_discarded")
extends GdUnitTestSuite
## Tests for PerformanceSampler — statistics aggregation, percentile ranking,
## comparison, and environment metadata. Pure math, no scene tree required.

const PerformanceSampler := preload("res://addons/stagehand/core/performance_sampler.gd")


func _samples(values: Array) -> Array[float]:
	var out: Array[float] = []
	for value: Variant in values:
		out.append(PerformanceSampler.to_float(value))
	return out


# ── compute_statistics ───────────────────────────────────────────────────

func test_single_sample_all_statistics_equal_that_sample() -> void:
	var stats: Dictionary = PerformanceSampler.compute_statistics(_samples([42.0]))
	assert_float(stats["min"]).is_equal_approx(42.0, 0.0001)
	assert_float(stats["max"]).is_equal_approx(42.0, 0.0001)
	assert_float(stats["mean"]).is_equal_approx(42.0, 0.0001)
	assert_float(stats["median"]).is_equal_approx(42.0, 0.0001)
	assert_float(stats["p95"]).is_equal_approx(42.0, 0.0001)


func test_min_max_mean_on_a_known_series() -> void:
	var stats: Dictionary = PerformanceSampler.compute_statistics(_samples([10.0, 20.0, 30.0, 40.0]))
	assert_float(stats["min"]).is_equal_approx(10.0, 0.0001)
	assert_float(stats["max"]).is_equal_approx(40.0, 0.0001)
	assert_float(stats["mean"]).is_equal_approx(25.0, 0.0001)


func test_statistics_are_order_independent() -> void:
	var ordered: Dictionary = PerformanceSampler.compute_statistics(_samples([1.0, 2.0, 3.0, 4.0, 5.0]))
	var shuffled: Dictionary = PerformanceSampler.compute_statistics(_samples([3.0, 5.0, 1.0, 4.0, 2.0]))
	assert_dict(shuffled).is_equal(ordered)


func test_median_of_odd_count_is_the_middle_sample() -> void:
	var stats: Dictionary = PerformanceSampler.compute_statistics(_samples([1.0, 2.0, 3.0]))
	assert_float(stats["median"]).is_equal_approx(2.0, 0.0001)


func test_p95_of_100_samples_is_the_95th_ranked_value() -> void:
	var values: Array = []
	for i: int in range(100):
		values.append(float(i + 1))  # 1.0 .. 100.0
	var stats: Dictionary = PerformanceSampler.compute_statistics(_samples(values))
	# Nearest-rank: ceil(0.95 * 100) - 1 = 94 (0-indexed) -> value 95.0.
	assert_float(stats["p95"]).is_equal_approx(95.0, 0.0001)


func test_p95_never_exceeds_the_maximum_sample() -> void:
	var stats: Dictionary = PerformanceSampler.compute_statistics(_samples([1.0, 2.0, 3.0]))
	assert_float(PerformanceSampler.to_float(stats["p95"])).is_equal_approx(
		PerformanceSampler.to_float(stats["max"]), 0.0001
	)


# ── compare ──────────────────────────────────────────────────────────────

func test_compare_lt() -> void:
	assert_bool(PerformanceSampler.compare(10.0, "lt", 20.0)).is_true()
	assert_bool(PerformanceSampler.compare(20.0, "lt", 20.0)).is_false()


func test_compare_lte() -> void:
	assert_bool(PerformanceSampler.compare(20.0, "lte", 20.0)).is_true()
	assert_bool(PerformanceSampler.compare(21.0, "lte", 20.0)).is_false()


func test_compare_gt() -> void:
	assert_bool(PerformanceSampler.compare(30.0, "gt", 20.0)).is_true()
	assert_bool(PerformanceSampler.compare(20.0, "gt", 20.0)).is_false()


func test_compare_gte() -> void:
	assert_bool(PerformanceSampler.compare(20.0, "gte", 20.0)).is_true()
	assert_bool(PerformanceSampler.compare(19.0, "gte", 20.0)).is_false()


func test_compare_eq() -> void:
	assert_bool(PerformanceSampler.compare(20.0, "eq", 20.0)).is_true()
	assert_bool(PerformanceSampler.compare(20.1, "eq", 20.0)).is_false()


func test_compare_unknown_operator_is_false() -> void:
	assert_bool(PerformanceSampler.compare(20.0, "no_such_op", 20.0)).is_false()


# ── environment_metadata ─────────────────────────────────────────────────

func test_environment_metadata_has_the_expected_keys() -> void:
	var meta: Dictionary = PerformanceSampler.environment_metadata()
	assert_bool(meta.has("engine_version")).is_true()
	assert_bool(meta.has("render_mode")).is_true()
	assert_bool(meta.has("display_server")).is_true()
	assert_bool(meta.has("headless")).is_true()
	assert_bool(meta.has("platform")).is_true()


func test_environment_metadata_headless_flag_matches_display_server() -> void:
	var meta: Dictionary = PerformanceSampler.environment_metadata()
	var display_server: String = meta["display_server"]
	assert_bool(meta["headless"]).is_equal(display_server == "headless")


# ── vocabulary constants ─────────────────────────────────────────────────

func test_known_monitors_include_time_fps() -> void:
	assert_bool(PerformanceSampler.MONITORS.has("TIME_FPS")).is_true()


func test_statistics_vocabulary_matches_the_documented_set() -> void:
	assert_array(PerformanceSampler.STATISTICS).contains_exactly(["min", "max", "mean", "median", "p95"])


@warning_ignore_restore("return_value_discarded")
