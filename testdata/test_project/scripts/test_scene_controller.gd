## Test-only instrumentation for godot-stagehand smoke tests (godot-stagehand-vv2.13).
## Exposes counters that observably change when a click/key/action input event
## actually reaches the engine, so Go-side tests can assert real state
## transitions instead of trusting an RPC "success" flag alone.
extends Node2D

var click_count: int = 0
var key_press_count: int = 0
var action_press_count: int = 0


func _ready() -> void:
	var button: Button = get_node("UICanvas/clickButton")
	var _err: int = button.pressed.connect(_on_click_button_pressed)


func _on_click_button_pressed() -> void:
	click_count += 1


func _input(event: InputEvent) -> void:
	if event is InputEventKey:
		var key_event: InputEventKey = event
		if key_event.pressed and not key_event.echo:
			key_press_count += 1
	elif event is InputEventAction:
		var action_event: InputEventAction = event
		if action_event.pressed:
			action_press_count += 1
