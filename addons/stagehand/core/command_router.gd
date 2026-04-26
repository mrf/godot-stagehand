class_name StagehandCommandRouter
extends RefCounted
## Routes incoming JSON-RPC method names to handler callables.


var _handlers: Dictionary = {}  # String -> Callable


## Register a handler callable for a JSON-RPC method name.
func register(method: String, handler: Callable) -> void:
	_handlers[method] = handler


## Remove a registered handler.
func unregister(method: String) -> void:
	_handlers.erase(method)


## Check whether a handler is registered for the given method.
func has_handler(method: String) -> bool:
	return _handlers.has(method)


## Call the registered handler for [param method] with [param params].
## Returns the handler's return value. Caller must check [method has_handler] first.
func dispatch(method: String, params: Variant) -> Variant:
	return _handlers[method].call(params)


## Return all registered method names.
func get_methods() -> PackedStringArray:
	return PackedStringArray(_handlers.keys())
