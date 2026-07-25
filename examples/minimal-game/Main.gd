@tool
extends Node2D

func _ready() -> void:
	print("Example scene loaded - ready for Stagehand automation!")

func button_pressed() -> void:
	print("Button pressed via Stagehand automation!")
	var label: Label = $CanvasLayer/VBoxContainer/Label
	label.text = "Button was pressed!"