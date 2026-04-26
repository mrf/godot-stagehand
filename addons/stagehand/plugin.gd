@tool
extends EditorPlugin

const AUTOLOAD_NAME := "StagehandServer"
const AUTOLOAD_PATH := "res://addons/stagehand/autoload/stagehand_server.gd"

var _toolbar_button: CheckButton


func _enter_tree() -> void:
	add_autoload_singleton(AUTOLOAD_NAME, AUTOLOAD_PATH)
	_setup_toolbar()


func _exit_tree() -> void:
	remove_autoload_singleton(AUTOLOAD_NAME)
	_teardown_toolbar()


func _setup_toolbar() -> void:
	_toolbar_button = CheckButton.new()
	_toolbar_button.text = "Stagehand"
	_toolbar_button.tooltip_text = "Enable Stagehand automation server when running the game"
	_toolbar_button.button_pressed = ProjectSettings.get_setting(
		"stagehand/server/enabled", false
	)
	_toolbar_button.toggled.connect(_on_toolbar_toggled)
	add_control_to_container(CONTAINER_TOOLBAR, _toolbar_button)


func _teardown_toolbar() -> void:
	if _toolbar_button:
		remove_control_from_container(CONTAINER_TOOLBAR, _toolbar_button)
		_toolbar_button.queue_free()
		_toolbar_button = null


func _on_toolbar_toggled(enabled: bool) -> void:
	ProjectSettings.set_setting("stagehand/server/enabled", enabled)
	ProjectSettings.save()
