## Test-only instrumentation for godot-stagehand smoke tests (godot-stagehand-vv2.13).
## Exposes counters that observably change when a click/key/action input event
## actually reaches the engine, so Go-side tests can assert real state
## transitions instead of trusting an RPC "success" flag alone.
extends Node2D

var click_count: int = 0
var key_press_count: int = 0
var action_press_count: int = 0


## User arg (after `--`) that makes the scene pause its tree at boot, the way a
## shipping game does behind an intro or menu overlay. It is the paused-tree
## variant the host-compat capability probe is proven against.
const PAUSE_AT_BOOT_ARG: String = "--pause-at-boot"
## User arg prefix that sets /root's content_scale_factor from script, the way
## Pixelorama does: project.godot shows nothing, only the runtime does.
const CONTENT_SCALE_ARG_PREFIX: String = "--content-scale-factor="


func _ready() -> void:
	var user_args: PackedStringArray = OS.get_cmdline_user_args()
	if PAUSE_AT_BOOT_ARG in user_args:
		get_tree().paused = true
	for arg: String in user_args:
		if arg.begins_with(CONTENT_SCALE_ARG_PREFIX):
			get_tree().root.content_scale_factor = arg.trim_prefix(CONTENT_SCALE_ARG_PREFIX).to_float()
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
