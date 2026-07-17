## Node script used by set_property regression tests (godot-stagehand-jzs).
## Exposes one property per Variant type this bug affects: bool, int, String,
## and untyped Variant (for the null case) — each initialized to a truthy
## value so a test can set it to the falsy counterpart and read it back.
extends Node

var flag_prop: bool = true
var string_bool_prop: bool = true
var count_prop: int = 5
var text_prop: String = "initial"
var variant_prop: Variant = "not_null"
var variant_bool_prop: Variant = true
var vector2_prop: Vector2 = Vector2(1.0, 2.0)
var vector3_prop: Vector3 = Vector3(1.0, 2.0, 3.0)
var vector2i_prop: Vector2i = Vector2i(1, 2)
var color_prop: Color = Color(1.0, 1.0, 1.0, 1.0)

## Custom setter that silently rejects a falsy assignment — models the
## keystone-reported SimManager.running incident (godot-stagehand-jzs): a
## guarded setter can leave the property unchanged even though the property
## was found and Object.set() was called without error.
var guarded_flag: bool = true:
	set(new_value):
		if new_value:
			guarded_flag = new_value
