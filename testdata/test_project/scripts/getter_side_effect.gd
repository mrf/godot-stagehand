## A get_<name>() method with no matching property, which wait_for_property
## must never invoke (godot-stagehand-ext-review-2026-11-327t.20).
extends Node

var boom_calls: int = 0


func get_boom() -> int:
	boom_calls += 1
	return 1
