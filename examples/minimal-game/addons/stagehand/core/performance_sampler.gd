extends RefCounted
## Pure statistics and environment-metadata helpers for
## godot_assert_performance's sampled-statistics mode.
##
## The sampling loop itself (warmup + per-sample waits) stays on
## StagehandServer because [method SceneTree.create_timer] only works from a
## node that is inside the tree; everything here is pure math with no await,
## so it is unit-testable on its own.


## Maps Performance monitor names to their enum values.
const MONITORS: Dictionary = {
	"TIME_FPS": Performance.TIME_FPS,
	"TIME_PROCESS": Performance.TIME_PROCESS,
	"TIME_PHYSICS_PROCESS": Performance.TIME_PHYSICS_PROCESS,
	"TIME_NAVIGATION_PROCESS": Performance.TIME_NAVIGATION_PROCESS,
	"MEMORY_STATIC": Performance.MEMORY_STATIC,
	"MEMORY_STATIC_MAX": Performance.MEMORY_STATIC_MAX,
	"MEMORY_MESSAGE_BUFFER_MAX": Performance.MEMORY_MESSAGE_BUFFER_MAX,
	"OBJECT_COUNT": Performance.OBJECT_COUNT,
	"OBJECT_RESOURCE_COUNT": Performance.OBJECT_RESOURCE_COUNT,
	"OBJECT_NODE_COUNT": Performance.OBJECT_NODE_COUNT,
	"OBJECT_ORPHAN_NODE_COUNT": Performance.OBJECT_ORPHAN_NODE_COUNT,
	"RENDER_TOTAL_OBJECTS_IN_FRAME": Performance.RENDER_TOTAL_OBJECTS_IN_FRAME,
	"RENDER_TOTAL_PRIMITIVES_IN_FRAME": Performance.RENDER_TOTAL_PRIMITIVES_IN_FRAME,
	"RENDER_TOTAL_DRAW_CALLS_IN_FRAME": Performance.RENDER_TOTAL_DRAW_CALLS_IN_FRAME,
	"RENDER_VIDEO_MEM_USED": Performance.RENDER_VIDEO_MEM_USED,
	"RENDER_TEXTURE_MEM_USED": Performance.RENDER_TEXTURE_MEM_USED,
	"RENDER_BUFFER_MEM_USED": Performance.RENDER_BUFFER_MEM_USED,
	"PHYSICS_2D_ACTIVE_OBJECTS": Performance.PHYSICS_2D_ACTIVE_OBJECTS,
	"PHYSICS_2D_COLLISION_PAIRS": Performance.PHYSICS_2D_COLLISION_PAIRS,
	"PHYSICS_2D_ISLAND_COUNT": Performance.PHYSICS_2D_ISLAND_COUNT,
	"PHYSICS_3D_ACTIVE_OBJECTS": Performance.PHYSICS_3D_ACTIVE_OBJECTS,
	"PHYSICS_3D_COLLISION_PAIRS": Performance.PHYSICS_3D_COLLISION_PAIRS,
	"PHYSICS_3D_ISLAND_COUNT": Performance.PHYSICS_3D_ISLAND_COUNT,
	"AUDIO_OUTPUT_LATENCY": Performance.AUDIO_OUTPUT_LATENCY,
}

## Default monitor subset for get_performance when the caller names none.
const DEFAULT_MONITORS: Array[String] = [
	"TIME_FPS", "TIME_PROCESS", "TIME_PHYSICS_PROCESS",
	"MEMORY_STATIC", "OBJECT_COUNT", "RENDER_TOTAL_DRAW_CALLS_IN_FRAME",
]

## Comparison operators assert_performance accepts.
const OPERATORS: Array[String] = ["lt", "lte", "gt", "gte", "eq"]

## Statistics assert_performance can select a threshold against. There is no
## separate "single value" option: with sample_count 1 (the default, and the
## old instantaneous behavior) every one of these degenerates to that one
## sample, so the vocabulary stays the same regardless of how many samples
## were taken.
const STATISTICS: Array[String] = ["min", "max", "mean", "median", "p95"]

const DEFAULT_STATISTIC: String = "mean"
const DEFAULT_OP: String = "lte"
const DEFAULT_SAMPLE_COUNT: int = 1
const DEFAULT_SAMPLE_INTERVAL_MS: int = 16
const DEFAULT_WARMUP_MS: int = 0


## Widen a Variant known to hold a numeric value into a statically-typed
## float, satisfying strict mode's ban on passing Variant where a narrower
## type is required. Mirrors StagehandServer._to_float; exposed here too
## since callers outside that script (tests, in particular) need the same
## widening when reading a value back out of a Dictionary this module built.
static func to_float(v: Variant) -> float:
	if v is float:
		return v
	if v is int:
		var i: int = v
		return float(i)
	return 0.0


## Aggregate a sample series into the statistics vocabulary in
## [constant STATISTICS]. [param samples] must be non-empty.
static func compute_statistics(samples: Array[float]) -> Dictionary:
	var sorted_samples: Array[float] = []
	sorted_samples.assign(samples)
	sorted_samples.sort()

	var n: int = sorted_samples.size()
	var total: float = 0.0
	for sample: float in sorted_samples:
		total += sample

	return {
		"min": sorted_samples[0],
		"max": sorted_samples[n - 1],
		"mean": total / n,
		"median": _percentile(sorted_samples, 0.5),
		"p95": _percentile(sorted_samples, 0.95),
	}


## Nearest-rank percentile: index = ceil(p * n) - 1, clamped to the valid
## range. Deterministic and always an actual sample value rather than one
## synthesized by interpolating between two samples.
static func _percentile(sorted_samples: Array[float], p: float) -> float:
	var n: int = sorted_samples.size()
	var rank: int = ceili(p * float(n)) - 1
	return sorted_samples[clampi(rank, 0, n - 1)]


## Evaluate `value <op> threshold`. Callers validate op against
## [constant OPERATORS] before sampling so this never has to fail; an
## unrecognized op falls back to false rather than aborting.
static func compare(value: float, op: String, threshold: float) -> bool:
	match op:
		"lt":
			return value < threshold
		"lte":
			return value <= threshold
		"gt":
			return value > threshold
		"gte":
			return value >= threshold
		"eq":
			return value == threshold
		_:
			return false


## Environment metadata attached to every assert_performance result, so a
## regression claim can be traced back to the engine build and rendering mode
## it was measured under.
static func environment_metadata() -> Dictionary:
	var display_server: String = DisplayServer.get_name()
	return {
		"engine_version": Engine.get_version_info()["string"],
		"render_mode": ProjectSettings.get_setting("rendering/renderer/rendering_method", "unknown"),
		"display_server": display_server,
		"headless": display_server == "headless",
		"platform": OS.get_name(),
	}
