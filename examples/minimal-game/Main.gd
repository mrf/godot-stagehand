@tool
extends Node2D

func _ready():
	print("Example scene loaded - ready for Stagehand automation!")

func button_pressed():
	print("Button pressed via Stagehand automation!")
	var label = $CanvasLayer/VBoxContainer/Label
	label.text = "Button was pressed!"