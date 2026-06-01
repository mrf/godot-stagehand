@tool
extends EditorPlugin

const AUTOLOAD_NAME: String = "StagehandServer"
const AUTOLOAD_PATH: String = "res://addons/stagehand/autoload/stagehand_server.gd"

const StagehandSetupPanel := preload("res://addons/stagehand/editor/setup_panel.gd")

var _toolbar_button: CheckButton
var _setup_button: Button
var _setup_panel: StagehandSetupPanel


func _enter_tree() -> void:
	add_autoload_singleton(AUTOLOAD_NAME, AUTOLOAD_PATH)
	_setup_toolbar()
	_setup_wizard()


func _exit_tree() -> void:
	remove_autoload_singleton(AUTOLOAD_NAME)
	_teardown_toolbar()
	_teardown_wizard()


func _setup_toolbar() -> void:
	_toolbar_button = CheckButton.new()
	_toolbar_button.text = "Stagehand"
	_toolbar_button.tooltip_text = "Enable Stagehand automation server when running the game"
	_toolbar_button.button_pressed = ProjectSettings.get_setting(
		"stagehand/server/enabled", false
	)
	var _connect_err: int = _toolbar_button.toggled.connect(_on_toolbar_toggled)
	add_control_to_container(CONTAINER_TOOLBAR, _toolbar_button)

	_setup_button = Button.new()
	_setup_button.text = "Setup…"
	_setup_button.tooltip_text = "Open the Stagehand setup wizard (binary download, MCP config, connection test)"
	_setup_button.pressed.connect(_on_setup_pressed)
	add_control_to_container(CONTAINER_TOOLBAR, _setup_button)


func _teardown_toolbar() -> void:
	if _toolbar_button:
		remove_control_from_container(CONTAINER_TOOLBAR, _toolbar_button)
		_toolbar_button.queue_free()
		_toolbar_button = null
	if _setup_button:
		remove_control_from_container(CONTAINER_TOOLBAR, _setup_button)
		_setup_button.queue_free()
		_setup_button = null


func _setup_wizard() -> void:
	_setup_panel = StagehandSetupPanel.new()
	_setup_panel.hide()
	add_child(_setup_panel)


func _teardown_wizard() -> void:
	if _setup_panel:
		_setup_panel.queue_free()
		_setup_panel = null


func _on_setup_pressed() -> void:
	if _setup_panel:
		_setup_panel.open_centered()


func _on_toolbar_toggled(enabled: bool) -> void:
	ProjectSettings.set_setting("stagehand/server/enabled", enabled)
	var _save_err: Error = ProjectSettings.save()
