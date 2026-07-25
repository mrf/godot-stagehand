@tool
extends EditorPlugin

const AUTOLOAD_NAME: String = "StagehandServer"
const AUTOLOAD_PATH: String = "res://addons/stagehand/autoload/stagehand_server.gd"
const ACTIVATION_ARG: String = "--stagehand"
const EDITOR_METADATA_SECTION: String = "stagehand"
const EDITOR_METADATA_KEY: String = "server_enabled"
const EDITOR_RUN_ARGS_SETTING: String = "editor/run/main_run_args"
const LEGACY_ACTIVATION_SETTING: String = "stagehand/server/enabled"

const StagehandSetupPanel := preload("res://addons/stagehand/editor/setup_panel.gd")

var _toolbar_button: CheckButton
var _setup_button: Button
var _setup_panel: StagehandSetupPanel


func _enter_tree() -> void:
	# The old toolbar persisted this setting into project.godot, where release
	# exports could read it. Erase it in memory so a later project save migrates
	# the stale value, while runtime activation ignores it unconditionally.
	ProjectSettings.set_setting(LEGACY_ACTIVATION_SETTING, null)
	add_autoload_singleton(AUTOLOAD_NAME, AUTOLOAD_PATH)
	_setup_toolbar()
	_setup_wizard()


func _exit_tree() -> void:
	_apply_editor_run_activation(false)
	remove_autoload_singleton(AUTOLOAD_NAME)
	_teardown_toolbar()
	_teardown_wizard()


func _setup_toolbar() -> void:
	_toolbar_button = CheckButton.new()
	_toolbar_button.text = "Stagehand"
	_toolbar_button.tooltip_text = "Enable Stagehand automation server when running the game"
	_toolbar_button.button_pressed = _editor_activation_enabled()
	var _connect_err: int = _toolbar_button.toggled.connect(_on_toolbar_toggled)
	add_control_to_container(CONTAINER_TOOLBAR, _toolbar_button)
	_apply_editor_run_activation(_toolbar_button.button_pressed)

	_setup_button = Button.new()
	_setup_button.text = "Setup…"
	_setup_button.tooltip_text = "Open the Stagehand setup wizard (binary download, MCP config, connection test)"
	var _setup_connect_err: int = _setup_button.pressed.connect(_on_setup_pressed)
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
	var editor_settings: EditorSettings = EditorInterface.get_editor_settings()
	editor_settings.set_project_metadata(EDITOR_METADATA_SECTION, EDITOR_METADATA_KEY, enabled)
	_apply_editor_run_activation(enabled)


func _editor_activation_enabled() -> bool:
	var editor_settings: EditorSettings = EditorInterface.get_editor_settings()
	var enabled_value: Variant = editor_settings.get_project_metadata(
		EDITOR_METADATA_SECTION, EDITOR_METADATA_KEY, false
	)
	if enabled_value is bool:
		var enabled: bool = enabled_value
		return enabled
	return false


func _apply_editor_run_activation(enabled: bool) -> void:
	var current_value: Variant = ProjectSettings.get_setting(EDITOR_RUN_ARGS_SETTING, "")
	var current_args: String = ""
	if current_value is String:
		current_args = current_value
	ProjectSettings.set_setting(
		EDITOR_RUN_ARGS_SETTING, _run_args_with_activation(current_args, enabled)
	)


static func _run_args_with_activation(run_args: String, enabled: bool) -> String:
	var args: PackedStringArray = run_args.split(" ", false)
	var filtered_args: PackedStringArray = PackedStringArray()
	for arg: String in args:
		if arg != ACTIVATION_ARG:
			var _append_ok: bool = filtered_args.append(arg)
	if enabled:
		var _append_activation_ok: bool = filtered_args.append(ACTIVATION_ARG)
	return " ".join(filtered_args)
